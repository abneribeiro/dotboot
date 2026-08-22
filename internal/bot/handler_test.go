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
		{"live sem DVR", &downloader.DownloadError{Kind: downloader.ErrLiveNoDVR}, "live"},
		{"timeout", &downloader.DownloadError{Kind: downloader.ErrTimeout}, "demasiado tempo"},
		{"URL não suportado", &downloader.DownloadError{Kind: downloader.ErrUnsupportedURL}, "não suportado"},
		{"vídeo privado", &downloader.DownloadError{Kind: downloader.ErrPrivate}, "privado ou indisponível"},
		{"vídeo indisponível", &downloader.DownloadError{Kind: downloader.ErrUnavailable}, "privado ou indisponível"},
		{"erro genérico", fmt.Errorf("algo correu mal"), "algo correu mal"},
		{"downloader desconhecido", &downloader.DownloadError{Kind: downloader.ErrUnknown}, "Não foi possível"},
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
