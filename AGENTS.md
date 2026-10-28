# AGENTS.md — dotboot

## Project Overview

dotboot is a Go application with several modes sharing one binary:

- **`dotboot bot`** — Discord bot that downloads videos from social media
  platforms using `yt-dlp` and uploads them to Discord channels via the
  `/baixar` slash command. Supports video trimming, a per-guild upload limit
  derived from the server's boost tier, size-targeted FFmpeg compression for
  files exceeding it, bounded download concurrency (global and per user),
  a public-URL policy, exponential backoff retry logic, and two-phase
  graceful shutdown.
- **`dotboot download`** — terminal downloader without Discord's limits: no
  size cap and no compression, best quality by default with an optional
  resolution cap (`-q 720`), audio-only mode (`-formato mp3|m4a`),
  playlist/channel support, URL batch files, and resumable "continue"
  semantics via `--download-archive` + `--continue` + a pending-conversions
  file. Output is guaranteed MP4/H.264/AAC unless `-formato original` opts out.
- **`dotboot stems`** — separates a song (URL or local file) into tracks
  (vocals, drums, bass, guitar, piano, other) with `audio-separator`, and can
  mix a backing track without some of them (`-sem guitarra`).
- **`dotboot doctor`** — checks dependencies and configuration and says how
  to fix each problem. **`dotboot healthcheck`** is the image's HEALTHCHECK.
  **`dotboot version`** prints the build metadata.

**Stack**: Go 1.24+ (built with 1.27), discordgo, yt-dlp, FFmpeg, Deno,
audio-separator (stems only), Docker

## Build Commands

`make` with no target lists everything. The common ones:

```bash
make setup            # Post-clone assistant: .env, CLI, images, bot, doctor
make build            # bin/dotboot with version ldflags
make install          # Native CLI into ~/.local/bin (PREFIX=… to change)
make install-docker   # Docker wrapper (scripts/dotboot-docker) as the CLI
make deps             # yt-dlp + deno (checksum-verified) and ffmpeg on the host
make deps-stems       # audio-separator via uv (needs a C compiler)
make docker-build     # Production image (dotboot:latest)
make stems-build      # Stems image (dotboot-stems:latest, ~3GB)
make up / down / logs / ps / dev
make update-ytdlp     # Pin the latest yt-dlp in the Dockerfile and rebuild
make release-snapshot # GoReleaser in Docker, all platforms into dist/

# Plain Go still works
go build -o /tmp/dotboot-test ./cmd/dotboot
go run ./cmd/dotboot download -o /tmp/videos <url>

# Dependency management
go mod tidy          # Clean up go.mod / go.sum
go mod verify        # Verify dependency integrity
```

The module path is `github.com/abneribeiro/dotboot` (so `go install …@latest`
works; with the repo private it needs `GOPRIVATE=github.com/abneribeiro/*`).

## Testing

Tests are table-driven, use only the standard `testing` package, and cover
pure logic (no network, no yt-dlp/audio-separator execution): argument
building, error classification, input validation, config parsing, CLI flag
parsing, stem selection, doctor checks with a fake system.

```bash
make check                          # gofmt + vet (incl. integration tag) + go test -race
go test -run TestBuildArgsCLIMode -v ./internal/downloader
make cover
```

Test files are named `*_test.go` and live alongside the source files they test.

Integration tests are gated behind the `integration` build tag so
`go test ./...` stays fast and offline. They execute the real tools:

```bash
make test-integration-docker   # downloader: yt-dlp + ffmpeg + network, inside dotboot:latest
make test-integration-stems    # stems: a real separation, inside dotboot-stems:latest
make test-integration          # downloader on the host (needs yt-dlp, ffmpeg, ffprobe)
```

The `-docker` variants compile the test binary with `go test -c` and run it
inside the image, so the host needs nothing but Docker.

## Linting & Formatting

No linter config (`.golangci.yml`) exists. Use Go's built-in tools
(`make lint`). Shell scripts are checked with shellcheck in CI. All code must
be `gofmt`-compliant. Do not configure custom formatters.

## Code Style Guidelines

### Architecture

