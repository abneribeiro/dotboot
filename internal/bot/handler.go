package bot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bwmarrin/discordgo"

	"dotboot/internal/downloader"
)

// extractOptions extrai o URL (obrigatório) e os tempos de recorte (opcionais)
// das opções da interação. Devolve erro se o URL faltar — nunca faz panic.
func extractOptions(opts []*discordgo.ApplicationCommandInteractionDataOption) (url, start, end string, err error) {
	optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(opts))
	for _, opt := range opts {
		if opt != nil {
			optionMap[opt.Name] = opt
		}
	}

	urlOpt, ok := optionMap["url"]
	if !ok || urlOpt.StringValue() == "" {
		return "", "", "", fmt.Errorf("opção obrigatória \"url\" em falta")
	}
	url = urlOpt.StringValue()

	if opt, ok := optionMap["inicio"]; ok {
		start = opt.StringValue()
	}
	if opt, ok := optionMap["fim"]; ok {
		end = opt.StringValue()
	}

	return url, start, end, nil
}

// interactionUser devolve o username de quem invocou a interação,
// quer venha de um servidor (Member) quer de uma DM (User).
func interactionUser(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.Username
	}
	if i.User != nil {
		return i.User.Username
	}
	return "desconhecido"
}

// messageForError traduz erros do downloader em mensagens para o utilizador.
func messageForError(err error, maxMB int) string {
	var dlErr *downloader.DownloadError
	if errors.As(err, &dlErr) {
		switch dlErr.Kind {
		case downloader.ErrTooLarge:
			return fmt.Sprintf("❌ O vídeo excede o limite de %dMB do Discord, mesmo após compressão. Tenta um vídeo mais curto ou com menor qualidade.", maxMB)
		case downloader.ErrLiveNoDVR:
			return "❌ Esta live não suporta DVR ou ainda não começou. Não é possível fazer o recorte."
		case downloader.ErrTimeout:
			return "❌ O download demorou demasiado tempo. Tenta novamente com um vídeo mais curto."
		case downloader.ErrUnsupportedURL:
			return "❌ URL não suportado ou não contém vídeo. Verifica se o link está correto."
		case downloader.ErrPrivate, downloader.ErrUnavailable:
			return "❌ Vídeo privado ou indisponível. Verifica as permissões do conteúdo."
		}
	}
	return fmt.Sprintf("❌ Não foi possível descarregar o vídeo: %v", err)
}

// handleInteraction processa o comando /baixar: valida o input, responde de
// forma diferida e delega o download a uma goroutine limitada pelo semáforo.
func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	if i.ApplicationCommandData().Name != "baixar" {
		return
	}

	b.log.Info("interação recebida",
		"interaction_id", i.ID,
		"user", interactionUser(i),
		"guild_id", i.GuildID,
	)

	if b.closing.Load() {
		b.respondWithError(s, i, "❌ O bot está a encerrar. Tenta novamente daqui a pouco.")
		return
	}

	url, startTime, endTime, err := extractOptions(i.ApplicationCommandData().Options)
	if err != nil {
		b.respondWithError(s, i, "❌ Pedido inválido: falta o URL.")
		return
	}

	if err := downloader.ValidateURL(url); err != nil {
		b.respondWithError(s, i, "❌ URL inválido. Usa um link completo (ex: https://youtube.com/...)")
		return
	}
	if startTime != "" && downloader.ValidateTime(startTime) != nil {
		b.respondWithError(s, i, fmt.Sprintf("❌ Formato de tempo inválido para `inicio`: `%s`. Usa `MM:SS` ou `HH:MM:SS`.", startTime))
		return
	}
	if endTime != "" && downloader.ValidateTime(endTime) != nil {
		b.respondWithError(s, i, fmt.Sprintf("❌ Formato de tempo inválido para `fim`: `%s`. Usa `MM:SS` ou `HH:MM:SS`.", endTime))
		return
	}
	if (startTime != "") != (endTime != "") {
		b.respondWithError(s, i, "❌ Para fazer um recorte, tens de fornecer ambos `inicio` e `fim`.")
		return
	}

	var sec *downloader.Section
	if startTime != "" {
		sec = &downloader.Section{Start: startTime, End: endTime}
	}

	b.log.Info("comando /baixar aceite", "interaction_id", i.ID, "url", url, "inicio", startTime, "fim", endTime)

	// Responde imediatamente de forma diferida — o Discord dá apenas
	// 3 segundos para a primeira resposta.
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		b.log.Error("erro ao enviar resposta diferida", "interaction_id", i.ID, "err", err)
		return
	}

	// wg.Add antes da goroutine para o encerramento contar trabalho em fila.
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		// Aquisição bloqueante do semáforo: pedidos em excesso esperam na
		// fila (o token da interação diferida vale 15 minutos), a menos
		// que o bot entre em encerramento.
		select {
		case b.sem <- struct{}{}:
			defer func() { <-b.sem }()
		case <-b.dlCtx.Done():
			b.editWithError(s, i, "❌ O bot está a encerrar; o download foi cancelado.")
			return
		}

		ctx, cancel := context.WithTimeout(b.dlCtx, b.cfg.DownloadTimeout)
		defer cancel()
		b.processDownload(ctx, s, i, url, sec)
	}()
}

