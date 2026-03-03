package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// timePattern valida formatos de tempo aceites pelo yt-dlp/ffmpeg.
// Aceita: MM:SS (ex: 01:30) ou HH:MM:SS (ex: 00:01:30).
var timePattern = regexp.MustCompile(`^(\d{1,2}:)?\d{1,2}:\d{2}$`)

// InteractionCreate é o handler chamado sempre que uma interação (Slash Command)
// é recebida pelo bot. Processa apenas o comando /baixar.
func InteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Ignora interações que não sejam Application Commands.
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	// Ignora comandos que não sejam o /baixar.
	if i.ApplicationCommandData().Name != "baixar" {
		return
	}

	// Extrai as opções do comando.
	options := i.ApplicationCommandData().Options
	optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(options))
	for _, opt := range options {
		optionMap[opt.Name] = opt
	}

	// Extrai o URL (obrigatório).
	url := optionMap["url"].StringValue()

	// Extrai os tempos de início e fim (opcionais).
	var startTime, endTime string
	if opt, ok := optionMap["inicio"]; ok {
		startTime = opt.StringValue()
	}
	if opt, ok := optionMap["fim"]; ok {
		endTime = opt.StringValue()
	}

	// Valida o formato dos tempos, se fornecidos.
	if startTime != "" && !timePattern.MatchString(startTime) {
		respondWithError(s, i, fmt.Sprintf("❌ Formato de tempo inválido para `inicio`: `%s`. Usa `MM:SS` ou `HH:MM:SS`.", startTime))
		return
	}
	if endTime != "" && !timePattern.MatchString(endTime) {
		respondWithError(s, i, fmt.Sprintf("❌ Formato de tempo inválido para `fim`: `%s`. Usa `MM:SS` ou `HH:MM:SS`.", endTime))
		return
	}

	// Se apenas um dos tempos for fornecido, avisa o utilizador.
	if (startTime != "" && endTime == "") || (startTime == "" && endTime != "") {
		respondWithError(s, i, "❌ Para fazer um recorte, tens de fornecer ambos `inicio` e `fim`.")
		return
	}

	log.Printf("Comando /baixar recebido — URL: %s | Início: %s | Fim: %s", url, startTime, endTime)

	// Responde imediatamente com "A pensar..." para evitar que a interação expire.
	// O Discord dá apenas 3 segundos para a primeira resposta.
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		log.Printf("Erro ao enviar resposta diferida: %v", err)
		return
	}

	// Processa o download numa goroutine separada para não bloquear o event loop.
	go processDownload(s, i, url, startTime, endTime)
}

// processDownload gere o ciclo completo: download, upload e limpeza.
func processDownload(s *discordgo.Session, i *discordgo.InteractionCreate, url, startTime, endTime string) {
	log.Printf("[%s] A iniciar processamento de download para URL: %s", i.ID, url)

	// Chama o downloader para descarregar o vídeo.
	filepath, err := DownloadVideo(url, startTime, endTime)

	// Garante que o ficheiro local é eliminado para libertar espaço em disco,
	// mesmo que o upload falhe. Só tenta remover se o caminho não estiver vazio.
	if filepath != "" {
		defer func() {
			if removeErr := os.Remove(filepath); removeErr != nil {
				log.Printf("[%s] Aviso: erro ao limpar ficheiro temporário %s: %v", i.ID, filepath, removeErr)
			} else {
				log.Printf("[%s] Ficheiro temporário %s limpo com sucesso", i.ID, filepath)
			}
		}()
	}

	if err != nil {
		log.Printf("[%s] Erro ao descarregar vídeo: %v", i.ID, err)

		// Tratamento de erros específicos para fornecer mensagens mais úteis
		var userMsg string
		errStr := strings.ToLower(err.Error())

		switch {
		case strings.Contains(errStr, "25mb"):
			userMsg = "❌ O vídeo excede o limite de 25MB do Discord. Tenta um vídeo mais curto ou com menor qualidade."
		case strings.Contains(errStr, "dvr") || strings.Contains(errStr, "live"):
			userMsg = "❌ Esta live não suporta DVR ou ainda não começou. Não é possível fazer o recorte."
		case strings.Contains(errStr, "timeout"):
			userMsg = "❌ O download demorou demasiado tempo. Tenta novamente com um vídeo mais curto."
		case strings.Contains(errStr, "not a video") || strings.Contains(errStr, "unsupported"):
			userMsg = "❌ URL não suportado ou não contém vídeo. Verifica se o link está correto."
		case strings.Contains(errStr, "private") || strings.Contains(errStr, "unavailable"):
			userMsg = "❌ Vídeo privado ou indisponível. Verifica as permissões do conteúdo."
		default:
			userMsg = fmt.Sprintf("❌ Não foi possível descarregar o vídeo: %v", err)
		}

		editWithError(s, i, userMsg)
		return
	}

	log.Printf("[%s] Download concluído: %s", i.ID, filepath)

	// Abre o ficheiro descarregado para o enviar ao Discord.
	file, err := os.Open(filepath)
	if err != nil {
		log.Printf("[%s] Erro ao abrir o ficheiro %s: %v", i.ID, filepath, err)
		editWithError(s, i, "❌ Erro interno ao processar o ficheiro.")
		return
	}
	defer file.Close()

	log.Printf("[%s] A enviar ficheiro para o Discord...", i.ID)

	// Edita a resposta diferida para anexar o vídeo.
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
		log.Printf("[%s] Erro ao enviar o vídeo para o Discord: %v", i.ID, err)

		// Tratamento específico para erros de upload do Discord
		errStr := strings.ToLower(err.Error())
		var userMsg string

		switch {
		case strings.Contains(errStr, "request entity too large") || strings.Contains(errStr, "413"):
			userMsg = "❌ O vídeo é demasiado grande para o Discord (>25MB). Tenta um vídeo mais curto."
		case strings.Contains(errStr, "timeout"):
			userMsg = "❌ Timeout ao enviar o vídeo. Tenta novamente ou com um ficheiro mais pequeno."
		case strings.Contains(errStr, "rate limit"):
			userMsg = "❌ Muitos pedidos. Aguarda alguns segundos e tenta novamente."
		default:
			userMsg = "❌ Erro ao fazer upload do vídeo para o Discord. Tenta novamente."
		}

		editWithError(s, i, userMsg)
		return
	}

	log.Printf("[%s] Vídeo enviado com sucesso via /baixar", i.ID)
}

// respondWithError envia uma resposta de erro imediata (para validações antes do defer).
func respondWithError(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

// editWithError edita a resposta diferida com uma mensagem de erro.
func editWithError(s *discordgo.Session, i *discordgo.InteractionCreate, msg string) {
	_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: &msg,
	})
}
