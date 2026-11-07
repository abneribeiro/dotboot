# **settings**

```bash
dotboot config output ~/Videos         # default output folder
dotboot config cookies ~/cookies.txt   # cookies for every download
dotboot config cookies ""              # clear a setting
```

stored in `~/.config/dotboot/config.env`. environment variables, and a `.env`
in the current folder, take priority:

| variable | meaning | default |
|---|---|---|
| `DOTBOOT_OUTPUT_DIR` | output folder, and the root of a relative `-o` | current folder |
| `DOTBOOT_COOKIES_FILE` | netscape cookies file | none |
| `DOTBOOT_TEMP_DIR` | temporary files | the system's |
| `DOTBOOT_JS_RUNTIMES` | yt-dlp's javascript runtime, e.g. `node` | deno, else node |
| `DOTBOOT_LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |
