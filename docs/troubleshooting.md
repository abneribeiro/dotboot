# **troubleshooting**

run `dotboot doctor` first: it lists what is missing or outdated and how to
fix it.

-   **"sign in to confirm you're not a bot"**: export your browser's cookies
    (e.g. the "get cookies.txt locally" extension) and run
    `dotboot config cookies <file>`.
-   **a site stopped working**: update yt-dlp (`yt-dlp -U`, or rerun the
    installer with `DOTBOOT_DEPS=1`).
-   **`dotboot: command not found`**: add `~/.local/bin` to your `PATH`.