```
cmd/dotboot/main.go      Entrypoint: env/config load, slog setup, signal
                         handling (signal.NotifyContext lives ONLY here),
                         subcommand dispatch, binary checks, JS runtime pick,
                         version metadata.
internal/config/         Env-var configuration with strict validation —
                         invalid values error out, never fall back silently.
internal/downloader/     yt-dlp + ffmpeg invocation. BOUNDARY RULE: this
                         package must NEVER import discordgo. Shared by every
                         mode. BuildArgs, QualitySort, MP4Sort, mp4Compatible,
                         compatArgs, encodeArgs, scaleFilter, sortForBudget,
                         bitratesFor and ValidatePublicURL are pure; Run
                         executes yt-dlp; EnsureMP4 (compat.go) enforces the
                         output format; DownloadForDiscord adds work-dir +
                         size handling; DownloadAudio feeds stems.
internal/stems/          audio-separator + ffmpeg for stem separation. Same
                         BOUNDARY RULE (no discordgo, no cli). Presets,
                         Select, SeparatorArgs, MixArgs, EncodeArgs and
                         ToWAVArgs are pure; Separate and Export execute.
internal/bot/            Discord session lifecycle, /baixar handler, upload
                         limits (limits.go), retry helpers, heartbeat +
                         gateway watchdog, two-phase graceful shutdown.
internal/cli/            Terminal modes: download (cli.go) and stems
                         (stems.go) — flag parsing + orchestration.
internal/doctor/         Environment report used by `make setup`.
scripts/                 setup.sh, configure-env.sh, install-deps.sh,
                         dotboot-docker (wrapper), install.sh (releases).
```

Key invariants to preserve:

- **Downloader/Discord boundary**: `internal/downloader` and `internal/stems`
  have zero Discord knowledge. Callers translate `DownloadError.Kind` into
  user-facing messages, and add their own hints (the bot appends the
  `inicio`/`fim` suggestion to `ErrTooLarge`).
- **Output is always MP4 / H.264 8-bit / AAC**: it is the only combination
  iPhones, Android and Discord's inline player all decode. Two layers enforce
  it and BOTH are needed. First, `MP4Sort` asks yt-dlp to *prefer* those codecs
  (`-S <res>,vcodec:h264,acodec:aac` + `--merge-output-format mp4`) — free, and
  enough for YouTube up to 1080p, Instagram, TikTok and X. Second, `EnsureMP4`
  probes what actually landed and fixes it, because a preference is not a
  guarantee: above 1080p YouTube has no H.264 at all. Note the ordering inside
  `MP4Sort` — resolution must outrank `vcodec`, or a 480p H.264 would beat a
  2160p AV1 and `-q 2160` would be silently ignored. `EnsureMP4` copies every
  stream that is already conformant, so the common path costs one `ffprobe`.
  Every re-encode — `compatArgs` AND the bot's `encodeArgs` — must carry
  `-pix_fmt yuv420p`: a 10-bit source would otherwise produce H.264 High10,
  which has the right codec name and still will not play on an iPhone.
  `ensureMP4WithinBudget` skips `EnsureMP4` for oversized files precisely
  because `encodeArgs` guarantees this.
- **Resolution means the SHORT side**: yt-dlp's `res` is the smaller
  dimension, and so is everything here. `-q N` is only `-S res:N` (a cap with
  fallback) — never an `-f [height<=N]` filter, which turned a vertical
  720x1280 Short into 360x640. The bot's compression ladder uses
  `scaleFilter`, which scales the short side for the same reason.
- **Two-phase shutdown** (`internal/bot/bot.go`): the download context derives
  from `context.Background()`, NOT from the signal context. On shutdown the
  bot first waits `ShutdownGrace` for in-flight downloads, and only then
  cancels them. Deriving downloads from the signal context would kill them
  instantly and defeat the grace period. `closing` and `wg.Add` are guarded by
  the same mutex (`track`), so no download is added after `wg.Wait` started.
- **Docker must wait for the grace period**: `stop_grace_period` (compose) /
  `--stop-timeout` (docker run) must be ≥ `DOTBOOT_SHUTDOWN_GRACE` + 5s. With
  Docker's default 10s the container is SIGKILLed mid-grace.
- **SIGINT, not SIGKILL**: `cmd.Cancel` sends `os.Interrupt` to
  yt-dlp/ffmpeg/audio-separator with a `WaitDelay` fallback, so `.part` files
  survive and CLI resume works. After the first signal, `main` restores the
  default handler, so a second CTRL+C exits immediately.
