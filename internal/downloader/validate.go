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
