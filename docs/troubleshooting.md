# **troubleshooting**

Run `dotboot doctor` first: it lists what is missing or outdated and how to
fix it.

- **"Sign in to confirm you're not a bot"**: export your browser's cookies
  (e.g. the "Get cookies.txt LOCALLY" extension) and run
  `dotboot config cookies <file>`.
- **A site stopped working**: update yt-dlp (`yt-dlp -U`, or rerun the
  installer with `DOTBOOT_DEPS=1`).
- **`dotboot: command not found`**: add `~/.local/bin` to your `PATH`.