- **One work dir per bot download**: `DownloadForDiscord` creates its own
  `<TempDir>/dotboot-*` directory and returns it in `Result.WorkDir`. The
  caller MUST `os.RemoveAll` it on every path, error paths included — that
  single call is what keeps `.part` files, per-format fragments and rejected
  re-encodes from piling up. Cookies are copied into it per download
  (`copyCookies`): yt-dlp rewrites the cookie file on exit and concurrent
  writers would corrupt the original. `stems` uses the same pattern
  (`dotboot-stems-*`). CLI downloads have no work dir: yt-dlp owns the output
  names and writes straight to the user's directory.
- **Pending conversions persist**: the CLI's `--print-to-file` target is
  `<outdir>/.dotboot-pendentes.txt`, not a temp file. yt-dlp records a video
  in the archive BEFORE `EnsureMP4` runs, so an interrupted conversion would
  otherwise leave a `.webm` forever. The list is deleted only when everything
  in it converted; the next run reprocesses it (`EnsureMP4` is idempotent).
- **`--print-to-file` implies `--simulate`**: `BuildArgs` always emits
  `--no-simulate` alongside it. Dropping that pair would make the bot print a
  path and download nothing.
- **The upload limit is per-guild, never a constant**: it comes from
  `Guild.PremiumTier` (see `internal/bot/limits.go`) and is resolved per
  interaction. `DOTBOOT_MAX_FILE_SIZE_MB` is a ceiling that can only lower it.
- **The interaction token lives 15 minutes**: `interactionDeadline` derives it
  from the interaction snowflake, and it bounds both the queue wait and the
  download timeout. Work that would finish after it is work whose result can
  never be delivered — but the user is always told (`editWithError`).
- **Trim + archive don't mix**: a trimmed download must not write to the
  download archive (it would mark the full video as done forever), forces
  `--no-playlist`, and has its interval in the file name (`outputTemplate`):
  with the full video's name, yt-dlp would answer "already downloaded".
  Audio-only downloads use their own archive (`.dotboot-archive-audio.txt`)
  because the archive stores IDs, not formats.
- **Bounded concurrency**: the bot acquires a semaphore (`MaxConcurrent`)
  before each download and a per-user slot (`MaxPerUser`, `userLimiter`)
  before deferring; `wg.Add` happens before the goroutine starts so shutdown
  counts queued work.
- **Bot URLs must be public**: the handler calls `ValidatePublicURL`
  (loopback, private, link-local, CGNAT, numeric hosts and local suffixes are
  refused; `DOTBOOT_ALLOWED_DOMAINS` narrows further). It is syntactic — no
  DNS — and applies only to the bot; the CLI user is the operator.
- **The healthcheck tells the truth**: `startHeartbeat` touches
  `<TempDir>/dotboot.alive` only while the gateway's `LastHeartbeatAck` is
  fresh, and `dotboot healthcheck` reads the same config, so the path never
  drifts from `DOTBOOT_TEMP_DIR`. Plain Docker never restarts an unhealthy
  container, so after `gatewayDeadAfter` (10 min) without the gateway the
  bot exits with an error and the restart policy takes over.

### Imports

Group imports in blocks separated by blank lines, each sorted alphabetically:

```go
import (
    "context"
    "fmt"

    "github.com/bwmarrin/discordgo"

    "github.com/abneribeiro/dotboot/internal/downloader"
)
```

1. Standard library
2. Third-party packages
3. Internal `github.com/abneribeiro/dotboot/...` packages

### Formatting

- **Indentation**: Tabs (Go standard, enforced by `gofmt`).
- **Quotes**: Double quotes for strings; backticks for raw/multiline strings.
- **Trailing commas**: Required in multi-line composite literals (Go syntax).
- **Struct field alignment**: Align values with tabs for readability.
- **Line length**: No hard limit; keep lines reasonable (~120 chars).

### Naming Conventions

| Element             | Convention     | Example                          |
|---------------------|----------------|----------------------------------|
| Exported functions  | `PascalCase`   | `DownloadForDiscord`, `BuildArgs` |
| Unexported functions| `camelCase`    | `processDownload`, `respondWithError` |
| Variables           | `camelCase`    | `startTime`, `optionMap`, `fileSize` |
| Constants           | `camelCase`    | `maxRetries`, `archiveFileName`  |
| Files               | `lowercase`    | `handler.go`, `downloader.go`    |
| Acronyms in names   | Uppercase      | `uniqueID`, `urlParam`           |

