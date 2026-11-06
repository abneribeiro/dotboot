# dotboot

A command-line tool that downloads videos and music from YouTube, Instagram,
X, TikTok, SoundCloud and every other site [yt-dlp](https://github.com/yt-dlp/yt-dlp)
supports, in a format that plays everywhere: iPhone, Android, Windows, macOS
and WhatsApp, with no extra app.

```
$ dotboot video -q 1080 https://youtu.be/xxxx

 dotboot  video · up to 1080p
⣾  Downloading  Lil Peep - Falling Down [-jRKsiAOAA8].mp4
████████████████████████████░░░░░░░░░░░░░░░░░░░░   61%  3.1 MB/s  0:12 left

✓ Lil Peep - Falling Down [-jRKsiAOAA8].mp4  H.264 · 1920×1080
Saved to ~/Videos
```

## Features

- **Video as MP4 with H.264 + AAC**, the pair every phone and computer plays
  natively. When a site only has VP9/AV1 (YouTube above 1080p, some
  Instagram reels), dotboot converts it, and says so.
- **Audio as MP3 or M4A** with title, artist and cover art.
- **Best quality by default**, or a maximum resolution. The resolution is the
  short side, so 720 on a vertical video gives 720×1280.
- **Playlists, channels, several URLs or a batch file**, and trims
  (`--start 01:30 --end 02:00`).
- **Resumable**: run the same command again and it skips what was downloaded
  and resumes what was interrupted, conversions included. Ctrl+C keeps the
  partial files.
- **A live progress view**, with speed and time left. When the output is not
  a terminal (pipes, cron), it prints plain lines instead.

## Install

dotboot needs [yt-dlp](https://github.com/yt-dlp/yt-dlp), FFmpeg and, for
YouTube, [Deno](https://deno.com). Every option below can install them for you.

### macOS and Linux (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotboot/main/scripts/install.sh | bash
```

The script downloads the latest release for your system, checks it against
`checksums.txt`, installs `dotboot` into `~/.local/bin`, offers to install
yt-dlp, Deno and FFmpeg, and finishes with `dotboot doctor`. Run it again to
update. It takes a few variables:

```bash
# A specific version, another folder, and the dependencies without asking
curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotboot/main/scripts/install.sh |
  DOTBOOT_VERSION=v0.1.0 BINDIR=/usr/local/bin DOTBOOT_DEPS=1 bash
```

If the command is not found afterwards, add the folder to your `PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # or ~/.bashrc
```

### With Go

```bash
go install github.com/abneribeiro/dotboot/cmd/dotboot@latest
```

This needs Go 1.26+ and puts the binary in `$(go env GOPATH)/bin`. Install the
tools separately (`brew install yt-dlp ffmpeg deno` on macOS), then run
`dotboot doctor`.

### Windows

Download `dotboot_<version>_windows_amd64.zip` (or `arm64`) from the
[Releases](https://github.com/abneribeiro/dotboot/releases/latest) page and put
`dotboot.exe` in a folder on your `PATH`. Install the tools with
`winget install yt-dlp.yt-dlp Gyan.FFmpeg DenoLand.Deno`. Under WSL, use the
macOS and Linux command instead.

### From source, natively or with Docker

```bash
git clone https://github.com/abneribeiro/dotboot
cd dotboot
make setup
```

`make setup` asks how to install, sets the output folder and runs
`dotboot doctor`. There are two ways:

| | Docker | Native |
|---|---|---|
| Command | `make docker-build && make install-docker` | `make deps && make install` |
| Needs | Docker | Go 1.26+ (`make deps` installs yt-dlp, deno and ffmpeg) |
| Size | ~1GB (mostly ffmpeg) | the binary plus yt-dlp, deno and ffmpeg |

Both install the `dotboot` command into `~/.local/bin` (change it with
`make install PREFIX=/usr/local`).

With Docker, `dotboot` is a small script (`scripts/dotboot-docker`) that runs
the `dotboot:latest` image as your user with your home mounted at the same
path, so files are yours and `~`, absolute paths and the settings work as
with the native install. It also mounts the configured output folder and, on
WSL, the Windows drives (`/mnt/c`…).

### Uninstall

Delete the binary (`rm ~/.local/bin/dotboot`, or `make uninstall`) and,
optionally, the settings in `~/.config/dotboot`.

## Usage

```
dotboot video [flags] <url>...    Download videos as MP4
dotboot audio [flags] <url>...    Download audio as MP3 or M4A
dotboot doctor                    Check that yt-dlp, ffmpeg and Deno are installed
dotboot config [key] [value]      Show or change the settings
dotboot version                   Print the version
dotboot help <command>            Show a command's flags
dotboot <url>                     Same as dotboot video <url>
```

Flags can go before or after the URLs. In zsh, quote URLs that contain `?`.

```bash
# Best quality, as MP4
dotboot video "https://youtube.com/watch?v=xxxx"

# A whole playlist, up to 720p, into ~/Videos/lessons
dotboot video -q 720 -o ~/Videos/lessons "https://youtube.com/playlist?list=yyyy"

# A trim (the file name carries the interval)
dotboot video --start 01:30 --end 02:00 https://x.com/user/status/123

# Keep the site's own format (.webm/.mkv) instead of MP4
dotboot video --original "https://youtube.com/watch?v=xxxx"

# Audio, as M4A with cover art
dotboot audio -f m4a -o music "https://youtube.com/watch?v=xxxx"

# A file with one URL per line
dotboot video -a urls.txt
```

### Video flags

| Flag | Meaning |
|---|---|
| `-q, --quality <res>` | `best` (default) or a maximum: `2160`, `1440`, `1080`, `720`, `480` |
| `-o, --output <dir>` | Output folder; a relative one goes inside the configured folder |
| `--original` | Keep the site's format instead of MP4 |
| `--start <time>`, `--end <time>` | Trim, as `MM:SS` or `HH:MM:SS` (one URL only) |
| `-a, --batch <file>` | Read URLs from a file |
| `--cookies <file>` | Netscape cookies for videos behind a login or a bot check |

`dotboot audio` takes the same flags except `--quality` and `--original`,
plus `-f, --format mp3|m4a` (default `mp3`).

### Why MP4 with H.264

yt-dlp prefers AV1/VP9 + Opus, which come as `.webm`/`.mkv` files an iPhone
does not open. dotboot guarantees an MP4 in two layers:

1. **It asks for the right formats**: SDR, H.264 and AAC. On YouTube up to
   1080p, Instagram, TikTok and X this usually needs no conversion.
2. **It checks what arrived and fixes it**: VP9/AV1 is converted to H.264
   High, 8-bit 4:2:0, with even dimensions; every MP4 gets its index at the
   front (*faststart*) so an iPhone starts playing right away.

> **Tip**: 1080p is YouTube's sweet spot, the highest resolution without a
> conversion. Above it the conversion takes a while (dotboot shows the
> progress).

## Settings

```bash
dotboot config                       # show every setting
dotboot config output ~/Videos       # default output folder
dotboot config cookies ~/cookies.txt # cookies for every download
dotboot config cookies ""            # clear a setting
```

Settings live in `~/.config/dotboot/config.env`. A `.env` in the current
folder and environment variables work too, and environment variables always
win.

| Variable | Meaning | Default |
|---|---|---|
| `DOTBOOT_OUTPUT_DIR` | Output folder when `-o` is not given, and the root of a relative `-o` | current folder |
| `DOTBOOT_COOKIES_FILE` | Netscape cookies file for sites that need a login | none |
| `DOTBOOT_TEMP_DIR` | Temporary files | the system's |
| `DOTBOOT_JS_RUNTIMES` | Force yt-dlp's JavaScript runtime (e.g. `node`) | deno, or node |
| `DOTBOOT_LOG_LEVEL` | `debug`, `info`, `warn` or `error` | `info` |

A leading `~` is expanded in every path. See `.env.example`.

## Troubleshooting

Start with `dotboot doctor`: it says what is missing, whether yt-dlp is out of
date, and how to fix it.

- **"Sign in to confirm you're not a bot" / login required**: YouTube
  distrusts some IPs. Export the cookies of a browser where you are logged in
  (for example with the "Get cookies.txt LOCALLY" extension) and set them with
  `dotboot config cookies <file>`, or pass `--cookies`.
- **A site that used to work stopped working**: yt-dlp is out of date. Docker:
  `make update-ytdlp`; native: `yt-dlp -U`.
- **The conversion at the end takes long**: the source had no H.264 (YouTube
  above 1080p). Pick `-q 1080`, or `--original`.
- **`dotboot: command not found`**: `~/.local/bin` is not on your `PATH` (see
  Install).

## Development

```bash
make run ARGS="video <url>"   # run from source
make check                    # gofmt, vet and tests with -race (what CI runs)
make test-integration-docker  # real yt-dlp and ffmpeg, inside the image
make release-snapshot         # binaries for every platform in dist/
make release VERSION=v1.0.0   # tag it; GitHub Actions publishes the release
```

```
cmd/dotboot/          Entry point: configuration, signals, version
internal/cli/         Commands and flags
internal/ui/          The look: Bubble Tea progress view and Lip Gloss output
internal/jobs/        Engine: validates, runs and reports a download
internal/downloader/  yt-dlp and ffmpeg (download, MP4 guarantee)
internal/doctor/      Environment checks
internal/config/      Settings
internal/procio/      Process output helpers
scripts/              Setup, dependencies, Docker wrapper, release install
```

The image's yt-dlp is pinned in the `Dockerfile` and verified against the
release checksum; `make update-ytdlp` moves it to the latest version.
Dependabot keeps Go, the base images and the actions up to date.

## License

[MIT](LICENSE).
