# **install**

## **macos and linux**

```bash
curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotboot/main/scripts/install.sh | bash
```

Installs the latest release into `~/.local/bin` (checksum verified), offers to
install yt-dlp, FFmpeg and Deno, and runs `dotboot doctor`. Run it again to
update. Options, set before `bash`: `DOTBOOT_VERSION=v0.1.0`,
`BINDIR=/usr/local/bin`, `DOTBOOT_DEPS=1` (dependencies without asking).

If `dotboot` is not found, add `~/.local/bin` to your `PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # or ~/.bashrc
```

## **go**

```bash
go install github.com/abneribeiro/dotboot/cmd/dotboot@latest
brew install yt-dlp ffmpeg deno   # or your package manager
```

## **windows**

Download the `.zip` for your architecture from
[Releases](https://github.com/abneribeiro/dotboot/releases/latest), put
`dotboot.exe` on your `PATH`, and install the tools:

```powershell
winget install yt-dlp.yt-dlp Gyan.FFmpeg DenoLand.Deno
```

## **docker (nothing else on your machine)**

The image brings yt-dlp, FFmpeg and Deno; `dotboot` becomes a small script
that runs it as your user, with your home mounted at the same path.

```bash
# macOS: OrbStack, the lightest Docker engine (free for personal use)
brew install --cask orbstack && open -a OrbStack
# Linux and WSL: Docker Engine (https://docs.docker.com/engine/install/)

git clone https://github.com/abneribeiro/dotboot && cd dotboot
make docker-build && make install-docker
```

The image takes about 1 GB. For an open-source engine on macOS, use Colima
instead (`brew install colima docker docker-buildx && colima start`); it only
shares your home, so save downloads inside it (e.g. `-o ~/Videos`).

## **from source**

```bash
git clone https://github.com/abneribeiro/dotboot && cd dotboot
make setup   # native (Go 1.26+) or Docker, then the output folder
```

**Uninstall**: `rm ~/.local/bin/dotboot`, and optionally `~/.config/dotboot`
(and `docker rmi dotboot:latest` for Docker).