Do NOT use `SCREAMING_SNAKE_CASE` for constants — follow Go convention.

### Error Handling

1. **Standard pattern** — Always check and handle errors immediately:
   ```go
   if err != nil {
       return fmt.Errorf("failed to do X: %w", err)
   }
   ```

2. **Error wrapping** — Use `fmt.Errorf` with `%w` to preserve the error chain.

3. **Classified download errors** — `internal/downloader` returns
   `*DownloadError` with a `Kind` (`ErrTimeout`, `ErrTooLarge`, `ErrPrivate`,
   `ErrLoginRequired`, `ErrRateLimited`, `ErrCanceled`, …). Callers use
   `errors.As` + a switch on `Kind` to pick user-facing messages — never
   `strings.Contains` on translated messages. For unclassified failures,
   `DownloadError.Detail()` gives yt-dlp's last `ERROR:` line; errors that are
   not `*DownloadError` are internal and never reach the Discord channel.

4. **Discord upload errors** come from discordgo, not the downloader, and are
   classified with `errors.As` on `*discordgo.RESTError` (HTTP status and the
   Discord error code, e.g. 40005) in `messageForUploadError` — never by
   substring on a translated message.

5. **Never discard errors silently** — even best-effort Discord replies log
   their errors (`respondWithError`/`editWithError`). A failed yt-dlp run in
   captured mode is logged at Warn with the tail of its output.

6. **Intentionally ignored errors** — Assign to `_` explicitly with a reason
   (e.g. `_ = godotenv.Load()` — the .env file is optional).

7. **CLI sentinels** — `cli.ErrUsage` (exit 2), `cli.ErrPartial` (exit 1),
   `cli.ErrInterrupted` (exit 130); a missing binary is `errMissingDeps` in
   `main`, printed without the log format.

### Logging

Structured logging with `log/slog` everywhere (no `log.Printf`). Attach
context as attrs: `interaction_id`, `url`, `guild_id`, `path`. Level comes
from `DOTBOOT_LOG_LEVEL`. The default logger is set in `cmd/dotboot/main.go`.

### Comments

- Written primarily in **European Portuguese** (project convention).
- Some technical/operational comments may be in English.
- Use `//` line comments only (no `/* */` block comments).
- Place comments above the declaration or block they describe.

### Concurrency Patterns

- **Async processing**: downloads run in goroutines launched from the handler,
  bounded by the semaphore channel and tracked by the WaitGroup.
- **Context with timeout**: each bot download gets the smaller of
  `cfg.DownloadTimeout` and the time left on the interaction token. CLI
  downloads and stems have no timeout (playlists can take hours) — only
  signal cancellation.
- **Defer for cleanup**: always `defer os.RemoveAll(res.WorkDir)` as soon as
  the work dir exists, before checking the download error.

### Retry Pattern

Network operations use hand-rolled exponential backoff (see
`internal/bot/retry.go`): classify errors with `isNetworkError()` (string
matching), retry only on transient network errors, cap delay at
`maxRetryDelay`, limit attempts to `maxRetries`. Waits go through
`sleepCtx` so a SIGTERM is observed immediately instead of after the delay.

The REST `http.Client` has a 15-minute `Timeout` on purpose: that timeout
covers writing the request body, so a short one silently aborts large
uploads. `ResponseHeaderTimeout` stays short — in Go it only starts after the
body is fully written. discordgo buffers the whole attachment in memory
(`MultipartBodyWithJSON`), which is why the compose memory limit is 1536M.

## Configuration

All knobs are env vars parsed in `internal/config` (see `.env.example`):
`DISCORD_TOKEN`, `DOTBOOT_GUILD_ID`, `DOTBOOT_MAX_FILE_SIZE_MB`,
`DOTBOOT_DOWNLOAD_TIMEOUT`, `DOTBOOT_MAX_CONCURRENT`, `DOTBOOT_MAX_PER_USER`,
`DOTBOOT_ALLOWED_DOMAINS`, `DOTBOOT_SHUTDOWN_GRACE`, `DOTBOOT_OUTPUT_DIR`,
`DOTBOOT_TEMP_DIR`, `DOTBOOT_COOKIES_FILE`, `DOTBOOT_JS_RUNTIMES`,
`DOTBOOT_MODELS_DIR`, `DOTBOOT_LOG_LEVEL`. A leading `~` is expanded in every
path (a `.env` never goes through a shell).

