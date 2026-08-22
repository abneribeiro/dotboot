package bot

import (
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// Limites de upload do Discord, em MiB. Desde 13-08-2026 o limite gratuito
// subiu de 10 para 20 MiB; os níveis de boost mantiveram-se.
const (
	limitFreeMiB  = 20  // sem boost e Nível 1 (e DMs, onde o bot não tem Nitro)
	limitTier2MiB = 50  // Boost Nível 2
	limitTier3MiB = 100 // Boost Nível 3

	// uploadOverhead reserva espaço para o envelope multipart e o JSON do
	// payload, que contam para o tamanho do pedido — um ficheiro com
	// exatamente o limite seria rejeitado com 413.
	uploadOverhead = 256 * 1024
)

const mib = 1024 * 1024

// limitForTier traduz o nível de boost de um servidor no limite de anexo que
// o Discord aceita nesse servidor, em bytes (ainda sem a margem do envelope).
func limitForTier(t discordgo.PremiumTier) int64 {
	switch t {
	case discordgo.PremiumTier2:
		return limitTier2MiB * mib
	case discordgo.PremiumTier3:
		return limitTier3MiB * mib
	default:
		// PremiumTierNone e PremiumTier1 partilham o limite gratuito.
		return limitFreeMiB * mib
	}
}

// guildTier obtém o nível de boost do servidor, preferindo o estado local
// (preenchido por GUILD_CREATE com IntentsGuilds) e caindo para o REST se
// o servidor ainda não estiver em cache.
func (b *Bot) guildTier(s *discordgo.Session, guildID string) discordgo.PremiumTier {
	if guildID == "" {
		return discordgo.PremiumTierNone
	}

	if g, err := s.State.Guild(guildID); err == nil && g != nil {
		return g.PremiumTier
	}

	g, err := s.Guild(guildID)
	if err != nil {
		b.log.Warn("não foi possível obter o nível de boost do servidor, a assumir o limite gratuito",
			"guild_id", guildID, "err", err)
		return discordgo.PremiumTierNone
	}
	return g.PremiumTier
}

// applyCeiling aplica o teto opcional de DOTBOOT_MAX_FILE_SIZE_MB e subtrai a
// margem do envelope multipart. Separada de uploadLimit para ser testável sem
// uma sessão do Discord.
func applyCeiling(guildLimit int64, ceilingMB int) int64 {
	limit := guildLimit
	if ceilingMB > 0 {
		if ceiling := int64(ceilingMB) * mib; ceiling < limit {
			limit = ceiling
		}
	}

	limit -= uploadOverhead
	if limit < 1 {
		limit = 1
	}
	return limit
}

// uploadLimit devolve o número de bytes de vídeo que cabem num upload neste
// servidor: o menor entre o limite do Discord para o nível de boost e o teto
// configurado, menos a margem do envelope.
func (b *Bot) uploadLimit(s *discordgo.Session, guildID string) int64 {
	tier := b.guildTier(s, guildID)
	limit := applyCeiling(limitForTier(tier), b.cfg.MaxFileSizeMB)

	slog.Debug("limite de upload calculado",
		"guild_id", guildID, "premium_tier", int(tier), "limit_bytes", limit)
	return limit
}
