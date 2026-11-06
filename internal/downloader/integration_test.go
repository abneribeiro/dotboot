//go:build integration

// These tests run the real yt-dlp and ffmpeg, against the network. They stay
// out of "go test ./..." because they need the binaries and an internet
// connection:
//
//	make test-integration-docker   (inside the image)
package downloader

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testURLShort is a ~19 s video, to exercise the full path without
// downloading a whole film.
const testURLShort = "https://www.youtube.com/watch?v=C0DPdy98e4c"

// assertPlayableOnPhones checks that the file is 8-bit H.264 + AAC in an MP4
// with the index at the front: what iPhones, iOS Photos and Android read
// without conversion, and start showing before reading the whole file.
func assertPlayableOnPhones(t *testing.T, ctx context.Context, path string) {
	t.Helper()

	info, err := probeMedia(ctx, path)
	if err != nil {
		t.Fatalf("ffprobe failed on %s: %v", path, err)
	}
	vcodec, acodec, pixFmt := info.VCodec, info.ACodec, info.PixFmt
	if !mp4Compatible(filepath.Ext(path), vcodec, acodec, pixFmt) {
		t.Errorf("%s does not play on phones: ext=%s vcodec=%s acodec=%s pix_fmt=%s",
			filepath.Base(path), filepath.Ext(path), vcodec, acodec, pixFmt)
	}
	if fast, err := moovFirst(path); err != nil || !fast {
		t.Errorf("%s: moov comes after mdat (missing +faststart)", filepath.Base(path))
	}
	t.Logf("codecs: %s / %s (%s)", vcodec, acodec, pixFmt)
}

// makeTestVideo generates a local file with the requested codecs. It beats
// downloading a real video: no network, no dependence on what YouTube keeps
// serving, and exactly the combination under test.
func makeTestVideo(t *testing.T, ctx context.Context, path string, ffmpegArgs ...string) {
	t.Helper()

	args := []string{
		"-f", "lavfi", "-i", "testsrc=duration=3:size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=3",
	}
	args = append(args, ffmpegArgs...)
	args = append(args, "-y", path)

	if out, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("cannot generate %s: %v\n%s", path, err, out)
	}
}

// A compatible H.264/AAC MP4 with the index at the end (like trims, which
// yt-dlp re-encodes) only starts playing on an iPhone once fully read.
// EnsureMP4 remuxes it losslessly, in place.
func TestEnsureMP4MovesTheIndex(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	source := filepath.Join(t.TempDir(), "no-faststart.mp4")
	makeTestVideo(t, ctx, source, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac")
	if fast, err := moovFirst(source); err != nil || fast {
		t.Fatalf("ffmpeg without +faststart should put the index at the end (fast=%v, %v)", fast, err)
	}

	reencode := true
	got, err := EnsureMP4(ctx, source, ConvertHooks{Start: func(r bool, _ string) { reencode = r }})
	if err != nil {
		t.Fatalf("EnsureMP4 failed: %v", err)
	}
	if got != source || reencode {
		t.Errorf("expected an in-place remux: %s (re-encoded: %v)", got, reencode)
	}
	assertPlayableOnPhones(t, ctx, got)
}

// libx264 in yuv420p rejects odd widths and heights, which show up in
// hand-cropped videos (TikTok, Instagram). The conversion rounds them.
func TestEnsureMP4OddDimensions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	source := filepath.Join(t.TempDir(), "odd.webm")
	makeTestVideo(t, ctx, source, "-vf", "scale=321:241", "-c:v", "libvpx-vp9", "-b:v", "300k", "-c:a", "libopus")

	got, err := EnsureMP4(ctx, source, ConvertHooks{})
	if err != nil {
		t.Fatalf("EnsureMP4 failed with odd dimensions: %v", err)
	}
	assertPlayableOnPhones(t, ctx, got)
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height,profile", "-of", "csv=p=0", got).Output()
	if err != nil {
		t.Fatal(err)
	}
	if info := strings.TrimSpace(string(out)); info != "High,320,240" && info != "320,240,High" {
		t.Errorf("profile and size = %q, want High at 320x240", info)
	}
}

