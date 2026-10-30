# AGENTS.md — dotboot

## Project Overview

dotboot is a local web app, in Go + SvelteKit. It downloads videos and audio
with `yt-dlp` and separates songs into stems with `audio-separator`. One
binary serves a JSON API, Server-Sent Events and the compiled page
(embedded with `go:embed`). `dotboot` with no arguments starts the server on
`127.0.0.1:8765` and opens the browser.

- **New**: video (MP4/original, quality cap, playlists, trims), music
  (mp3/m4a with tags and cover) and stems separation (URL or uploaded file,
  presets 6s/4s/voz/voz-hq, trims).
- **Jobs**: a queue with real progress (bytes, speed, ETA, passes), per-job
  logs, cancel/retry, live over SSE.
- **Studio** (`/estudio/[id]`): one lane per stem with a waveform,
  mute/solo/volume, synchronized playback. Selected stems download as one
  mixed file (WAV/FLAC/MP3 320k, the studio volumes applied) or as separate
  files in a ZIP.
- **Library**: past separations and downloads. **Settings**: cookies upload,
  storage usage, theme and the doctor report.

Files never land in user folders on their own. Everything stays in the data
dir until the user downloads it through the browser.

There is no TUI and no CLI subcommands anymore, only `dotboot [-addr]
[-no-browser]`, `dotboot version` and `dotboot health`.

**Stack**: Go 1.26+ (built with 1.27), SvelteKit 3 + Svelte 5 (runes) +
Tailwind CSS 4 + TypeScript (adapter-static SPA), yt-dlp, FFmpeg, Deno,
audio-separator (PyTorch CPU), Docker.

## Build Commands

`make` with no target lists everything. The common ones:

```bash
make setup            # Post-clone assistant: install (docker|native)
make run              # Build the page, run the server from source (ADDR=127.0.0.1:8765)
make web-dev          # Vite with hot reload (:5173, proxies /api) + Go server (:8765)
make web              # npm ci + build, copied to internal/web/dist/app (embedded)
make build            # web + bin/dotboot with version ldflags
make install          # Native binary into ~/.local/bin (PREFIX=… to change)
make install-docker   # Docker wrapper (scripts/dotboot-docker) as `dotboot`
make deps             # yt-dlp + deno (checksum-verified) and ffmpeg on the host
make deps-stems       # audio-separator via uv (needs a C compiler)
make docker-build     # The single image dotboot:latest (~3GB, includes stems)
make update-ytdlp     # Pin the latest yt-dlp in the Dockerfile and rebuild
make release-snapshot # GoReleaser in Docker, all platforms into dist/
```

The module path is `github.com/abneribeiro/dotboot`.

## Testing

```bash
make check                     # gofmt + vet + go test -race + svelte-check + prettier + vitest
make test-integration-docker   # real yt-dlp/ffmpeg/audio-separator inside dotboot:latest
```

- Go tests are table-driven and use only the standard `testing` package.
  Pure logic is unit-tested: argument building, progress parsers (yt-dlp
  template, ffmpeg `out_time_us`, tqdm), `ComputePeaks`, `MixArgs` with
  gains, error classification, validation, the store, doctor with a fake
  system.
- `internal/jobs` and `internal/web` run whole jobs against **fake
  binaries**: small `sh` scripts on a test `PATH` that imitate yt-dlp,
  ffprobe, ffmpeg and audio-separator (progress lines, output files, PCM for
  the waveform). `internal/web` tests the HTTP API end to end with
  `httptest`: the guard (Host/Origin/Content-Type), upload → stems → studio
  → export (mix and ZIP), downloads, cancel/retry/remove, SSE and cookies.
  Use this pattern for new orchestration tests: no network, no real tools.
- The frontend's pure logic (`web/src/lib/*.ts`: mixer gains, drift
  resync, export params, peaks resampling, formatting, SSE reducer,
  Content-Disposition) is tested with vitest (`*.test.ts` next to the file).
- Integration tests (`//go:build integration`) execute the real tools;
  `make test-integration-docker` compiles them with `go test -c` and runs
  them inside the image (separation, `Export`, `MeasurePeaks`, `Render`).

## Linting & Formatting

