package downloader

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClassify(t *testing.T) {
	runErr := fmt.Errorf("exit status 1")

	tests := []struct {
		name   string
		output string
		ctxErr error
		want   ErrorKind
	}{
		{"live without DVR", "ERROR: this video does not allow DVR", nil, ErrLiveNoDVR},
		{"not a video", "ERROR: yt-dlp: this URL is not a video", nil, ErrLiveNoDVR},
		{"live not started", "ERROR: This live event will begin in 2 hours", nil, ErrLiveNoDVR},
		{"private video", "ERROR: Private video. Sign in if you've been granted access", nil, ErrPrivate},
		{"unavailable video", "ERROR: Video unavailable", nil, ErrUnavailable},
		{"unsupported URL", "ERROR: Unsupported URL: https://example.com", nil, ErrUnsupportedURL},
		{"cancellation is not a failure", "ERROR: Interrupted by user", context.Canceled, ErrCanceled},
		{"youtube bot check", "ERROR: [youtube] abc: Sign in to confirm you’re not a bot. Use --cookies-from-browser or --cookies for the authentication.", nil, ErrLoginRequired},
		{"age restriction", "ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users.", nil, ErrLoginRequired},
		{"instagram login", "ERROR: [Instagram] abc: Requested content is not available, rate-limit reached or login required.", nil, ErrLoginRequired},
		// The private-video message also says "Sign in": it must stay private.
		{"private with sign in", "ERROR: [youtube] abc: Private video. Sign in if you've been granted access to this video", nil, ErrPrivate},
		{"rate limited", "ERROR: unable to download webpage: HTTP Error 429: Too Many Requests", nil, ErrRateLimited},
		{"unknown error", "ERROR: something completely different", nil, ErrUnknown},
		{"empty output", "", nil, ErrUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.output, tt.ctxErr, runErr)
			if got.Kind != tt.want {
				t.Errorf("classify(%q).Kind = %v, want %v", tt.output, got.Kind, tt.want)
			}
			if !errors.Is(got, runErr) {
				t.Error("classify must keep the original error for Unwrap")
			}
		})
	}
}

func TestDownloadErrorMessage(t *testing.T) {
	kinds := []ErrorKind{ErrLiveNoDVR, ErrPrivate, ErrUnavailable, ErrUnsupportedURL,
		ErrLoginRequired, ErrRateLimited, ErrCanceled, ErrUnknown}
	for _, k := range kinds {
		if (&DownloadError{Kind: k}).Error() == "" {
			t.Errorf("kind %v has an empty message", k)
		}
	}
	// An unclassified failure shows yt-dlp's own reason, not "exit status 1".
	e := &DownloadError{Output: "ERROR: [generic] boom: the site exploded\n", Err: fmt.Errorf("exit status 1")}
	if e.Error() != "[generic] boom: the site exploded" {
		t.Errorf("Error() = %q", e.Error())
	}
	if (&DownloadError{Kind: ErrLoginRequired}).Hint() == "" {
		t.Error("a login failure must suggest cookies")
	}
}

func TestDownloadErrorDetail(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"last ERROR line wins", "ERROR: first\n[download] x\nERROR: second\n", "second"},
		{"no ERROR line", "WARNING: something\n", ""},
		{"indented ERROR", "  ERROR:   spaced out  \n", "spaced out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (&DownloadError{Output: tt.output}).Detail(); got != tt.want {
				t.Errorf("Detail() = %q, want %q", got, tt.want)
			}
		})
	}

	long := "ERROR: " + strings.Repeat("x", maxDetailLen*2)
	if got := (&DownloadError{Output: long}).Detail(); utf8.RuneCountInString(got) != maxDetailLen {
		t.Errorf("Detail() has %d characters, want %d", utf8.RuneCountInString(got), maxDetailLen)
	}
}

// Cutting by bytes could split an accented character and print invalid
// UTF-8 to the terminal.
func TestTruncateRunesKeepsCharactersWhole(t *testing.T) {
	got := truncateRunes(strings.Repeat("ã", 10), 5)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 5 || !strings.HasSuffix(got, "…") {
		t.Errorf("truncateRunes = %q", got)
	}
}
