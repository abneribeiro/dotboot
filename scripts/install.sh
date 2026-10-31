#!/usr/bin/env bash
# Installs the latest dotboot release without cloning the repository.
#
# While the repository is private, through an authenticated gh (gh auth login):
#   gh api repos/abneribeiro/dotboot/contents/scripts/install.sh \
#     -H 'Accept: application/vnd.github.raw' | bash
#
# Once the repository is public:
#   curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotboot/main/scripts/install.sh | bash
#
# Variables: DOTBOOT_VERSION (tag, default the latest release), BINDIR
# (default ~/.local/bin), DOTBOOT_DEPS=1 to install yt-dlp, deno and ffmpeg too.
set -euo pipefail

REPO=abneribeiro/dotboot
BINDIR=${BINDIR:-$HOME/.local/bin}
version=${DOTBOOT_VERSION:-}

have() { command -v "$1" >/dev/null 2>&1; }

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*)
	echo "Unsupported system: $(uname -s). On Windows, download the .zip from the releases page." >&2
	exit 1
	;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "Unsupported architecture: $(uname -m)" >&2
	exit 1
	;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

if have gh && gh auth status >/dev/null 2>&1; then
	if [ -z "$version" ]; then
		version=$(gh release view -R "$REPO" --json tagName -q .tagName)
	fi
	asset="dotboot_${version#v}_${os}_${arch}.tar.gz"
	gh release download "$version" -R "$REPO" -D "$tmp" -p "$asset" -p checksums.txt
else
	# Without gh this only works for public repositories: private assets need the authenticated API.
	if [ -z "$version" ]; then
		version=$(curl -fsSI "https://github.com/$REPO/releases/latest" | tr -d '\r' |
			grep -i '^location:' | sed 's#.*/tag/##') || true
	fi
	if [ -z "$version" ]; then
		echo "Cannot find the latest release. While the repository is private, install and authenticate gh (gh auth login)." >&2
		exit 1
	fi
	asset="dotboot_${version#v}_${os}_${arch}.tar.gz"
	curl -fsSL -o "$tmp/$asset" "https://github.com/$REPO/releases/download/$version/$asset"
	curl -fsSL -o "$tmp/checksums.txt" "https://github.com/$REPO/releases/download/$version/checksums.txt"
fi

expected=$(awk -v f="$asset" '$2 == f {print $1}' "$tmp/checksums.txt")
if have sha256sum; then
	actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else
	actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
	echo "Bad checksum for $asset: the download is corrupted or was tampered with." >&2
	exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$BINDIR"
install -m 0755 "$tmp/dotboot" "$BINDIR/dotboot"
echo "✓ dotboot $("$BINDIR/dotboot" version) installed in $BINDIR"

case ":$PATH:" in
*":$BINDIR:"*) ;;
*) echo "! Add $BINDIR to your PATH: export PATH=\"$BINDIR:\$PATH\"" ;;
esac

# The dependencies (yt-dlp, deno, ffmpeg): the script ships in the archive.
# When run through "| bash", stdin is the script, so the question goes to /dev/tty.
want_deps=${DOTBOOT_DEPS:-}
if [ -z "$want_deps" ]; then
	if ! have yt-dlp || ! have ffmpeg; then
		printf "Install yt-dlp, deno and ffmpeg now? [Y/n] " >/dev/tty 2>/dev/null || true
		if read -r answer </dev/tty 2>/dev/null; then
			case "${answer:-y}" in [yY]*) want_deps=1 ;; esac
		fi
	fi
fi
if [ "$want_deps" = 1 ]; then
	BINDIR=$BINDIR bash "$tmp/scripts/install-deps.sh" </dev/tty
fi

"$BINDIR/dotboot" doctor || true