`DOTBOOT_MAX_FILE_SIZE_MB` defaults to 0 = no ceiling; the effective limit is
the guild's. `DOTBOOT_GUILD_ID` scopes command registration to one server
(instant propagation) instead of globally (up to 1 hour).

`DOTBOOT_OUTPUT_DIR` is the CLI **base path**: used when `-o` is absent, and
the root for relative `-o` values (`-o aulas` → `$base/aulas`); an absolute
`-o` ignores it. It never affects the bot, which only uses `DOTBOOT_TEMP_DIR`.

`.env` is created and edited by `scripts/configure-env.sh` (`make env`), which
never overwrites existing keys it was not asked about and never prints the
token.

## Environment & Dependencies

- **Runtime deps**: `yt-dlp`, `ffmpeg` and `ffprobe` for `bot`/`download`
  (checked per subcommand by `checkBinaries` in `cmd/dotboot/main.go`; `-h`
  always works). A JavaScript runtime for yt-dlp's YouTube challenges: Deno
  (yt-dlp's default, no flag needed) or node (`--js-runtimes node`, picked
  automatically when it is the only one). `stems` needs `ffmpeg` and
  `audio-separator`, plus `yt-dlp` only for URL inputs.
- **Config**: `.env` file (see `.env.example`). Never commit `.env` — it is
  gitignored and contains secrets.
- **Go deps**: Managed via Go Modules (`go.mod` / `go.sum`). Keep them
  minimal — CLI parsing uses stdlib `flag` (with `parseInterspersed` for
  flags after positional args), not cobra.

## Docker

Multi-stage `Dockerfile`. Base images are written literally in the `FROM`
lines (no ARG) so Dependabot can bump them.

| Stage         | Purpose                                    |
|---------------|--------------------------------------------|
| `base`        | Go + dependency download (cache layer)     |
| `builder`     | Static binary (`CGO_ENABLED=0`, `-trimpath`, version ldflags) |
| `tools`       | yt-dlp standalone binary, pinned (`ARG YTDLP_VERSION`) and verified against the release's `SHA2-256SUMS` |
| `deno`        | Deno binary (yt-dlp's JS runtime)          |
| `runtime`     | Debian trixie slim + ffmpeg + yt-dlp + deno + non-root user **with a home** (yt-dlp's cache lives there) |
| `production`  | `runtime` + binary, `ENTRYPOINT /dotboot`, `CMD bot`, `HEALTHCHECK /dotboot healthcheck` |
| `development` | golang:1.27-trixie + the same tools + Air (pinned), runs `bot` via `--build.args_bin` |
| `stems-deps`  | Python 3.12 venv with CPU-only PyTorch + audio-separator (+ audioread); has a compiler for diffq |
| `stems`       | Python slim + the venv + ffmpeg + yt-dlp + deno + binary; `/models` (1777) as a volume |

`make update-ytdlp` rewrites the `ARG YTDLP_VERSION` line with the latest
release and rebuilds — yt-dlp is the dependency that ages fastest, since it
breaks whenever YouTube changes.

`docker-compose.yml`: the `videobot` service builds and runs `dotboot:latest`
(the same tag the CLI wrapper uses) with `init: true`, `stop_grace_period:
45s` (see the shutdown invariant) and a 1536M memory limit; `--profile dev`
runs `videobot-dev` with Air and no healthcheck. The production image runs
the bot by default; pass `download ...` as the container command to use the
CLI mode.

## Releases & CI

- `.github/workflows/ci.yml`: gofmt, vet (with and without the integration
  tag), `go test -race`, cross-compilation for every release target,
  shellcheck, and a production image build on pushes to main.
- `.github/workflows/release.yml`: a `v*` tag runs GoReleaser
  (`.goreleaser.yaml`): linux/darwin/windows × amd64/arm64 archives with
  README, `.env.example` and `scripts/install-deps.sh`, plus `checksums.txt`.
  `scripts/install.sh` depends on the archive name template.
- `make release VERSION=vX.Y.Z` creates and pushes the tag.
