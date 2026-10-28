package bot

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bwmarrin/discordgo"

	"dotboot/internal/downloader"
)

func strOption(name, value string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name:  name,
		Type:  discordgo.ApplicationCommandOptionString,
		Value: value,
	}
}

func TestExtractOptions(t *testing.T) {
	tests := []struct {
		name      string
		opts      []*discordgo.ApplicationCommandInteractionDataOption
		wantURL   string
		wantStart string
		wantEnd   string
		wantErr   bool
	}{
		{
			name:    "nil slice não faz panic",
			opts:    nil,
			wantErr: true,
		},
		{
			name:    "url em falta",
			opts:    []*discordgo.ApplicationCommandInteractionDataOption{strOption("inicio", "01:00")},
			wantErr: true,
		},
		{
			name:    "url vazio",
			opts:    []*discordgo.ApplicationCommandInteractionDataOption{strOption("url", "")},
			wantErr: true,
		},
		{
			name:    "só url",
			opts:    []*discordgo.ApplicationCommandInteractionDataOption{strOption("url", "https://youtu.be/abc")},
			wantURL: "https://youtu.be/abc",
		},
		{
			name: "url com inicio",
			opts: []*discordgo.ApplicationCommandInteractionDataOption{
				strOption("url", "https://youtu.be/abc"),
				strOption("inicio", "01:00"),
			},
			wantURL:   "https://youtu.be/abc",
			wantStart: "01:00",
		},
		{
			name: "url com inicio e fim",
			opts: []*discordgo.ApplicationCommandInteractionDataOption{
				strOption("url", "https://youtu.be/abc"),
				strOption("inicio", "01:00"),
				strOption("fim", "02:00"),
			},
			wantURL:   "https://youtu.be/abc",
			wantStart: "01:00",
			wantEnd:   "02:00",
		},
		{
			name: "entrada nil na slice é ignorada",
			opts: []*discordgo.ApplicationCommandInteractionDataOption{
				nil,
				strOption("url", "https://youtu.be/abc"),
			},
			wantURL: "https://youtu.be/abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, start, end, err := extractOptions(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("extractOptions err = %v, wantErr = %v", err, tt.wantErr)
			}
			if url != tt.wantURL || start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("extractOptions = (%q, %q, %q), esperado (%q, %q, %q)",
					url, start, end, tt.wantURL, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestMessageForError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string // substring esperada na mensagem
	}{
		{"demasiado grande", &downloader.DownloadError{Kind: downloader.ErrTooLarge, Msg: "o vídeo excede o limite de 20MB"}, "20MB"},
		// O downloader não conhece o /baixar: a dica do recorte é do bot.
		{"demasiado grande sugere o recorte", &downloader.DownloadError{Kind: downloader.ErrTooLarge, Msg: "excede"}, "`inicio`/`fim`"},
		{"live sem DVR", &downloader.DownloadError{Kind: downloader.ErrLiveNoDVR}, "live"},
		{"timeout", &downloader.DownloadError{Kind: downloader.ErrTimeout}, "demasiado tempo"},
		{"URL não suportado", &downloader.DownloadError{Kind: downloader.ErrUnsupportedURL}, "não suportado"},
		{"vídeo privado", &downloader.DownloadError{Kind: downloader.ErrPrivate}, "privado ou indisponível"},
		{"vídeo indisponível", &downloader.DownloadError{Kind: downloader.ErrUnavailable}, "privado ou indisponível"},
		{"login pedido", &downloader.DownloadError{Kind: downloader.ErrLoginRequired}, "DOTBOOT_COOKIES_FILE"},
		{"limite de pedidos", &downloader.DownloadError{Kind: downloader.ErrRateLimited}, "limitar os pedidos"},
		{"cancelado no encerramento", &downloader.DownloadError{Kind: downloader.ErrCanceled}, "a reiniciar"},
		{"desconhecido mostra o erro do yt-dlp", &downloader.DownloadError{
			Kind:   downloader.ErrUnknown,
			Output: "[generic] a\nERROR: [generic] Unable to download webpage: HTTP Error 403: Forbidden\n",
		}, "HTTP Error 403"},
		{"downloader desconhecido sem output", &downloader.DownloadError{Kind: downloader.ErrUnknown}, "Não foi possível"},
		{"erro interno não expõe detalhes", fmt.Errorf("falhou ao criar /tmp/dotboot-123: disco cheio"), "erro interno"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := messageForError(tt.err)
			if !strings.Contains(got, tt.want) {
				t.Errorf("messageForError(%v) = %q, devia conter %q", tt.err, got, tt.want)
			}
		})
	}
}