// The case -S cannot fix: VP9/Opus, which is what YouTube serves above
// 1080p. It also checks that ffmpeg's progress reaches the hooks.
func TestEnsureMP4ReencodesNonH264(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	source := filepath.Join(t.TempDir(), "vp9.webm")
	makeTestVideo(t, ctx, source, "-c:v", "libvpx-vp9", "-b:v", "300k", "-c:a", "libopus")

	var started, reencode bool
	var last float64
	got, err := EnsureMP4(ctx, source, ConvertHooks{
		Start:    func(r bool, _ string) { started, reencode = true, r },
		Progress: func(f float64) { last = f },
	})
	if err != nil {
		t.Fatalf("EnsureMP4 failed: %v", err)
	}
	assertPlayableOnPhones(t, ctx, got)
	if _, err := os.Stat(source); err == nil {
		t.Errorf("the original %s should have been removed", source)
	}
	if !started || !reencode {
		t.Errorf("Start should announce a re-encode (started=%v reencode=%v)", started, reencode)
	}
	if last < 0.9 {
		t.Errorf("progress should get close to 1, got %v", last)
	}
	if d := Describe(ctx, got); d != "H.264 · 320×240" {
		t.Errorf("Describe = %q", d)
	}
}

// The pixel format trap: a 10-bit source converted without -pix_fmt becomes
// H.264 High10, which has the right codecs and still does not open on an
// iPhone.
func TestEnsureMP4Reencodes10Bit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	source := filepath.Join(t.TempDir(), "10bit.mp4")
	makeTestVideo(t, ctx, source, "-c:v", "libx264", "-pix_fmt", "yuv420p10le", "-c:a", "aac")

	got, err := EnsureMP4(ctx, source, ConvertHooks{})
	if err != nil {
		t.Fatalf("EnsureMP4 failed: %v", err)
	}
	assertPlayableOnPhones(t, ctx, got)
}

// The cheap path: right codecs in the wrong container are remuxed in seconds,
// and a second run over a compliant file changes nothing.
func TestEnsureMP4RemuxesLosslessly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	source := filepath.Join(t.TempDir(), "source.mkv")
	makeTestVideo(t, ctx, source, "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac")

	got, err := EnsureMP4(ctx, source, ConvertHooks{})
	if err != nil {
		t.Fatalf("EnsureMP4 failed: %v", err)
	}
	if filepath.Ext(got) != ".mp4" {
		t.Errorf("EnsureMP4 = %q, want an .mp4", got)
	}
	if _, err := os.Stat(source); err == nil {
		t.Errorf("the original %s should have been removed", source)
	}
	assertPlayableOnPhones(t, ctx, got)

	again, err := EnsureMP4(ctx, got, ConvertHooks{})
	if err != nil || again != got {
		t.Errorf("second pass = %q, %v; want %q untouched", again, err, got)
	}
}

// A real download must feed the --progress-template lines to the callback:
// the progress bar depends on it.
func TestRunReportsProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dir := t.TempDir()
	var mu sync.Mutex
	var updates int
	var maxFrac float64
	err := Run(ctx, Options{
		URLs:           []string{testURLShort},
		FormatSort:     MP4Sort("res:360"),
		MergeFormat:    "mp4",
		OutputTemplate: filepath.Join(dir, "%(id)s.%(ext)s"),
		NoPlaylist:     true,
		Progress: func(p Progress) {
			mu.Lock()
			defer mu.Unlock()
			if f := p.Fraction(); f >= 0 {
				updates++
				maxFrac = max(maxFrac, f)
			}
		},
	})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if updates == 0 || maxFrac < 0.99 {
		t.Errorf("not enough progress: %d updates, max %v", updates, maxFrac)
	}

	files, _ := filepath.Glob(filepath.Join(dir, "*.mp4"))
	if len(files) != 1 {
		t.Fatalf("want one .mp4, got %v", files)
	}
	assertPlayableOnPhones(t, ctx, files[0])
	if d := Describe(ctx, files[0]); !strings.HasPrefix(d, "H.264 · ") {
		t.Errorf("Describe = %q", d)
	}
}
