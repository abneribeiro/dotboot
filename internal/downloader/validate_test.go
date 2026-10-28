package downloader

import (
	"errors"
	"testing"
)

func TestValidateTime(t *testing.T) {
	// 75:00 é um MM:SS legítimo (1h15): os minutos só têm teto quando há horas.
	valid := []string{"01:30", "00:01:30", "1:30", "10:59:59", "0:00", "75:00"}
	for _, s := range valid {
		if err := ValidateTime(s); err != nil {
			t.Errorf("ValidateTime(%q) = %v, esperado nil", s, err)
		}
	}

	invalid := []string{"", "90", "1:2:3:4", "01:3", "abc", "1h30m", "-1:30", "01:300",
		// Segundos e minutos de relógio vão de 00 a 59.
		"01:75", "1:60", "00:60:00", "01:00:99"}
	for _, s := range invalid {
		if err := ValidateTime(s); err == nil {
			t.Errorf("ValidateTime(%q) = nil, esperado erro", s)
		}
	}
}

func TestValidateURL(t *testing.T) {
	valid := []string{
		"https://youtube.com/watch?v=abc",
		"https://x.com/user/status/123",
		"http://instagram.com/p/xyz",
		"https://www.facebook.com/watch?v=1",
	}
	for _, s := range valid {
		if err := ValidateURL(s); err != nil {
			t.Errorf("ValidateURL(%q) = %v, esperado nil", s, err)
		}
	}

	invalid := []string{
		"",
		"youtube.com/watch?v=abc", // sem esquema
		"ftp://example.com/video", // esquema não suportado
		"https://",                // sem host
		"http://",                 // sem host
		"not a url at all",
		"file:///etc/passwd",
	}
	for _, s := range invalid {
		if err := ValidateURL(s); err == nil {
			t.Errorf("ValidateURL(%q) = nil, esperado erro", s)
		}
	}
}

func TestValidateRange(t *testing.T) {
	tests := []struct {
		name    string
		start   string
		end     string
		wantErr bool
	}{
		{"intervalo normal", "01:00", "02:00", false},
		{"com horas", "00:01:00", "01:00:00", false},
		{"mistura de formatos", "59:00", "01:00:00", false},
		{"início igual ao fim", "01:00", "01:00", true},
		{"início depois do fim", "02:00", "01:00", true},
		{"início depois do fim com horas", "01:00:00", "00:30:00", true},
		{"formato inválido no início", "abc", "02:00", true},
		{"formato inválido no fim", "01:00", "xyz", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRange(tt.start, tt.end)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRange(%q, %q) = %v, wantErr = %v", tt.start, tt.end, err, tt.wantErr)
			}
		})
	}
}

func TestValidatePublicURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		allowed []string
		wantErr error // nil = aceite
	}{
		{"site público", "https://www.youtube.com/watch?v=abc", nil, nil},
		{"IP público", "http://8.8.8.8/video.mp4", nil, nil},
		{"host com ponto final", "https://youtu.be./abc", nil, nil},

		{"localhost", "http://localhost:8080/x", nil, ErrNotPublic},
		{"subdomínio de localhost", "http://app.localhost/x", nil, ErrNotPublic},
		{"loopback", "http://127.0.0.1/x", nil, ErrNotPublic},
		{"loopback abreviado", "http://127.1/x", nil, ErrNotPublic},
		{"loopback em decimal", "http://2130706433/x", nil, ErrNotPublic},
		{"loopback em hexadecimal", "http://0x7f.1/x", nil, ErrNotPublic},
		{"loopback em octal", "http://0177.0.0.1/x", nil, ErrNotPublic},
		{"loopback IPv6", "http://[::1]/x", nil, ErrNotPublic},
		{"loopback IPv4 em IPv6", "http://[::ffff:127.0.0.1]/x", nil, ErrNotPublic},
		{"metadados da cloud", "http://169.254.169.254/latest/meta-data", nil, ErrNotPublic},
		{"rede doméstica", "http://192.168.1.1/admin", nil, ErrNotPublic},
		{"rede privada 10/8", "http://10.0.0.5/", nil, ErrNotPublic},
		{"CGNAT/Tailscale", "http://100.100.100.100/", nil, ErrNotPublic},
		{"esta rede", "http://0.0.0.0:8080/", nil, ErrNotPublic},
		{"IPv6 local único", "http://[fd00::1]/", nil, ErrNotPublic},
		{"nome sem ponto", "http://router/x", nil, ErrNotPublic},
		{"sufixo .local", "http://nas.local/x", nil, ErrNotPublic},
		{"sufixo .internal", "http://metadata.google.internal/x", nil, ErrNotPublic},

		{"domínio permitido", "https://youtube.com/watch?v=abc", []string{"youtube.com"}, nil},
		{"subdomínio permitido", "https://m.youtube.com/watch?v=abc", []string{"youtube.com", "x.com"}, nil},
		{"domínio fora da lista", "https://example.com/v.mp4", []string{"youtube.com"}, ErrDomainNotAllowed},
		// "notyoutube.com" acaba em "youtube.com" mas não é um subdomínio.
		{"sufixo sem ponto não conta", "https://notyoutube.com/v", []string{"youtube.com"}, ErrDomainNotAllowed},
		// A allowlist não abre a porta à rede local.
		{"allowlist não aceita privados", "http://127.0.0.1/x", []string{"127.0.0.1"}, ErrNotPublic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePublicURL(tt.url, tt.allowed)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidatePublicURL(%q) = %v, esperado nil", tt.url, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidatePublicURL(%q) = %v, esperado %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

// Um URL mal formado continua a ser só "inválido", não "não público".
func TestValidatePublicURLMalFormado(t *testing.T) {
	err := ValidatePublicURL("ftp://example.com/x", nil)
	if err == nil || errors.Is(err, ErrNotPublic) || errors.Is(err, ErrDomainNotAllowed) {
		t.Errorf("ValidatePublicURL(ftp) = %v, esperado um erro de URL inválido", err)
	}
}

// Os TLDs com letras de a-f (.de, .be, .cafe) não podem ser confundidos com
// números hexadecimais.
func TestIsNumericLabel(t *testing.T) {
	for label, want := range map[string]bool{
		"1": true, "2130706433": true, "0x7f": true, "0X1": true,
		"de": false, "be": false, "cafe": false, "com": false, "": false,
	} {
		if got := isNumericLabel(label); got != want {
			t.Errorf("isNumericLabel(%q) = %v, esperado %v", label, got, want)
		}
	}
}
