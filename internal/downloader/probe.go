package downloader

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// probe runs ffprobe and returns the first line of its output.
func probe(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ffprobe failed: %w", err)
	}
	return strings.TrimSpace(strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]), nil
}

// ProbeDuration returns the file's duration in seconds.
func ProbeDuration(ctx context.Context, path string) (float64, error) {
	out, err := probe(ctx, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", path)
	if err != nil {
		return 0, err
	}
	d, err := strconv.ParseFloat(out, 64)
	if err != nil {
		return 0, fmt.Errorf("unreadable duration from ffprobe: %q", out)
	}
	return d, nil
}

// Describe summarizes a downloaded file for the user, e.g. "H.264 · 1920×1080"
// or "AAC" for audio. It returns "" when ffprobe cannot read the file.
func Describe(ctx context.Context, path string) string {
	video, err := probe(ctx, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height", "-of", "csv=p=0", path)
	if err == nil {
		if f := strings.Split(video, ","); len(f) == 3 && f[0] != "mjpeg" && f[0] != "png" {
			return fmt.Sprintf("%s · %s×%s", codecName(f[0]), f[1], f[2])
		}
	}
	audio, err := probe(ctx, "-v", "error", "-select_streams", "a:0",
		"-show_entries", "stream=codec_name", "-of", "csv=p=0", path)
	if err != nil || audio == "" {
		return ""
	}
	return codecName(audio)
}

// codecName turns an ffprobe codec name into the one people know.
func codecName(c string) string {
	switch c {
	case "h264":
		return "H.264"
	case "hevc":
		return "HEVC"
	case "av1":
		return "AV1"
	case "vp9":
		return "VP9"
	case "aac":
		return "AAC"
	case "mp3":
		return "MP3"
	case "opus":
		return "Opus"
	default:
		return strings.ToUpper(c)
	}
}

// FFmpegFraction reads one line of ffmpeg's "-progress pipe:1" output and
// returns the processed fraction of a file durationSec seconds long. Only
// out_time_us lines count (ffmpeg's out_time_ms is, for historical reasons,
// also in microseconds).
func FFmpegFraction(line string, durationSec float64) (float64, bool) {
	value, ok := strings.CutPrefix(strings.TrimSpace(line), "out_time_us=")
	if !ok || durationSec <= 0 {
		return 0, false
	}
	us, err := strconv.ParseFloat(value, 64)
	if err != nil || us < 0 {
		return 0, false
	}
	return min(us/1e6/durationSec, 1), true
}
