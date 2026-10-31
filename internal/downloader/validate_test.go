package downloader

import "testing"

func TestValidateTime(t *testing.T) {
	// 75:00 is a valid MM:SS (1h15): minutes are only capped when there are
	// hours.
	for _, s := range []string{"01:30", "00:01:30", "1:30", "10:59:59", "0:00", "75:00"} {
		if err := ValidateTime(s); err != nil {
			t.Errorf("ValidateTime(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{"", "90", "1:2:3:4", "01:3", "abc", "1h30m", "-1:30", "01:300",
		// Clock seconds and minutes go from 00 to 59.
		"01:75", "1:60", "00:60:00", "01:00:99"} {
		if err := ValidateTime(s); err == nil {
			t.Errorf("ValidateTime(%q) = nil, want an error", s)
		}
	}
}

func TestValidateURL(t *testing.T) {
	for _, s := range []string{
		"https://youtube.com/watch?v=abc",
		"https://x.com/user/status/123",
		"http://instagram.com/p/xyz",
	} {
		if err := ValidateURL(s); err != nil {
			t.Errorf("ValidateURL(%q) = %v, want nil", s, err)
		}
	}
	for _, s := range []string{
		"",
		"youtube.com/watch?v=abc", // no scheme
		"ftp://example.com/video",
		"https://", // no host
		"not a url at all",
		"file:///etc/passwd",
	} {
		if err := ValidateURL(s); err == nil {
			t.Errorf("ValidateURL(%q) = nil, want an error", s)
		}
	}
}

func TestValidateRange(t *testing.T) {
	tests := []struct {
		start, end string
		wantErr    bool
	}{
		{"01:00", "02:00", false},
		{"00:01:00", "01:00:00", false},
		{"59:00", "01:00:00", false},
		{"01:00", "01:00", true},
		{"02:00", "01:00", true},
		{"01:00:00", "00:30:00", true},
		{"abc", "02:00", true},
		{"01:00", "xyz", true},
	}
	for _, tt := range tests {
		if err := ValidateRange(tt.start, tt.end); (err != nil) != tt.wantErr {
			t.Errorf("ValidateRange(%q, %q) = %v, want error: %v", tt.start, tt.end, err, tt.wantErr)
		}
	}
}