Go: `make lint` = gofmt check + `go vet` (with and without the integration
tag). Page: `npm run check` (svelte-check), `npm run lint` (prettier, with
the Svelte and Tailwind plugins), `npm run format` to fix. Shell scripts are
checked with shellcheck in CI.

## Code Style Guidelines

### Architecture

```
cmd/dotboot/main.go    Entrypoint: config load, slog, signals (NotifyContext
                       lives ONLY here), store.Open + Clean, http.Server,
                       browser opening, shutdown order, version/health.
internal/config/       Env-var configuration, strict validation, config.env.
internal/procio/       LineWriter (splits on \n AND \r) and TailBuffer.
internal/downloader/   yt-dlp + ffmpeg. BuildArgs, QualitySort, MP4Sort,
                       ParseLine, FFmpegFraction, mp4Compatible, compatArgs
                       are pure; Run executes yt-dlp with progress callbacks;
                       EnsureMP4 enforces the output format (ConvertHooks);
                       DownloadAudio feeds stems.
internal/stems/        audio-separator + ffmpeg. Presets, SeparatorArgs,
                       MixArgs (per-track gains), EncodeArgs, ToWAVArgs,
                       PeaksArgs, ComputePeaks and ParseTQDM are pure;
                       Separate, Export, Render and MeasurePeaks execute.
internal/jobs/         THE ENGINE: NewDownload/NewStems validate a request;
                       Download.Run/Stems.Run execute it into a directory
                       the caller gives and report jobs.Event via a Reporter.
internal/store/        The data dir: sessoes/<id> (FLAC stems + peaks.json +
                       meta.json), downloads/<id>, uploads/, cookies.txt.
                       IDs are 16 hex chars (ValidID), so no path traversal.
internal/web/          The server: server.go (routes, guard, job creation,
                       uploads, cookies), library.go (sessions, tracks with
                       Range, export mix/ZIP, downloads), queue.go (one job at
                       a time), hub.go (SSE fan-out), static.go (embedded SPA).
internal/doctor/       Environment report (Collect; Check marshals to JSON).
web/                   SvelteKit SPA. src/lib: api.ts (types + fetch),
                       events.svelte.ts (one EventSource for the app),
                       jobs.ts (SSE reducer), mixer.ts (pure studio logic),
                       player.svelte.ts (StudioPlayer), format.ts,
                       components/. Routes: / (new), /trabalhos,
                       /biblioteca, /estudio/[id], /definicoes.
scripts/               setup.sh, install-deps.sh, dotboot-docker (wrapper),
                       install.sh (releases).
```

Key invariants to preserve:

- **One engine**: validation and orchestration of a job live in
  `internal/jobs`; `internal/web` only maps HTTP requests to
  `jobs.DownloadInput`/`jobs.StemsInput`, picks the directories (store) and
  records the result. `downloader`, `stems` and `jobs` never import `web`.
- **Nothing leaves the data dir by itself**: downloads go to
  `<data>/downloads/<id>` (fixed per job, so a retry resumes `.part` files
  and skips the archive), separations to `<data>/sessoes/<id>` (a new id per
  run; a failed run deletes its dir). A dir only appears in the library once
  its `meta.json` is written (atomically); `store.Clean` at startup removes
  dirs without one and uploads older than a day.
- **Sessions are lossless**: stems are stored as FLAC (`jobs.SessionFormat`)
  with `peaks.json` (2000 points per stem, measured from 8 kHz mono PCM).
  Exports start from the FLAC: `mode=mix` renders with `stems.Render` (amix
  `normalize=0` keeps the original loudness; per-track `volume=` gains from
  the studio, 0–2); `mode=zip` streams a stored ZIP, copying FLACs at gain 1
  as they are and rendering the rest one by one.
- **Events, not output**: progress reaches the page as `jobs.Event` →
  `queue` → `hub` → SSE. An Event's empty fields mean "unchanged". Progress
  updates are throttled (150 ms) but stage/item changes go out at once. On
  connect the client gets the full job list (`type: "jobs"`). A client that
  falls behind is dropped and reconnects (EventSource does it on its own),
  so nothing is lost.
