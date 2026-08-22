package bot

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestLimitForTier(t *testing.T) {
	tests := []struct {
		name string
		tier discordgo.PremiumTier
		want int64
	}{
		// Desde 13-08-2026 o limite gratuito é 20MB (era 10).
		{"sem boost", discordgo.PremiumTierNone, 20 * mib},
		{"nível 1 partilha o limite gratuito", discordgo.PremiumTier1, 20 * mib},
		{"nível 2", discordgo.PremiumTier2, 50 * mib},
		{"nível 3", discordgo.PremiumTier3, 100 * mib},
		{"tier desconhecido cai no gratuito", discordgo.PremiumTier(99), 20 * mib},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := limitForTier(tt.tier); got != tt.want {
				t.Errorf("limitForTier(%v) = %d, esperado %d", tt.tier, got, tt.want)
			}
		})
	}
}

func TestApplyCeiling(t *testing.T) {
	tests := []struct {
		name       string
		guildLimit int64
		ceilingMB  int
		want       int64
	}{
		{"sem teto usa o limite do servidor", 20 * mib, 0, 20*mib - uploadOverhead},
		{"teto mais baixo ganha", 100 * mib, 25, 25*mib - uploadOverhead},
		// Um teto configurado nunca pode AUMENTAR o que o Discord aceita.
		{"teto mais alto não aumenta o limite", 20 * mib, 500, 20*mib - uploadOverhead},
		{"teto igual ao limite", 50 * mib, 50, 50*mib - uploadOverhead},
		{"teto de 1MB", 20 * mib, 1, 1*mib - uploadOverhead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyCeiling(tt.guildLimit, tt.ceilingMB)
			if got != tt.want {
				t.Errorf("applyCeiling(%d, %d) = %d, esperado %d", tt.guildLimit, tt.ceilingMB, got, tt.want)
			}
			if got > tt.guildLimit {
				t.Errorf("applyCeiling devolveu %d, acima do limite do servidor %d", got, tt.guildLimit)
			}
			if got < 1 {
				t.Errorf("applyCeiling devolveu %d; o limite tem de ser sempre positivo", got)
			}
		})
	}
}

// A margem do envelope multipart é o que evita que um ficheiro exatamente no
// limite seja recusado com 413.
func TestApplyCeilingReservaMargem(t *testing.T) {
	if got := applyCeiling(20*mib, 0); got >= 20*mib {
		t.Errorf("applyCeiling = %d, devia ficar abaixo de %d", got, 20*mib)
	}
}
