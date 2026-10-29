# AGENTS.md — dotboot

## Project Overview

dotboot is a terminal tool, in Go, that downloads videos and audio with
`yt-dlp` and separates songs into stems with `audio-separator`. One binary,
two faces over the same engine:

- **TUI** (`dotboot` with no arguments, or `dotboot tui`) — Bubble Tea v2 +
  Lip Gloss v2 + Bubbles v2 + Huh v2: a menu, validated forms, a job queue
  with real progress (bytes, speed, ETA), per-job logs, cancel/retry,
  diagnostics and settings.
- **Subcommands** for scripts: `download` (video MP4/original or audio
  mp3/m4a, quality cap, playlists, batch files, trims, resumable), `stems`
  (6s/4s/voz presets, keep only some tracks, backing track without some),
  `doctor` and `version`.

Video output is guaranteed MP4/H.264 8-bit/AAC unless the original format is
requested. There is no Discord bot anymore (removed in favour of the TUI).

**Stack**: Go 1.26+ (built with 1.27), Charm v2 (`charm.land/...`), yt-dlp,
FFmpeg, Deno, audio-separator (PyTorch CPU), Docker

## Build Commands

`make` with no target lists everything. The common ones:

```bash
make setup            # Post-clone assistant: install (docker|native), config, doctor
make run              # Open the TUI from source (go run)
make build            # bin/dotboot with version ldflags
make install          # Native binary into ~/.local/bin (PREFIX=… to change)
make install-docker   # Docker wrapper (scripts/dotboot-docker) as `dotboot`
make deps             # yt-dlp + deno (checksum-verified) and ffmpeg on the host
make deps-stems       # audio-separator via uv (needs a C compiler)
make docker-build     # The single image dotboot:latest (~3GB, includes stems)
make config           # Edit ~/.config/dotboot/config.env
make update-ytdlp     # Pin the latest yt-dlp in the Dockerfile and rebuild
make release-snapshot # GoReleaser in Docker, all platforms into dist/
```

The module path is `github.com/abneribeiro/dotboot` (`go install …@latest`
works; with the repo private it needs `GOPRIVATE=github.com/abneribeiro/*`).

## Testing

Tests are table-driven and use only the standard `testing` package.

```bash
make check                     # gofmt + vet (incl. integration tag) + go test -race
make test-integration-docker   # real yt-dlp/ffmpeg/audio-separator inside dotboot:latest
```

- Pure logic is unit-tested: argument building, progress parsers (yt-dlp
  template, ffmpeg `out_time_us`, tqdm), error classification, validation,
  config and `config.Save`, stem selection, doctor with a fake system.
- `internal/jobs` runs whole jobs against **fake binaries** — small `sh`
  scripts placed on a test `PATH` that imitate yt-dlp, ffprobe, ffmpeg and
  audio-separator (progress lines, output files). Use this pattern for any
  new orchestration test: no network, no real tools.
- `internal/tui` drives the model directly: key messages, job events through
  the queue channel (`pump`), and assertions on `View()` — including that no
  line exceeds the terminal width at 60x20/80x24/160x50.
- Integration tests (`//go:build integration`) execute the real tools and the
  network; `make test-integration-docker` compiles them with `go test -c` and
  runs them inside the image, so the host needs only Docker.

## Linting & Formatting

No linter config. `make lint` = gofmt check + `go vet` (with and without the
integration tag). Shell scripts are checked with shellcheck in CI. All code
must be `gofmt`-compliant.

## Code Style Guidelines

### Architecture

```
cmd/dotboot/main.go    Entrypoint: config load (config.LoadFiles), slog,
                       signals (signal.NotifyContext lives ONLY here),
                       dispatch: no args/tui → TUI, download, stems, doctor,
                       version; binary checks; JS runtime pick.
internal/config/       Env-var configuration, strict validation, the global
                       ~/.config/dotboot/config.env and Save.
internal/procio/       LineWriter (splits on \n AND \r) and TailBuffer.
internal/downloader/   yt-dlp + ffmpeg. BuildArgs, QualitySort, MP4Sort,
                       ParseLine, FFmpegFraction, mp4Compatible, compatArgs
                       are pure; Run executes yt-dlp with progress callbacks;
                       EnsureMP4 enforces the output format (ConvertHooks);
                       DownloadAudio feeds stems.
internal/stems/        audio-separator + ffmpeg. Presets, Select,
                       SeparatorArgs, MixArgs, EncodeArgs, ToWAVArgs and
                       ParseTQDM are pure; Separate and Export execute.
internal/jobs/         THE ENGINE shared by CLI and TUI: NewDownload/NewStems
                       validate a request; Download.Run/Stems.Run execute it
                       and report jobs.Event through a Reporter.
internal/cli/          Subcommands: flag parsing → jobs → text reporter.
internal/tui/          The TUI: model.go (state/Update), views.go (render),
                       forms.go (huh), queue.go (job queue), theme.go.
internal/doctor/       Environment report (Collect for the TUI, Run for CLI).
scripts/               setup.sh, configure-env.sh, install-deps.sh,
                       dotboot-docker (wrapper), install.sh (releases).
```

