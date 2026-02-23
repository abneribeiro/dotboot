package main

import (
	"fmt"
	"log"
	"os"
	"regexp"

	"github.com/bwmarrin/discordgo"
)

// urlPattern é a expressão regular compilada que deteta links de redes sociais suportadas.
// Suporta: YouTube (youtube.com/watch, youtu.be, youtube.com/shorts),
// Twitter/X (twitter.com, x.com) e Instagram (instagram.com/reel, instagram.com/p).
var urlPattern = regexp.MustCompile(
	`https?://(?:(?:www\.)?(?:youtube\.com/(?:watch|shorts)|youtu\.be/)|` +
		`(?:(?:www\.)?(?:twitter\.com|x\.com)/\S+/status/)|` +
		`(?:(?:www\.)?instagram\.com/(?:reels?|p)/))` +
		`[^\s]+`,
)

// MessageCreate é o handler chamado sempre que uma nova mensagem é criada
// num canal visível ao bot. Verifica se a mensagem contém um URL válido
// e, em caso positivo, inicia o processo de download numa goroutine separada.
func MessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignora mensagens enviadas pelo próprio bot para evitar loops infinitos.
	if m.Author.ID == s.State.User.ID {
		return
	}

	// Ignora mensagens de outros bots.
	if m.Author.Bot {
		return
	}

	// Extrai o primeiro URL correspondente ao padrão de redes sociais.
	url := urlPattern.FindString(m.Content)
	if url == "" {
		return
	}

	log.Printf("URL detetado de %s: %s", m.Author.Username, url)

	// Processa o download e upload numa goroutine separada para não bloquear
	// o event loop do bot, permitindo lidar com múltiplos pedidos em simultâneo.
	go processVideo(s, m, url)
}

// processVideo gere o ciclo completo: reação de espera, download, upload e limpeza.
func processVideo(s *discordgo.Session, m *discordgo.MessageCreate, url string) {
	channelID := m.ChannelID
	messageID := m.ID

	// Adiciona a reação ⏳ (ampulheta) para indicar que o processamento começou.
	_ = s.MessageReactionAdd(channelID, messageID, "⏳")

	// Chama o downloader para descarregar o vídeo. O ficheiro é sempre eliminado
	// no final, independentemente de sucesso ou erro (defer).
	filepath, err := DownloadVideo(url)

	// Garante que o ficheiro local é eliminado para libertar espaço em disco,
	// mesmo que o upload falhe. Só tenta remover se o caminho não estiver vazio.
	if filepath != "" {
		defer os.Remove(filepath)
	}

	if err != nil {
		log.Printf("Erro ao descarregar vídeo: %v", err)

		// Remove a reação ⏳ e adiciona ❌ para indicar falha.
		_ = s.MessageReactionRemove(channelID, messageID, "⏳", s.State.User.ID)
		_ = s.MessageReactionAdd(channelID, messageID, "❌")

		// Envia uma mensagem de erro no canal como resposta à mensagem original.
		_, _ = s.ChannelMessageSendReply(channelID,
			fmt.Sprintf("❌ Não foi possível descarregar o vídeo: %v", err),
			m.Reference(),
		)
		return
	}

	// Abre o ficheiro descarregado para o enviar ao Discord.
	file, err := os.Open(filepath)
	if err != nil {
		log.Printf("Erro ao abrir o ficheiro: %v", err)
		_ = s.MessageReactionRemove(channelID, messageID, "⏳", s.State.User.ID)
		_ = s.MessageReactionAdd(channelID, messageID, "❌")
		return
	}
	defer file.Close()

	// Envia o ficheiro de vídeo como resposta à mensagem original.
	// Usa ChannelMessageSendComplex para anexar o ficheiro e referenciar a mensagem.
	_, err = s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Reference: m.Reference(),
		Files: []*discordgo.File{
			{
				Name:   "video.mp4",
				Reader: file,
			},
		},
	})

	if err != nil {
		log.Printf("Erro ao enviar o vídeo para o Discord: %v", err)
		_ = s.MessageReactionRemove(channelID, messageID, "⏳", s.State.User.ID)
		_ = s.MessageReactionAdd(channelID, messageID, "❌")
		_, _ = s.ChannelMessageSendReply(channelID,
			"❌ Erro ao fazer upload do vídeo para o Discord.",
			m.Reference(),
		)
		return
	}

	// Sucesso: remove ⏳ e adiciona ✅.
	_ = s.MessageReactionRemove(channelID, messageID, "⏳", s.State.User.ID)
	_ = s.MessageReactionAdd(channelID, messageID, "✅")

	log.Printf("Vídeo enviado com sucesso para o canal %s", channelID)
}
