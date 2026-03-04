# AGENTS.md — dotboot

## Project Overview

dotboot is a Go Discord bot that downloads videos from social media platforms
(YouTube, X/Twitter, Instagram) using `yt-dlp` and uploads them to Discord
channels via the `/baixar` slash command. It supports video trimming, automatic
FFmpeg compression for files exceeding Discord's 25 MB limit, and exponential
backoff retry logic for network resilience.

**Stack**: Go 1.24, discordgo, yt-dlp, FFmpeg, Docker

## Build Commands

```bash
# Local build
go build -o dotboot .

# Build to temp (for validation only)
go build -o /tmp/dotboot-test .

# Docker production build
docker build --target production -t dotboot:latest .

# Docker dev build (Air live reload)
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

No tests exist yet. When adding tests, use Go's built-in `testing` package.

```bash
# Run all tests
go test ./...

# Run a single test by name
go test -run TestFunctionName -v .

# Run tests with race detection
go test -race ./...

# Run tests with coverage
go test -cover ./...
```

Test files must be named `*_test.go` and placed alongside the source files they
test (flat layout, `package main`).

## Linting & Formatting

No linter config (`.golangci.yml`) exists. Use Go's built-in tools:

```bash
# Format all files (standard Go formatting — tabs, canonical style)
go fmt ./...

# Vet for common mistakes
go vet ./...
```

All code must be `gofmt`-compliant. Do not configure custom formatters.

## Code Style Guidelines

### Architecture

- Single `package main`, flat directory layout (no `cmd/`, `internal/`, `pkg/`).
- Three source files separated by responsibility:
  - `main.go` — Entry point, Discord session setup, signal handling, retry logic.
  - `handler.go` — Slash command interaction handler, input validation.
  - `downloader.go` — yt-dlp invocation, video compression, file size validation.
- Keep this separation when adding new functionality. Create a new file only
  when introducing a clearly distinct domain (e.g., `config.go` for configuration).

### Imports

Group imports in two blocks separated by a blank line, each sorted alphabetically:

```go
import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/bwmarrin/discordgo"
)
```

1. Standard library
2. Third-party packages

No blank import lines within a group. When only stdlib imports exist, use a
single block.

### Formatting

- **Indentation**: Tabs (Go standard, enforced by `gofmt`).
- **Quotes**: Double quotes for strings; backticks for raw/multiline strings.
- **Trailing commas**: Required in multi-line composite literals (Go syntax).
- **Struct field alignment**: Align values with tabs for readability:
  ```go
  Name:        "baixar",
  Description: "Descarrega um video.",
  Required:    true,
  ```
- **Line length**: No hard limit; keep lines reasonable (~120 chars). Break long
  conditions or arguments across multiple lines.

### Naming Conventions

| Element             | Convention     | Example                          |
|---------------------|----------------|----------------------------------|
| Exported functions  | `PascalCase`   | `InteractionCreate`, `DownloadVideo` |
| Unexported functions| `camelCase`    | `processDownload`, `respondWithError` |
| Variables           | `camelCase`    | `startTime`, `optionMap`, `fileSize` |
| Constants           | `camelCase`    | `maxRetries`, `maxFileSize`      |
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

2. **Error wrapping** — Use `fmt.Errorf` with `%w` to preserve the error chain:
   ```go
   return "", fmt.Errorf("falhou ao comprimir video: %w", err)
   ```

3. **Fatal errors** — Use `log.Fatalf` only for unrecoverable startup failures:
   ```go
   log.Fatalf("Environment validation failed: %v", err)
   ```

4. **Intentionally ignored errors** — Assign to `_` explicitly:
   ```go
   _ = godotenv.Load()
   ```

5. **User-facing errors** — Translate internal errors to Portuguese messages
   using `strings.Contains` on lowercased error strings.

6. **No custom error types** are used; the project uses plain `error` values.

### Comments

- Written primarily in **European Portuguese** (project convention).
- Some technical/operational comments may be in English.
- Use `//` line comments only (no `/* */` block comments).
- Place comments above the declaration or block they describe.
- Inline end-of-line comments are acceptable for flag explanations:
  ```go
  "--no-warnings",  // Reduce noise in logs
  "--quiet",        // Only show errors
  ```

### Concurrency Patterns

- **Async processing**: Long-running work (downloads) runs in a goroutine
  launched from the handler (`go processDownload(...)`).
- **Context with timeout**: Used for graceful shutdown and cancellation:
  ```go
  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()
  ```
- **Defer for cleanup**: Always use `defer` to remove temporary files.

### Retry Pattern

Network operations use hand-rolled exponential backoff (see `main.go`):
- Classify errors with `isNetworkError()` (string matching).
- Retry only on transient network errors; bail immediately on others.
- Cap delay at `maxRetryDelay`; limit attempts to `maxRetries`.

## Environment & Dependencies

- **Runtime deps**: `yt-dlp` (Python), `ffmpeg` (system package).
- **Config**: `.env` file with `DISCORD_TOKEN` (see `.env.example`). Never
  commit `.env` — it is gitignored and contains secrets.
- **Go deps**: Managed via Go Modules (`go.mod` / `go.sum`).

## Docker

Multi-stage `Dockerfile` with four targets:

| Stage         | Purpose                                    |
|---------------|--------------------------------------------|
| `base`        | Go + dependency download (cache layer)     |
| `builder`     | Compile static binary (`CGO_ENABLED=0`)    |
| `development` | Alpine + Air live reload + yt-dlp + ffmpeg |
| `production`  | Debian slim + compiled binary + yt-dlp + ffmpeg |

`docker-compose.yml` uses profiles: default for production, `--profile dev` for
development with Air.
