package downloader

import (
	"fmt"
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