- **One job at a time**: separation and conversions are CPU-bound, and
  parallel yt-dlp only annoys sites. Shutdown order in `main`:
  `srv.Shutdown()` (closes SSE streams, cancels jobs and waits up to 15s
  for yt-dlp/ffmpeg/audio-separator to exit) and then `http.Server.Shutdown`.
- **Local only**: the server binds to 127.0.0.1 by default. `guard` rejects
  a Host that is not localhost/loopback (DNS rebinding), and for
  state-changing requests a foreign `Origin` or a body that is not JSON or
  multipart (CSRF from other sites). There are no accounts, so never expose
  it on a network.
- **Studio playback**: one `<audio>` per stem (streamed with Range, not
  decoded in memory) → `MediaElementSource` → `GainNode`. Mute/solo/volume
  only ramp the gain (`setTargetAtTime`); the first track is the master
  clock and others are re-aligned when they drift more than 40 ms
  (`tracksToResync`); a `waiting` track pauses all until all can play.
- **Real progress, parsed purely**: yt-dlp gets `--newline` and a
  `--progress-template` with a marker that `ParseLine` reads (template lines
  never reach the log). ffmpeg runs with `-progress pipe:1`
  (`FFmpegFraction`); audio-separator's tqdm bars are read by `ParseTQDM`,
  and a bar that drops back is a new numbered pass.
- **Output is always MP4 / H.264 8-bit / AAC** (video mode): `MP4Sort` asks
  yt-dlp to prefer those codecs (resolution must outrank `vcodec` in `-S`),
  and `EnsureMP4` probes and fixes what landed. Every re-encode carries
  `-pix_fmt yuv420p`.
- **Resolution means the SHORT side**: quality N is only `-S res:N`, never an
  `-f [height<=N]` filter.
- **Pending conversions persist** (`.dotboot-pendentes.txt` in the download
  dir) because yt-dlp records the archive before `EnsureMP4` runs.
- **Trim + archive don't mix**: a trim never writes the archive, forces
  `--no-playlist` and has the interval in the file name (`outputTemplate`).
- **SIGINT, not SIGKILL**: `cmd.Cancel` sends `os.Interrupt` with a
  `WaitDelay`. Cancelled jobs return `context.Canceled`; partial success
  returns an error wrapping `jobs.ErrPartial` plus the Result (the job
  counts as done, with a warning).
- **`--print-to-file` implies `--simulate`**: `BuildArgs` always emits
  `--no-simulate` with it.
- **The page is embedded**: `make web` copies `web/build` into
  `internal/web/dist/app` (gitignored). The repo keeps only
  `internal/web/dist/.gitkeep`, so Go tests run without Node; a binary built
  without the page serves a notice. Unknown paths fall back to `200.html`
  (SPA); `_app/immutable/*` is cached forever.

### Frontend conventions

- Svelte 5 runes only (`$state`, `$derived`, `$effect`, `$props`); shared
  reactive state lives in `*.svelte.ts` classes.
- SvelteKit 3 imports the lib through package subpath imports: `#lib/…`,
  with `.js` for TS modules (`#lib/api.js`, `#lib/events.svelte.js`).
- Colors come from the theme tokens in `src/routes/layout.css`
  (`bg-panel`, `text-muted`, `border-line`, `bg-accent`…), defined for dark
  (default) and light. Avoid raw hex colors in markup. The stem colors are
  in `mixer.ts`.
- User-facing text is European Portuguese. Keep logic that can be pure in
  `src/lib/*.ts` with a vitest test.

### Imports (Go)

Group imports in blocks separated by blank lines, each sorted alphabetically:
standard library, third-party, then internal
`github.com/abneribeiro/dotboot/...`.

### Formatting & Naming (Go)

gofmt; tabs; trailing commas in multi-line literals; ~120-char lines.
`PascalCase` exported, `camelCase` everything else including constants (never
`SCREAMING_SNAKE_CASE`); acronyms uppercase (`URL`, `ID`); lowercase file
names.

### Error Handling

1. Check errors immediately; wrap with `fmt.Errorf("…: %w", err)`.
2. `internal/downloader` returns `*DownloadError` with a `Kind`; classify
   with `errors.As` + switch, never by substring of translated messages.
3. Never discard errors silently; intentionally ignored ones are assigned to
   `_` with a reason.
