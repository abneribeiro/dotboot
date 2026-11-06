package downloader

import (
	"fmt"
	"strconv"
)

// Section is a trim with start and end times as MM:SS or HH:MM:SS, already
// validated by the caller (see ValidateRange).
type Section struct {
	Start string
	End   string
}

// Options describes one yt-dlp invocation (see BuildArgs).
type Options struct {
	// URLs are the links to download, passed as the last arguments.
	URLs []string

	// BatchFile is a file with one URL per line (--batch-file).
	BatchFile string

	// OutputTemplate is the full -o value, built by the caller.
	OutputTemplate string

	// Format is the -f value; empty omits the flag (yt-dlp's default is the
	// best quality).
	Format string

	// FormatSort is the -S/--format-sort value (see QualitySort and
	// MP4Sort); empty omits the flag.
	FormatSort string

	// ExtractAudio, when set (e.g. "mp3", "m4a"), keeps only the audio in
	// that format (-x --audio-format). Without video, MergeFormat and the MP4
	// guarantee do not apply.
	ExtractAudio string

	// EmbedMetadata embeds title, artist and cover art: it is what makes an
	// MP3 show up with a name and a cover on a phone.
	EmbedMetadata bool

	// CookiesFile passes --cookies: a Netscape-format file with a browser
	// session, which gets past YouTube's "confirm you're not a bot" check,
	// age restrictions and content that requires a login.
	CookiesFile string

	// JSRuntimes passes --js-runtimes (e.g. "node"). Empty keeps yt-dlp's
	// default, Deno, the runtime that solves the JavaScript challenges
	// YouTube protects its formats with.
	JSRuntimes string

	// NoPlaylist downloads only the video when the link points at a video
	// inside a playlist (used for trims).
	NoPlaylist bool

	// Section adds --download-sections with --force-keyframes-at-cuts.
	Section *Section

	// MergeFormat forces the output container (e.g. "mp4").
	MergeFormat string

	// PrintToFile, when set, makes yt-dlp write each final file path to that
	// file (--print-to-file after_move:filepath). It implies --no-simulate:
	// without it, --print-to-file would cancel the download.
	PrintToFile string

	// ArchiveFile enables --download-archive: videos already recorded are
	// skipped on later runs, which is what makes a rerun resume a playlist.
	ArchiveFile string

	// Continue resumes .part files left by interrupted runs.
	Continue bool

	// IgnoreErrors keeps going through a playlist when one video fails.
	IgnoreErrors bool

	// Progress, when set, receives yt-dlp's progress and phase changes (see
	// ParseLine); BuildArgs adds the --progress-template that produces them.
	// Progress lines never reach Log.
	Progress func(Progress)

	// Log, when set, receives every other line yt-dlp prints, on stdout and
	// stderr, as it runs.
	Log func(line string)
}

// QualitySort turns a requested quality into yt-dlp's -S. "" or "best"
// return empty (yt-dlp already defaults to the best quality). An integer
// (e.g. "1080", "720") returns "res:N", which is a cap with a fallback:
// yt-dlp picks the highest resolution not above N and, only when there is
// none, the smallest one above.
//
// yt-dlp's "res" is the SHORT side of the video, which is why there is no
// -f [height<=N] filter: a vertical 720p video (Shorts, TikTok, Reels) is
// 720x1280, and filtering on height would throw it away and deliver
// 360x640. Sites that do not expose the resolution (e.g. Instagram) still
// work: without the value, the field just does not break ties.
func QualitySort(q string) (string, error) {
	if q == "" || q == "best" {
		return "", nil
	}
	n, err := strconv.Atoi(q)
	if err != nil || n < 1 {
		return "", fmt.Errorf("invalid quality %q (use \"best\" or a resolution such as 1080 or 720)", q)
	}
	return fmt.Sprintf("res:%d", n), nil
}

// mp4CodecSort makes yt-dlp prefer SDR and H.264 + AAC, the only pair that
// iPhones and Android phones play natively. vcodec and acodec are the fields
// of yt-dlp's own "-t mp4" preset. hdr:sdr comes first because yt-dlp
// prefers HDR by default (hdr:12): an HDR VP9 converted to 8-bit H.264 looks
// washed out, without the HDR metadata a player needs to show it right.
const mp4CodecSort = "hdr:sdr,vcodec:h264,acodec:aac"

// MP4Sort adds the codec preference to a resolution sort. An empty resSort
// means "the highest resolution available".
//
// The resolution ALWAYS comes first: asking for 2160 must give 2160p even if
// that size only exists in AV1 (EnsureMP4 converts it afterwards). With
// vcodec first, a 480p H.264 would beat a 2160p AV1 and the requested
// quality would be silently ignored.
//
// As a tie-breaker, though, vcodec is gold: between two formats of the same
// size it picks H.264, which needs no re-encoding. That is what makes the
// common path (YouTube up to 1080p, Instagram, TikTok, X) come out as MP4 at
// no CPU cost.
func MP4Sort(resSort string) string {
	if resSort == "" {
		resSort = "res"
	}
	return resSort + "," + mp4CodecSort
}

// BuildArgs builds yt-dlp's argument list from the options. It is pure so it
// can be tested without running yt-dlp.
func BuildArgs(o Options) []string {
	var args []string

	if o.Format != "" {
		args = append(args, "-f", o.Format)
	}
	if o.FormatSort != "" {
		args = append(args, "-S", o.FormatSort)
	}
	if o.MergeFormat != "" {
		args = append(args, "--merge-output-format", o.MergeFormat)
	}
	if o.ExtractAudio != "" {
		// --audio-quality 0 is the codec's best VBR setting.
		args = append(args, "-x", "--audio-format", o.ExtractAudio, "--audio-quality", "0")
	}
	if o.EmbedMetadata {
		args = append(args, "--embed-metadata", "--embed-thumbnail")
	}
	if o.CookiesFile != "" {
		args = append(args, "--cookies", o.CookiesFile)
	}
	if o.JSRuntimes != "" {
		args = append(args, "--js-runtimes", o.JSRuntimes)
	}
	if o.NoPlaylist {
		args = append(args, "--no-playlist")
	}
	if o.OutputTemplate != "" {
		args = append(args, "-o", o.OutputTemplate)
	}
	if o.ArchiveFile != "" {
		args = append(args, "--download-archive", o.ArchiveFile)
	}
	if o.Continue {
		args = append(args, "--continue")
	}
	if o.IgnoreErrors {
		args = append(args, "--ignore-errors")
	}
	// --newline: one line per progress update instead of "\r", readable in
	// logs and by ParseLine.
	args = append(args, "--newline")
	if o.Progress != nil {
		args = append(args, "--progress-template", progressTemplate)
	}
	if o.BatchFile != "" {
		args = append(args, "--batch-file", o.BatchFile)
	}
	if o.Section != nil {
		section := fmt.Sprintf("*%s-%s", o.Section.Start, o.Section.End)
		args = append(args, "--download-sections", section, "--force-keyframes-at-cuts")
	}
	// --print-to-file implies --simulate; without --no-simulate yt-dlp would
	// write the path but download nothing.
	if o.PrintToFile != "" {
		args = append(args, "--print-to-file", "after_move:filepath", o.PrintToFile, "--no-simulate")
	}

	args = append(args, o.URLs...)
	return args
}
