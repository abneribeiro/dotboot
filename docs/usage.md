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

| flag | meaning |
|---|---|
| `-q, --quality <res>` | `best` (default) or a maximum short side: `2160`, `1080`, `720`… |
| `-o, --output <dir>` | output folder; a relative one goes inside the configured folder |
| `--original` | keep the site's format (`.webm`/`.mkv`) instead of mp4 |
| `--start`, `--end <time>` | trim, `MM:SS` or `HH:MM:SS`, one url only |
| `-a, --batch <file>` | read urls from a file |
| `--cookies <file>` | netscape cookies for logins and bot checks |
| `-f, --format mp3\|m4a` | audio only; default `mp3` |

flags can go before or after the urls. in zsh, quote urls that contain `?`.

**why 1080p is the sweet spot**: up to 1080p youtube serves h.264, so the mp4
needs no conversion. above it only vp9/av1 exist, and dotboot converts them,
which takes a while.
