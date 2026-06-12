# AGENTS.md — dotboot

## Project Overview

dotboot is a Go application with two modes sharing one binary:

- **`dotboot bot`** — Discord bot that downloads videos from social media
  platforms using `yt-dlp` and uploads them to Discord channels via the
  `/baixar` slash command. Supports video trimming, automatic FFmpeg
  compression for files exceeding Discord's upload limit (25 MB default),
  bounded download concurrency, exponential backoff retry logic, and
  two-phase graceful shutdown.
- **`dotboot download`** — terminal downloader without Discord's limits:
  best quality, no compression, playlist/channel support, URL batch files,
  and resumable "continue" semantics via `--download-archive` + `--continue`.

**Stack**: Go 1.24, discordgo, yt-dlp, FFmpeg, Docker

## Build Commands

```bash
# Local build
go build -o dotboot ./cmd/dotboot

# Build to temp (for validation only)
go build -o /tmp/dotboot-test ./cmd/dotboot

# Run locally
go run ./cmd/dotboot bot
go run ./cmd/dotboot download -o /tmp/videos <url>

# Docker production build
docker build --target production -t dotboot:latest .

# Docker dev build (Air live reload, runs the "bot" subcommand)
docker compose --profile dev up --build

# Docker production run
docker compose up -d --build

# Dependency management
go mod tidy          # Clean up go.mod / go.sum
go mod download      # Download all dependencies
go mod verify        # Verify dependency integrity
go get -u <package>  # Add or update a dependency
```

## Testing

Tests are table-driven, use only the standard `testing` package, and cover
pure logic (no network, no yt-dlp execution): argument building, error
classification, input validation, config parsing, and CLI flag parsing.

```bash
# Run all tests
go test ./...

# Run a single test by name
go test -run TestBuildArgsCLIMode -v ./internal/downloader

# Run tests with race detection
go test -race ./...

# Run tests with coverage
go test -cover ./...
```

Test files are named `*_test.go` and live alongside the source files they test.

## Linting & Formatting

No linter config (`.golangci.yml`) exists. Use Go's built-in tools:

```bash
go fmt ./...   # Format all files
go vet ./...   # Vet for common mistakes
```

All code must be `gofmt`-compliant. Do not configure custom formatters.

## Code Style Guidelines

### Architecture

```
cmd/dotboot/main.go      Entrypoint: env/config load, slog setup, signal
                         handling (signal.NotifyContext lives ONLY here),
                         subcommand dispatch (bot | download).
internal/config/         Env-var configuration with strict validation —
                         invalid values error out, never fall back silently.
internal/downloader/     yt-dlp + ffmpeg invocation. BOUNDARY RULE: this
                         package must NEVER import discordgo. Shared by both
                         modes. BuildArgs is pure; Run executes yt-dlp;
                         DownloadForDiscord adds temp-file + size handling.
internal/bot/            Discord session lifecycle, /baixar handler,
                         retry helpers, two-phase graceful shutdown.
internal/cli/            Terminal download mode (flag parsing + orchestration).
```

Key invariants to preserve:

- **Downloader/Discord boundary**: `internal/downloader` has zero Discord
  knowledge. Callers translate `DownloadError.Kind` into user-facing messages.
- **Two-phase shutdown** (`internal/bot/bot.go`): the download context derives
  from `context.Background()`, NOT from the signal context. On shutdown the
  bot first waits `ShutdownGrace` for in-flight downloads, and only then
  cancels them. Deriving downloads from the signal context would kill them
  instantly and defeat the grace period.
- **SIGINT, not SIGKILL**: `cmd.Cancel` sends `os.Interrupt` to yt-dlp/ffmpeg
  with a `WaitDelay` fallback, so `.part` files survive and CLI resume works.
- **Glob only in `DownloadForDiscord`**: finding the output file by glob is
  safe only because `--no-playlist` guarantees a single file. CLI mode never
  globs — yt-dlp owns the output names.
