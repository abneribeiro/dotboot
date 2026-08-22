package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"

	"dotboot/internal/downloader"
)

const (
	// interactionTokenTTL é a validade do token de uma interação diferida:
	// passados 15 minutos o Discord recusa qualquer edição da resposta.
	interactionTokenTTL = 15 * time.Minute

	// uploadReserve é o tempo guardado para o upload em si, depois de o
	// download terminar. Sem esta reserva um download que use os 15 minutos
	// todos deixaria o utilizador sem resposta nenhuma.
	uploadReserve = 2 * time.Minute

	// errCodeTooLarge é o código do Discord para "Request entity too large".
	errCodeTooLarge = 40005
)

// interactionDeadline devolve o instante até ao qual ainda vale a pena
// trabalhar neste pedido, derivado do timestamp embutido no ID da interação.
func interactionDeadline(id string) time.Time {
	created, err := discordgo.SnowflakeTimestamp(id)
	if err != nil {
		created = time.Now()
	}
	return created.Add(interactionTokenTTL - uploadReserve)
}

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
func messageForError(err error) string {
	var dlErr *downloader.DownloadError
	if errors.As(err, &dlErr) {
		switch dlErr.Kind {
		case downloader.ErrTooLarge:
			// O downloader já explica o porquê (duração, bitrate necessário).
			return fmt.Sprintf("❌ %s", dlErr)
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
	if (startTime != "") != (endTime != "") {
		b.respondWithError(s, i, "❌ Para fazer um recorte, tens de fornecer ambos `inicio` e `fim`.")
		return
	}

	var sec *downloader.Section
	if startTime != "" {
		if err := downloader.ValidateRange(startTime, endTime); err != nil {
			b.respondWithError(s, i, fmt.Sprintf("❌ Recorte inválido: %v. Usa `MM:SS` ou `HH:MM:SS`.", err))
			return
		}
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

	// O limite depende do servidor onde o comando corre (nível de boost),
	// por isso é calculado por pedido e não uma vez no arranque.
	maxSize := b.uploadLimit(s, i.GuildID)

	// wg.Add antes da goroutine para o encerramento contar trabalho em fila.
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()

		// O token da interação diferida vale 15 minutos: esperar mais do que
		// isso na fila seria trabalhar para uma resposta que já não pode ser
		// entregue. O prazo limita a espera e, depois, o próprio download.
		deadline := interactionDeadline(i.ID)
		queueTimer := time.NewTimer(time.Until(deadline))
		defer queueTimer.Stop()

		select {
		case b.sem <- struct{}{}:
			defer func() { <-b.sem }()
		case <-queueTimer.C:
			b.editWithError(s, i, "❌ O bot está demasiado ocupado neste momento. Tenta novamente daqui a pouco.")
			return
		case <-b.dlCtx.Done():
			b.editWithError(s, i, "❌ O bot está a encerrar; o download foi cancelado.")
			return
		}

		budget := time.Until(deadline)
		if budget <= 0 {
			b.log.Warn("prazo da interação esgotado antes do download", "interaction_id", i.ID)
			return
		}
		if budget > b.cfg.DownloadTimeout {
			budget = b.cfg.DownloadTimeout
		}

		ctx, cancel := context.WithTimeout(b.dlCtx, budget)
		defer cancel()
		b.processDownload(ctx, s, i, url, sec, maxSize)
	}()
}

// processDownload gere o ciclo completo: download, upload e limpeza.
// maxSize é o limite de upload já resolvido para este servidor.
func (b *Bot) processDownload(ctx context.Context, s *discordgo.Session, i *discordgo.InteractionCreate, url string, sec *downloader.Section, maxSize int64) {
	maxMB := int(maxSize / (1024 * 1024))
	b.log.Info("a iniciar processamento de download",
		"interaction_id", i.ID, "url", url, "limit_mb", maxMB)

	res, err := downloader.DownloadForDiscord(ctx, url, sec, downloader.DiscordOpts{
		MaxFileSize: maxSize,
		TempDir:     b.cfg.TempDir,
	})

	// Um único RemoveAll limpa o ficheiro final, os fragmentos, os .part e
	// os comprimidos rejeitados — em todos os caminhos, incluindo os de erro.
	if res.WorkDir != "" {
		defer func() {
			if removeErr := os.RemoveAll(res.WorkDir); removeErr != nil {
				b.log.Warn("erro ao limpar o diretório temporário",
					"interaction_id", i.ID, "dir", res.WorkDir, "err", removeErr)
			} else {
				b.log.Debug("diretório temporário limpo", "interaction_id", i.ID, "dir", res.WorkDir)
			}
		}()
	}

	if err != nil {
		b.log.Error("erro ao descarregar vídeo", "interaction_id", i.ID, "url", url, "err", err)
		b.editWithError(s, i, messageForError(err))
		return
	}

	b.log.Info("download concluído", "interaction_id", i.ID, "path", res.Path)

	file, err := os.Open(res.Path)
	if err != nil {
		b.log.Error("erro ao abrir o ficheiro", "interaction_id", i.ID, "path", res.Path, "err", err)
		b.editWithError(s, i, "❌ Erro interno ao processar o ficheiro.")
		return
	}
	defer file.Close()

	b.log.Info("a enviar ficheiro para o Discord", "interaction_id", i.ID, "name", res.Name)

	contentMsg := "✅ Vídeo descarregado com sucesso!"
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &contentMsg,
		Files: []*discordgo.File{
			{
				// O nome real preserva a extensão do contentor; enviar tudo
				// como .mp4 partia a reprodução inline de .webm/.mkv.
				Name:   res.Name,
				Reader: file,
			},
		},
	})

	if err != nil {
		b.log.Error("erro ao enviar o vídeo para o Discord", "interaction_id", i.ID, "err", err)
		b.editWithError(s, i, messageForUploadError(err, maxMB))
		return
	}

	b.log.Info("vídeo enviado com sucesso", "interaction_id", i.ID)
}

// messageForUploadError traduz uma falha de upload numa mensagem para o
// utilizador. Os erros vêm do discordgo, não do downloader, por isso a
// classificação usa o RESTError estruturado em vez do texto da mensagem.
func messageForUploadError(err error, maxMB int) string {
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) {
		if restErr.Message != nil && restErr.Message.Code == errCodeTooLarge {
			return fmt.Sprintf("❌ O vídeo é demasiado grande para este servidor (limite: %dMB). Tenta um vídeo mais curto.", maxMB)
		}
		if restErr.Response != nil {
			switch restErr.Response.StatusCode {
			case http.StatusRequestEntityTooLarge:
				return fmt.Sprintf("❌ O vídeo é demasiado grande para este servidor (limite: %dMB). Tenta um vídeo mais curto.", maxMB)
			case http.StatusTooManyRequests:
				return "❌ Muitos pedidos. Aguarda alguns segundos e tenta novamente."
			}
		}
	}

	if os.IsTimeout(err) {
		return "❌ Timeout ao enviar o vídeo. Tenta novamente ou com um ficheiro mais pequeno."
	}

	return "❌ Erro ao fazer upload do vídeo para o Discord. Tenta novamente."
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
