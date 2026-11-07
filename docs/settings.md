# **settings**

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