// processDownload gere o ciclo completo: download, upload e limpeza.
func (b *Bot) processDownload(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, url string, sec *downloader.Section) {
	b.log.Info("a iniciar processamento de download", "interaction_id", i.ID, "url", url)

	path, err := downloader.DownloadForDiscord(ctx, url, sec, downloader.DiscordOpts{
		MaxFileSize: b.cfg.MaxFileSizeBytes(),
		TempDir:     b.cfg.TempDir,
	})

	// Garante que o ficheiro local é eliminado para libertar espaço em disco,
	// mesmo que o upload falhe. Só tenta remover se o caminho não estiver vazio.
	if path != "" {
		defer func() {
			if removeErr := os.Remove(path); removeErr != nil {
				b.log.Warn("erro ao limpar ficheiro temporário", "interaction_id", i.ID, "path", path, "err", removeErr)
			} else {
				b.log.Debug("ficheiro temporário limpo", "interaction_id", i.ID, "path", path)
			}
		}()
	}

	if err != nil {
		b.log.Error("erro ao descarregar vídeo", "interaction_id", i.ID, "url", url, "err", err)
		b.editWithError(s, i, messageForError(err, b.cfg.MaxFileSizeMB))
		return
	}

	b.log.Info("download concluído", "interaction_id", i.ID, "path", path)

	file, err := os.Open(path)
	if err != nil {
		b.log.Error("erro ao abrir o ficheiro", "interaction_id", i.ID, "path", path, "err", err)
		b.editWithError(s, i, "❌ Erro interno ao processar o ficheiro.")
		return
	}
	defer file.Close()

	b.log.Info("a enviar ficheiro para o Discord", "interaction_id", i.ID)

	contentMsg := "✅ Vídeo descarregado com sucesso!"
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &contentMsg,
		Files: []*discordgo.File{
			{
				Name:   "video.mp4",
				Reader: file,
			},
		},
	})

	if err != nil {
		b.log.Error("erro ao enviar o vídeo para o Discord", "interaction_id", i.ID, "err", err)

		// Erros de upload vêm do Discord, não do downloader — mantém-se a
		// classificação por substring aqui.
		errStr := strings.ToLower(err.Error())
		var userMsg string
		switch {
		case strings.Contains(errStr, "request entity too large") || strings.Contains(errStr, "413"):
			userMsg = fmt.Sprintf("❌ O vídeo é demasiado grande para o Discord (>%dMB). Tenta um vídeo mais curto.", b.cfg.MaxFileSizeMB)
		case strings.Contains(errStr, "timeout"):
			userMsg = "❌ Timeout ao enviar o vídeo. Tenta novamente ou com um ficheiro mais pequeno."
		case strings.Contains(errStr, "rate limit"):
			userMsg = "❌ Muitos pedidos. Aguarda alguns segundos e tenta novamente."
		default:
			userMsg = "❌ Erro ao fazer upload do vídeo para o Discord. Tenta novamente."
		}

		b.editWithError(s, i, userMsg)
		return
	}

	b.log.Info("vídeo enviado com sucesso", "interaction_id", i.ID)
}

// respondWithError envia uma resposta de erro imediata (para validações
// antes da resposta diferida).
func (b *Bot) respondWithError(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		b.log.Error("erro ao enviar resposta de erro", "interaction_id", i.ID, "err", err)
	}
}

// editWithError edita a resposta diferida com uma mensagem de erro.
func (b *Bot) editWithError(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg}); err != nil {
		b.log.Error("erro ao editar resposta com mensagem de erro", "interaction_id", i.ID, "err", err)
	}
}
