package downloader

import (
	"fmt"
	"net/url"
	"regexp"
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
