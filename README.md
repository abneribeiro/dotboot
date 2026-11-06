# dotboot

Download videos and music from YouTube, Instagram, X, TikTok, SoundCloud and
[every site yt-dlp supports](https://github.com/yt-dlp/yt-dlp/blob/master/supportedsites.md),
in a format that plays on iPhone, Android, Windows, macOS and WhatsApp.

```
$ dotboot video -q 1080 https://youtu.be/xxxx

 dotboot  video · up to 1080p
⣾  Downloading  Big Buck Bunny [aqz-KE-bpKQ].mp4
████████████████████████████░░░░░░░░░░░░░░░░░░░░   61%  3.1 MB/s  0:12 left

✓ Big Buck Bunny [aqz-KE-bpKQ].mp4  H.264 · 1920×1080
Saved to ~/Videos
```

- **MP4 with H.264 + AAC** by default, converted when a site only has VP9/AV1.
- **MP3 or M4A** with title, artist and cover art.
- **Playlists, batch files and trims**, best quality or a maximum resolution.
- **Resumable**: run the same command again to skip what is done and resume
  what was interrupted.

## Install

### macOS and Linux

```bash
curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotboot/main/scripts/install.sh | bash
```

Installs the latest release into `~/.local/bin` (checksum verified), offers to
install yt-dlp, FFmpeg and Deno, and runs `dotboot doctor`. Run it again to
update. Options, set before `bash`: `DOTBOOT_VERSION=v0.1.0`,
`BINDIR=/usr/local/bin`, `DOTBOOT_DEPS=1` (dependencies without asking).

If `dotboot` is not found, add `~/.local/bin` to your `PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # or ~/.bashrc
```

### Go

```bash
go install github.com/abneribeiro/dotboot/cmd/dotboot@latest
brew install yt-dlp ffmpeg deno   # or your package manager
```

### Windows

Download the `.zip` for your architecture from
[Releases](https://github.com/abneribeiro/dotboot/releases/latest), put
`dotboot.exe` on your `PATH`, and install the tools:

```powershell
winget install yt-dlp.yt-dlp Gyan.FFmpeg DenoLand.Deno
```

### Docker (nothing else on your machine)

The image brings yt-dlp, FFmpeg and Deno; `dotboot` becomes a small script
that runs it as your user, with your home mounted at the same path.

```bash
# macOS: OrbStack, the lightest Docker engine (free for personal use)
brew install --cask orbstack && open -a OrbStack
# Linux and WSL: Docker Engine (https://docs.docker.com/engine/install/)

git clone https://github.com/abneribeiro/dotboot && cd dotboot
make docker-build && make install-docker
```

The image takes about 1 GB. For an open-source engine on macOS, use Colima
instead (`brew install colima docker docker-buildx && colima start`); it only
shares your home, so save downloads inside it (e.g. `-o ~/Videos`).

### From source

```bash
git clone https://github.com/abneribeiro/dotboot && cd dotboot
make setup   # native (Go 1.26+) or Docker, then the output folder
```

**Uninstall**: `rm ~/.local/bin/dotboot`, and optionally `~/.config/dotboot`
(and `docker rmi dotboot:latest` for Docker).

## Usage

```
dotboot video [flags] <url>...    Download videos as MP4
dotboot audio [flags] <url>...    Download audio as MP3 or M4A
dotboot doctor                    Check yt-dlp, FFmpeg and Deno
dotboot config [key] [value]      Show or change the settings
dotboot help <command>            Show a command's flags
dotboot <url>                     Same as dotboot video <url>
```

```bash
dotboot video "https://youtube.com/watch?v=xxxx"                     # best quality
dotboot video -q 720 -o lessons "https://youtube.com/playlist?list=yyyy"
dotboot video --start 01:30 --end 02:00 https://x.com/user/status/123
dotboot audio -f m4a "https://youtube.com/watch?v=xxxx"
dotboot video -a urls.txt                                            # one URL per line
```

| Flag | Meaning |
|---|---|
| `-q, --quality <res>` | `best` (default) or a maximum short side: `2160`, `1080`, `720`… |
| `-o, --output <dir>` | Output folder; a relative one goes inside the configured folder |
| `--original` | Keep the site's format (`.webm`/`.mkv`) instead of MP4 |
| `--start`, `--end <time>` | Trim, `MM:SS` or `HH:MM:SS`, one URL only |
| `-a, --batch <file>` | Read URLs from a file |
| `--cookies <file>` | Netscape cookies for logins and bot checks |
| `-f, --format mp3\|m4a` | Audio only; default `mp3` |

Flags can go before or after the URLs. In zsh, quote URLs that contain `?`.

**Why 1080p is the sweet spot**: up to 1080p YouTube serves H.264, so the MP4
needs no conversion. Above it only VP9/AV1 exist, and dotboot converts them,
which takes a while.

## Settings

```bash
dotboot config output ~/Videos         # default output folder
dotboot config cookies ~/cookies.txt   # cookies for every download
dotboot config cookies ""              # clear a setting
```

Stored in `~/.config/dotboot/config.env`. Environment variables, and a `.env`
in the current folder, take priority:

| Variable | Meaning | Default |
|---|---|---|
| `DOTBOOT_OUTPUT_DIR` | Output folder, and the root of a relative `-o` | current folder |
| `DOTBOOT_COOKIES_FILE` | Netscape cookies file | none |
| `DOTBOOT_TEMP_DIR` | Temporary files | the system's |
| `DOTBOOT_JS_RUNTIMES` | yt-dlp's JavaScript runtime, e.g. `node` | deno, else node |
| `DOTBOOT_LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |

## Troubleshooting

Run `dotboot doctor` first: it lists what is missing or outdated and how to
fix it.

- **"Sign in to confirm you're not a bot"**: export your browser's cookies
  (e.g. the "Get cookies.txt LOCALLY" extension) and run
  `dotboot config cookies <file>`.
- **A site stopped working**: update yt-dlp (`yt-dlp -U`, or rerun the
  installer with `DOTBOOT_DEPS=1`).
- **`dotboot: command not found`**: add `~/.local/bin` to your `PATH`.

## Development

```bash
make run ARGS="video <url>"   # run from source
make check                    # gofmt, vet and tests with -race, as CI
make test-integration-docker  # real downloads inside the Docker image
make release VERSION=vX.Y.Z   # tag a release; GitHub Actions publishes it
```

`make` lists every target. Architecture and conventions are in
[AGENTS.md](AGENTS.md).

## License

[MIT](LICENSE)