4. The API answers errors as `{"error": "…"}` in Portuguese, with the right
   status (400 validation, 404 unknown id via `store.ErrNotFound`, 409 when
   removing a running job, 415/403 from the guard).

### Logging

`log/slog` only, level from `DOTBOOT_LOG_LEVEL`, default logger set in
`main`. User-facing progress goes through `jobs.Event`, not slog.

### Comments

- Written primarily in **European Portuguese** (project convention); user
  facing text too. Some technical comments may be in English.
- `//` line comments only, above the declaration they describe.

## Configuration

Env vars parsed in `internal/config` (see `.env.example`):
`DOTBOOT_DATA_DIR`, `DOTBOOT_TEMP_DIR`, `DOTBOOT_COOKIES_FILE`,
`DOTBOOT_JS_RUNTIMES`, `DOTBOOT_MODELS_DIR`, `DOTBOOT_LOG_LEVEL`, plus
`DOTBOOT_ADDR` read in `main`. Priority: environment > `./.env` >
`~/.config/dotboot/config.env` (godotenv never overrides; loaded before the
flags). A leading `~` is expanded in every path. Cookies uploaded in
Settings go to `<data>/cookies.txt` (0600) and are used when
`DOTBOOT_COOKIES_FILE` is unset.

## Environment & Dependencies

- **Runtime deps**: `yt-dlp`, `ffmpeg`, `ffprobe`, a JS runtime for YouTube
  (Deno, or node picked automatically), and `audio-separator` for stems.
  The server starts without them; Settings → Diagnostics says what is
  missing, and `main` logs failed checks at startup.
- **Go deps**: godotenv only; HTTP routing is stdlib `net/http` (Go 1.22+
  method/wildcard patterns).
- **Node deps** (`web/`): SvelteKit, Svelte, Vite, Tailwind, vitest,
  prettier, svelte-check, all dev dependencies (the output is static).

## Docker

One image, `dotboot:latest` (target `app`, ~3GB). Base images are written
literally in the `FROM` lines so Dependabot can bump them.

| Stage        | Purpose |
|--------------|---------|
| `base`       | Go + module download (cache layer) |
| `web`        | Node: `npm ci` + `npm run build` of the page |
| `builder`    | Static binary with the page embedded (`CGO_ENABLED=0`, version ldflags) |
| `tools`      | yt-dlp standalone binary, pinned (`ARG YTDLP_VERSION`), verified against `SHA2-256SUMS` |
| `deno`       | Deno binary (yt-dlp's JS runtime) |
| `stems-deps` | Python 3.12 venv: CPU-only PyTorch, audio-separator, audioread; has gcc for diffq |
| `app`        | Python slim + venv + ffmpeg + yt-dlp + deno + binary; non-root user; volumes `/data` and `/models` (1777); listens on 0.0.0.0:8765; `HEALTHCHECK dotboot health` |

`scripts/dotboot-docker` publishes the port only on `127.0.0.1`
(`DOTBOOT_PORT`, default 8765) with the `dotboot-data` and `dotboot-models`
volumes. `dotboot` runs in the foreground and opens the browser; `up`/`down`
manage a background container (`--restart unless-stopped`, 30s stop so jobs
cancel cleanly); `open`, `logs`, `status`, `version`. It refuses to start
when another app holds the port. `DOTBOOT_COOKIES_FILE` mounts a host
cookies file read-only; `DOTBOOT_NO_BROWSER=1` skips opening the browser.

## Releases & CI

- `.github/workflows/ci.yml`: page checks (svelte-check, prettier, vitest),
  `make web`, gofmt, vet (with/without the integration tag), `go test -race`,
  cross-compilation for every release target, shellcheck, and an image
  build on pushes to main.
- `.github/workflows/release.yml`: a `v*` tag runs `make web` and GoReleaser
  (`.goreleaser.yaml`, which checks the page is built):
  linux/darwin/windows × amd64/arm64 archives with README, `.env.example`
  and `scripts/install-deps.sh`, plus `checksums.txt`. `scripts/install.sh`
  depends on the archive name template.
- Commit messages: English conventional style (`type: summary` + past-tense
  bullets), no attribution trailers.
