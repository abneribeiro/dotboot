package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/abneribeiro/dotboot/internal/downloader"
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
	if u := interactionAuthor(i); u != nil {
		return u.Username
	}
	return "desconhecido"
}

// interactionUserID devolve o ID de quem invocou a interação — a chave do
// limite por utilizador. O username pode mudar; o ID não.
func interactionUserID(i *discordgo.InteractionCreate) string {
	if u := interactionAuthor(i); u != nil {
		return u.ID
	}
	return ""
}

func interactionAuthor(i *discordgo.InteractionCreate) *discordgo.User {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	return i.User
}

// userLimiter conta os downloads de cada utilizador, em curso ou em fila,
// para uma só pessoa não ocupar a fila de todos.
type userLimiter struct {
	mu     sync.Mutex
	max    int
	active map[string]int
}

func newUserLimiter(perUser int) *userLimiter {
	return &userLimiter{max: perUser, active: make(map[string]int)}
}

// acquire reserva um lugar para o utilizador; false quando já tem max.
func (l *userLimiter) acquire(userID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[userID] >= l.max {
		return false
	}
	l.active[userID]++
	return true
}

// release devolve o lugar reservado por acquire.
func (l *userLimiter) release(userID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active[userID] <= 1 {
		delete(l.active, userID)
		return
	}
	l.active[userID]--
}

// tooLargeHint completa a mensagem de ErrTooLarge do downloader, que não
// conhece as opções do /baixar.
const tooLargeHint = "Usa um recorte com `inicio`/`fim`, ou o modo terminal `dotboot download`."

// messageForError traduz erros do downloader em mensagens para o utilizador.
func messageForError(err error) string {
	var dlErr *downloader.DownloadError
	if !errors.As(err, &dlErr) {
		// Erros fora do downloader (disco, ffprobe…) são internos: os
		// detalhes, com caminhos do servidor, ficam no log e não no canal.
		return "❌ Não foi possível descarregar o vídeo devido a um erro interno."
	}

	switch dlErr.Kind {
	case downloader.ErrTooLarge:
		// O downloader já explica o porquê (duração, bitrate necessário).
		return fmt.Sprintf("❌ %s %s", dlErr, tooLargeHint)
	case downloader.ErrLiveNoDVR:
		return "❌ Esta live não suporta DVR ou ainda não começou. Não é possível fazer o recorte."
	case downloader.ErrTimeout:
		return "❌ O download demorou demasiado tempo. Tenta novamente com um vídeo mais curto."
	case downloader.ErrUnsupportedURL:
		return "❌ URL não suportado ou não contém vídeo. Verifica se o link está correto."
	case downloader.ErrPrivate, downloader.ErrUnavailable:
		return "❌ Vídeo privado ou indisponível. Verifica as permissões do conteúdo."
	case downloader.ErrLoginRequired:
		return "❌ O site pediu login ou verificação (ex: «confirma que não és um robô» no YouTube, ou restrição de idade). " +
			"Quem gere o bot pode configurar cookies em `DOTBOOT_COOKIES_FILE`."
	case downloader.ErrRateLimited:
		return "❌ O site está a limitar os pedidos deste bot. Tenta novamente daqui a alguns minutos."
	case downloader.ErrCanceled:
		return "❌ O bot está a reiniciar e o download foi cancelado. Tenta novamente daqui a pouco."
	}

	// Sem classificação, a linha de erro do yt-dlp é o que melhor explica a
	// falha — muito mais do que "exit status 1".
	if detail := dlErr.Detail(); detail != "" {
		return "❌ Não foi possível descarregar o vídeo:\n> " + detail
	}
	return "❌ Não foi possível descarregar o vídeo."
}

// messageForURLError explica porque é que um URL foi recusado.
func messageForURLError(err error, allowed []string) string {
	switch {
	case errors.Is(err, downloader.ErrDomainNotAllowed):
		return fmt.Sprintf("❌ Este bot só aceita links de: %s.", strings.Join(allowed, ", "))
	case errors.Is(err, downloader.ErrNotPublic):
		return "❌ Esse endereço não é um site público."
	default:
		return "❌ URL inválido. Usa um link completo (ex: https://youtube.com/...)"
	}
}

// displayMB converte o limite em bytes em MB para mensagens, arredondando: o
// limite chega sem a margem do envelope e truncá-lo anunciava "19MB" num
// servidor de 20MB.
func displayMB(bytes int64) int {
	return int((bytes + mib/2) / mib)
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

	if b.isClosing() {
		b.respondWithError(s, i, "❌ O bot está a encerrar. Tenta novamente daqui a pouco.")
		return
	}

	url, startTime, endTime, err := extractOptions(i.ApplicationCommandData().Options)
	if err != nil {
		b.respondWithError(s, i, "❌ Pedido inválido: falta o URL.")
		return
	}

	// Qualquer membro do servidor escolhe o URL que o bot vai buscar: a rede
	// local do servidor onde o bot corre tem de ficar fora de alcance.
	if err := downloader.ValidatePublicURL(url, b.cfg.AllowedDomains); err != nil {
		b.log.Info("URL recusado", "interaction_id", i.ID, "url", url, "err", err)
		b.respondWithError(s, i, messageForURLError(err, b.cfg.AllowedDomains))
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

	userID := interactionUserID(i)
	if !b.users.acquire(userID) {
		b.respondWithError(s, i, fmt.Sprintf(
			"❌ Já tens %d download(s) em curso ou na fila. Espera que terminem antes de pedires outro.", b.cfg.MaxPerUser))
		return
	}

	b.log.Info("comando /baixar aceite", "interaction_id", i.ID, "url", url, "inicio", startTime, "fim", endTime)

	// Responde imediatamente de forma diferida — o Discord dá apenas
	// 3 segundos para a primeira resposta.
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		b.users.release(userID)
		b.log.Error("erro ao enviar resposta diferida", "interaction_id", i.ID, "err", err)
		return
	}

	// O limite depende do servidor onde o comando corre (nível de boost),
	// por isso é calculado por pedido e não uma vez no arranque.
	maxSize := b.uploadLimit(s, i.GuildID)

	// wg.Add antes da goroutine para o encerramento contar trabalho em fila.
	if !b.track() {
		b.users.release(userID)
		b.editWithError(s, i, "❌ O bot está a encerrar. Tenta novamente daqui a pouco.")
		return
	}
	go func() {
		defer b.wg.Done()
		defer b.users.release(userID)

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
			// O prazo já desconta a reserva do upload, por isso o token ainda
			// é válido para avisar quem pediu — sem isto ficava "a pensar…".
			b.log.Warn("prazo da interação esgotado antes do download", "interaction_id", i.ID)
			b.editWithError(s, i, "❌ O pedido expirou enquanto esperava na fila. Tenta novamente.")
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
	maxMB := displayMB(maxSize)
	b.log.Info("a iniciar processamento de download",
		"interaction_id", i.ID, "url", url, "limit_mb", maxMB)

	res, err := downloader.DownloadForDiscord(ctx, url, sec, downloader.DiscordOpts{
		MaxFileSize: maxSize,
		TempDir:     b.cfg.TempDir,
		CookiesFile: b.cfg.CookiesFile,
		JSRuntimes:  b.cfg.JSRuntimes,
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