Key invariants to preserve:

- **One engine, two faces**: validation and orchestration live in
  `internal/jobs`, never in `cli` or `tui`. Both build a `jobs.DownloadInput`
  / `jobs.StemsInput`, call `NewDownload`/`NewStems`, then `Run` with a
  Reporter. `downloader`, `stems` and `jobs` never write to the terminal and
  never import `cli`/`tui`.
- **Events, not output**: progress reaches callers as `jobs.Event` (Stage,
  Fraction/-1, Speed, ETA, Item, Index/Count, Log). An Event's empty fields
  mean "unchanged". Reporters may be called from several goroutines; the
  `reporterSink` serializes them.
- **Real progress, parsed purely**: yt-dlp gets `--newline` and, when
  `Options.Progress` is set, a `--progress-template` with a marker that
  `ParseLine` reads (template lines never reach the log). ffmpeg runs with
  `-progress pipe:1` (`FFmpegFraction`); audio-separator's tqdm bars are read
  by `ParseTQDM`. `procio.LineWriter` splits on `\r` too — tqdm and ffmpeg
  rewrite one line with carriage returns.
- **Output is always MP4 / H.264 8-bit / AAC** (video mode): `MP4Sort` asks
  yt-dlp to prefer those codecs (resolution must outrank `vcodec` in `-S`, or
  `-q 2160` would be ignored), and `EnsureMP4` probes and fixes what landed
  (above 1080p YouTube has no H.264). Every re-encode carries
  `-pix_fmt yuv420p`: a 10-bit source would otherwise become H.264 High10,
  which iPhones cannot play.
- **Resolution means the SHORT side**: `-q N` is only `-S res:N` (a cap with
  fallback), never an `-f [height<=N]` filter, which turned a vertical
  720x1280 Short into 360x640.
- **Pending conversions persist**: in MP4 mode the `--print-to-file` target
  is `<outdir>/.dotboot-pendentes.txt`. yt-dlp records the archive BEFORE
  `EnsureMP4` runs, so an interrupted conversion would otherwise leave a
  `.webm` forever. Deleted only when everything converted. Other modes use a
  temp file only to learn which files were produced.
- **Trim + archive don't mix**: a trim never writes the archive, forces
  `--no-playlist` and has the interval in the file name (`outputTemplate`).
  Audio uses its own archive (`.dotboot-archive-audio.txt`).
- **SIGINT, not SIGKILL**: `cmd.Cancel` sends `os.Interrupt` to
  yt-dlp/ffmpeg/audio-separator with a `WaitDelay`, so `.part` files survive
  and resume works. Cancelled jobs return `context.Canceled`; partial
  success returns an error wrapping `jobs.ErrPartial` plus the Result.
- **The TUI waits for its children**: `tui.Run` cancels running jobs on exit
  and waits (`queue.shutdown`) for them, draining the event channel —
  leaving earlier would orphan a yt-dlp/ffmpeg. The first ctrl+c with jobs
  running only warns. Jobs run one at a time (CPU-heavy, and parallel yt-dlp
  only annoys sites). slog is silenced while the TUI owns the screen.
- **Stems work dirs**: each song gets `<TempDir>/dotboot-stems-*`, always
  removed; the user's cookie file is copied into the work dir (yt-dlp
  rewrites it on exit).
- **`--print-to-file` implies `--simulate`**: `BuildArgs` always emits
  `--no-simulate` with it.

### Imports

Group imports in blocks separated by blank lines, each sorted alphabetically:
standard library, third-party (including `charm.land/...`), then internal
`github.com/abneribeiro/dotboot/...`.

### Formatting & Naming

gofmt; tabs; trailing commas in multi-line literals; ~120-char lines.
`PascalCase` exported, `camelCase` everything else including constants (never
`SCREAMING_SNAKE_CASE`); acronyms uppercase (`URL`, `ID`); lowercase file
names.

