package downloader

import "testing"

func TestValidateTime(t *testing.T) {
	valid := []string{"01:30", "00:01:30", "1:30", "10:59:59", "0:00"}
	for _, s := range valid {
		if err := ValidateTime(s); err != nil {
			t.Errorf("ValidateTime(%q) = %v, esperado nil", s, err)
		}
	}

	invalid := []string{"", "90", "1:2:3:4", "01:3", "abc", "1h30m", "-1:30", "01:300"}
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
