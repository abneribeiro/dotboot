package downloader

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// timePattern valida formatos de tempo aceites pelo yt-dlp/ffmpeg.
// Aceita: MM:SS (ex: 01:30) ou HH:MM:SS (ex: 00:01:30).
var timePattern = regexp.MustCompile(`^(\d{1,2}:)?\d{1,2}:\d{2}$`)

// ValidateTime verifica se o tempo está no formato MM:SS ou HH:MM:SS.
func ValidateTime(s string) error {
	if !timePattern.MatchString(s) {
		return fmt.Errorf("formato de tempo inválido: %q (usa MM:SS ou HH:MM:SS)", s)
	}
	// Os segundos, e os minutos quando há horas, são campos de relógio: 01:75
	// passava o padrão e chegava ao yt-dlp como 2:15. Os minutos de um MM:SS
	// ficam livres — 75:00 é uma forma legítima de dizer 1h15.
	for _, p := range strings.Split(s, ":")[1:] {
		if n, _ := strconv.Atoi(p); n >= 60 {
			return fmt.Errorf("formato de tempo inválido: %q (minutos e segundos vão de 00 a 59)", s)
		}
	}
	return nil
}

// ValidateURL verifica se o URL é absoluto, com esquema http/https e host.
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL inválido: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("URL inválido: esquema %q não suportado (usa http ou https)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL inválido: falta o host")
	}
	return nil
}

// ErrNotPublic e ErrDomainNotAllowed distinguem as recusas de
// ValidatePublicURL de um URL simplesmente mal formado.
var (
	ErrNotPublic        = errors.New("o endereço não é público")
	ErrDomainNotAllowed = errors.New("domínio não permitido")
)

// localSuffixes são sufixos de nomes que só existem em redes privadas.
var localSuffixes = []string{"localhost", "local", "internal", "lan", "home.arpa", "intranet", "corp"}

// reservedPrefixes são gamas IPv4 que o netip considera unicast global mas que
// não são a Internet: "esta rede" (0.0.0.0/8, que no Linux chega ao próprio
// host), o CGNAT/Tailscale (100.64.0.0/10) e blocos reservados pelo IETF.
var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
}

// ValidatePublicURL junta ao ValidateURL as regras do bot, que descarrega URLs
// escolhidos por qualquer membro do servidor: recusa o próprio host e a rede
// local (localhost, 127.0.0.1, 192.168.x.x, o endpoint de metadados da cloud
// em 169.254.169.254…), que de outra forma ficavam ao alcance de quem escreve
// no Discord. Com allowed não vazio, só aceita esses domínios e os seus
// subdomínios.
//
// É uma verificação sintática: não resolve DNS, por isso um nome público que
// aponte para um IP privado passa. Para isso serve a allowlist.
func ValidatePublicURL(raw string, allowed []string) error {
	if err := ValidateURL(raw); err != nil {
		return err
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL inválido: %w", err)
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")

	if !isPublicHost(host) {
		return fmt.Errorf("%w: %s", ErrNotPublic, host)
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, domain := range allowed {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrDomainNotAllowed, host)
}

// isPublicHost decide se um host pode ser um site da Internet. Um IP tem de
// ser unicast global e não privado; um nome tem de ter pelo menos um ponto e
// não pode acabar num sufixo de rede local.
func isPublicHost(host string) bool {
	if host == "" {
		return false
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return isPublicAddr(addr)
	}

	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	// O resolver aceita 127.1, 0177.0.0.1 ou 0x7f.1 como 127.0.0.1, formas que
	// o netip recusa. Nenhum domínio de topo é numérico, por isso um nome que
	// acabe num rótulo numérico é sempre um IP disfarçado.
	if isNumericLabel(labels[len(labels)-1]) {
		return false
	}
	for _, suffix := range localSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return false
		}
	}
	return true
}

func isPublicAddr(addr netip.Addr) bool {
	// ::ffff:127.0.0.1 é o 127.0.0.1 escrito em IPv6.
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, p := range reservedPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// isNumericLabel reporta se um rótulo DNS é um número decimal ou hexadecimal
// (0x…), que o resolver interpreta como parte de um IPv4.
func isNumericLabel(label string) bool {
	if strings.HasPrefix(strings.ToLower(label), "0x") {
		return true
	}
	return label != "" && strings.Trim(label, "0123456789") == ""
}

// parseTime converte um tempo já validado em segundos.
func parseTime(s string) int {
	parts := strings.Split(s, ":")
	total := 0
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		total = total*60 + n
	}
	return total
}

// ValidateRange verifica o formato de ambos os tempos e que o início é
// anterior ao fim. Sem esta verificação um recorte invertido chegaria ao
// yt-dlp como "*02:00-01:00" e produziria um ficheiro vazio.
func ValidateRange(start, end string) error {
	if err := ValidateTime(start); err != nil {
		return err
	}
	if err := ValidateTime(end); err != nil {
		return err
	}
	if parseTime(start) >= parseTime(end) {
		return fmt.Errorf("o início (%s) tem de ser anterior ao fim (%s)", start, end)
	}
	return nil
}
