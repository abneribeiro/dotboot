# **usage**

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