### Error Handling

1. Check errors immediately; wrap with `fmt.Errorf("…: %w", err)`.
2. `internal/downloader` returns `*DownloadError` with a `Kind`
   (`ErrTimeout`, `ErrPrivate`, `ErrLoginRequired`, `ErrRateLimited`,
   `ErrCanceled`, …); classify with `errors.As` + switch, never by substring
   of translated messages. `Detail()` gives yt-dlp's last `ERROR:` line.
3. Never discard errors silently; intentionally ignored ones are assigned to
   `_` with a reason (e.g. the optional `.env`/`config.env` loads).
4. CLI sentinels: `cli.ErrUsage` (exit 2), `cli.ErrPartial` (exit 1),
   `cli.ErrInterrupted` (exit 130); a missing binary is `errMissingDeps`.

### Logging

`log/slog` only (no `log.Printf`), level from `DOTBOOT_LOG_LEVEL`, default
logger set in `main`. User-facing progress goes through `jobs.Event`, not
slog.

### Comments

- Written primarily in **European Portuguese** (project convention); user
  facing text too. Some technical comments may be in English.
- `//` line comments only, above the declaration they describe.

## Configuration

Env vars parsed in `internal/config` (see `.env.example`):
`DOTBOOT_OUTPUT_DIR`, `DOTBOOT_TEMP_DIR`, `DOTBOOT_COOKIES_FILE`,
`DOTBOOT_JS_RUNTIMES`, `DOTBOOT_MODELS_DIR`, `DOTBOOT_LOG_LEVEL`. Priority:
environment > `./.env` > `~/.config/dotboot/config.env` (godotenv never
overrides). The TUI's Settings and `scripts/configure-env.sh` write the
config file; `config.Save` rewrites only the keys asked and keeps 0600. A
leading `~` is expanded in every path.

`DOTBOOT_OUTPUT_DIR` is the **base path**: used when `-o` is absent and the
root of a relative `-o`; an absolute `-o` ignores it.

## Environment & Dependencies

- **Runtime deps**: `yt-dlp`, `ffmpeg`, `ffprobe` (download), plus a JS
  runtime for YouTube — Deno (yt-dlp's default) or node (`--js-runtimes node`,
  picked automatically when it is the only one). `stems` needs `ffmpeg` and
  `audio-separator` (+ `yt-dlp` for URLs). The TUI opens without them; the
  Diagnostics screen says what is missing.
- **Go deps**: Charm v2 for the TUI, godotenv, `charmbracelet/x` helpers.
  CLI parsing stays on stdlib `flag` (`parseInterspersed` allows flags after
  positional args).

## Docker

One image, `dotboot:latest` (target `app`, ~3GB). Base images are written
literally in the `FROM` lines so Dependabot can bump them.

| Stage        | Purpose |
|--------------|---------|
| `base`       | Go + module download (cache layer) |
| `builder`    | Static binary (`CGO_ENABLED=0`, `-trimpath`, version ldflags) |
| `tools`      | yt-dlp standalone binary, pinned (`ARG YTDLP_VERSION`), verified against `SHA2-256SUMS` |
| `deno`       | Deno binary (yt-dlp's JS runtime) |
| `stems-deps` | Python 3.12 venv: CPU-only PyTorch, audio-separator, audioread; has gcc for diffq |
| `app`        | Python slim + venv + ffmpeg + yt-dlp + deno + binary; non-root user with a home; `/models` (1777) volume; `ENTRYPOINT /dotboot` (no args = TUI) |

`scripts/dotboot-docker` runs the image as the calling user with `$HOME`
mounted at the same path (so `~`, absolute paths, `~/.config/dotboot` and
the TUI file picker behave like the native install), the `dotboot-models`
volume, `-it` when there is a terminal, and same-path mounts for absolute
paths outside the home.

## Releases & CI

- `.github/workflows/ci.yml`: gofmt, vet (with/without the integration tag),
  `go test -race`, cross-compilation for every release target, shellcheck,
  and an image build on pushes to main.
- `.github/workflows/release.yml`: a `v*` tag runs GoReleaser
  (`.goreleaser.yaml`): linux/darwin/windows × amd64/arm64 archives with
  README, `.env.example` and `scripts/install-deps.sh`, plus `checksums.txt`.
  `scripts/install.sh` depends on the archive name template.
- Commit messages: English conventional style (`type: summary` + past-tense
  bullets), no attribution trailers.