// Um erro de fora do downloader traz caminhos do servidor: não podem chegar
// ao canal.
func TestMessageForErrorNaoExpoeCaminhos(t *testing.T) {
	got := messageForError(fmt.Errorf("não foi possível criar /tmp/dotboot-123: disco cheio"))
	if strings.Contains(got, "/tmp") {
		t.Errorf("messageForError expôs um caminho interno: %q", got)
	}
}

func TestMessageForURLError(t *testing.T) {
	allowed := []string{"youtube.com", "x.com"}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"fora da allowlist lista os domínios", downloader.ValidatePublicURL("https://example.com/v", allowed), "youtube.com, x.com"},
		{"rede local", downloader.ValidatePublicURL("http://192.168.1.1/", nil), "não é um site público"},
		{"mal formado", downloader.ValidatePublicURL("youtube.com/sem-esquema", nil), "URL inválido"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := messageForURLError(tt.err, allowed); !strings.Contains(got, tt.want) {
				t.Errorf("messageForURLError(%v) = %q, devia conter %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestUserLimiter(t *testing.T) {
	l := newUserLimiter(2)

	if !l.acquire("ana") || !l.acquire("ana") {
		t.Fatal("os dois primeiros pedidos da ana deviam passar")
	}
	if l.acquire("ana") {
		t.Error("o terceiro pedido da ana devia ser recusado")
	}
	// O limite é por pessoa: os pedidos da ana não contam para o rui.
	if !l.acquire("rui") {
		t.Error("o rui devia ter lugar")
	}

	l.release("ana")
	if !l.acquire("ana") {
		t.Error("depois de um release a ana devia voltar a ter lugar")
	}

	// Libertar tudo não deixa entradas a zero esquecidas no mapa.
	l.release("ana")
	l.release("ana")
	l.release("rui")
	if len(l.active) != 0 {
		t.Errorf("active = %v, esperado vazio", l.active)
	}
}

func TestInteractionUserID(t *testing.T) {
	inGuild := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Member: &discordgo.Member{User: &discordgo.User{ID: "1", Username: "ana"}},
	}}
	inDM := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		User: &discordgo.User{ID: "2", Username: "rui"},
	}}
	empty := &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{}}

	if got := interactionUserID(inGuild); got != "1" {
		t.Errorf("servidor: interactionUserID = %q, esperado 1", got)
	}
	if got := interactionUserID(inDM); got != "2" {
		t.Errorf("DM: interactionUserID = %q, esperado 2", got)
	}
	if got := interactionUserID(empty); got != "" {
		t.Errorf("sem utilizador: interactionUserID = %q, esperado vazio", got)
	}
	if got := interactionUser(empty); got != "desconhecido" {
		t.Errorf("sem utilizador: interactionUser = %q, esperado desconhecido", got)
	}
}

func TestDisplayMB(t *testing.T) {
	// 20MB menos a margem do envelope continua a ser anunciado como 20MB.
	if got := displayMB(20*mib - uploadOverhead); got != 20 {
		t.Errorf("displayMB = %d, esperado 20", got)
	}
	if got := displayMB(100*mib - uploadOverhead); got != 100 {
		t.Errorf("displayMB = %d, esperado 100", got)
	}
}