- **Trim + archive don't mix**: a trimmed download must not write to the
  download archive (it would mark the full video as done forever) and forces
  `--no-playlist`.
- **Bounded concurrency**: the bot acquires a semaphore (`MaxConcurrent`)
  before each download; `wg.Add` happens before the goroutine starts so
  shutdown counts queued work.

### Imports

Group imports in blocks separated by blank lines, each sorted alphabetically:

```go
import (
    "context"
    "fmt"

    "github.com/bwmarrin/discordgo"

    "dotboot/internal/downloader"
)
```

1. Standard library
2. Third-party packages
3. Internal `dotboot/...` packages

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
   …). Callers use `errors.As` + a switch on `Kind` to pick user-facing
   messages — never `strings.Contains` on translated messages.

4. **Discord upload errors** are the exception: they come from discordgo, not
   the downloader, and are still matched by substring in
   `internal/bot/handler.go`.

5. **Never discard errors silently** — even best-effort Discord replies log
   their errors (`respondWithError`/`editWithError`).

6. **Intentionally ignored errors** — Assign to `_` explicitly with a reason
   (e.g. `_ = godotenv.Load()` — the .env file is optional).

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
- **Context with timeout**: each bot download gets
  `context.WithTimeout(dlCtx, cfg.DownloadTimeout)`. CLI downloads have no
  timeout (playlists can take hours) — only signal cancellation.
- **Defer for cleanup**: always use `defer` to remove temporary files.

### Retry Pattern

Network operations use hand-rolled exponential backoff (see
`internal/bot/retry.go`): classify errors with `isNetworkError()` (string
matching), retry only on transient network errors, cap delay at
`maxRetryDelay`, limit attempts to `maxRetries`.

## Configuration

All knobs are env vars parsed in `internal/config` (see `.env.example`):
`DISCORD_TOKEN`, `DOTBOOT_MAX_FILE_SIZE_MB`, `DOTBOOT_DOWNLOAD_TIMEOUT`,
`DOTBOOT_MAX_CONCURRENT`, `DOTBOOT_SHUTDOWN_GRACE`, `DOTBOOT_OUTPUT_DIR`,
`DOTBOOT_TEMP_DIR`, `DOTBOOT_LOG_LEVEL`.

`DOTBOOT_OUTPUT_DIR` is the CLI **base path**: used when `-o` is absent, and
the root for relative `-o` values (`-o aulas` → `$base/aulas`); an absolute
`-o` ignores it. It never affects the bot, which only uses `DOTBOOT_TEMP_DIR`.

Historical note: the old `compressionThreshold` constant (20 MB) was removed —
it was never used. Compression triggers strictly above `MaxFileSizeBytes()`.

## Environment & Dependencies

- **Runtime deps**: `yt-dlp` (Python), `ffmpeg` (system package). Both modes
  require both binaries (best-quality CLI downloads need ffmpeg for merging).
- **Config**: `.env` file (see `.env.example`). Never commit `.env` — it is
  gitignored and contains secrets.
- **Go deps**: Managed via Go Modules (`go.mod` / `go.sum`). Keep them
  minimal — CLI parsing uses stdlib `flag`, not cobra.

## Docker

Multi-stage `Dockerfile` with four targets:

| Stage         | Purpose                                    |
|---------------|--------------------------------------------|
| `base`        | Go + dependency download (cache layer)     |
| `builder`     | Compile static binary (`CGO_ENABLED=0`, `./cmd/dotboot`) |
| `development` | Alpine + Air live reload + yt-dlp + ffmpeg (runs `bot` via `--build.args_bin`) |
| `production`  | Debian slim + compiled binary + yt-dlp + ffmpeg (`ENTRYPOINT /dotboot`, `CMD bot`) |

`docker-compose.yml` uses profiles: default for production, `--profile dev`
for development with Air. The production image runs the bot by default; pass
`download ...` as the container command to use the CLI mode.
