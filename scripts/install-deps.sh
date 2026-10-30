#!/usr/bin/env bash
# Instala as dependências da CLI nativa do dotboot:
#   - yt-dlp e deno em BINDIR (default ~/.local/bin), sem sudo, verificados
#     pelo checksum publicado em cada release;
#   - ffmpeg/ffprobe pelo gestor de pacotes do sistema (pede confirmação
#     antes de usar o sudo).
#
#   scripts/install-deps.sh [--check] [--yes]
#
# Versões: YTDLP_VERSION (default latest) e DENO_VERSION podem ser fixadas
# pelo ambiente.
set -euo pipefail

YTDLP_VERSION=${YTDLP_VERSION:-latest}
DENO_VERSION=${DENO_VERSION:-2.9.8}
BINDIR=${BINDIR:-$HOME/.local/bin}

check_only=0
assume_yes=${YES:-0}
for arg in "$@"; do
	case "$arg" in
	--check) check_only=1 ;;
	--yes | -y) assume_yes=1 ;;
	-h | --help)
		sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "opção desconhecida: $arg" >&2
		exit 2
		;;
	esac
done

have() { command -v "$1" >/dev/null 2>&1; }
ok() { printf '  ✅ %s\n' "$*"; }
warn() { printf '  ⚠️  %s\n' "$*"; }
fail() { printf '  ❌ %s\n' "$*"; }
step() { printf '\n\033[1m%s\033[0m\n' "$*"; }

confirm() {
	local answer
	if [ "$assume_yes" = 1 ]; then
		return 0
	fi
	if [ ! -t 0 ]; then
		return 1
	fi
	read -r -p "$1 [S/n] " answer
	case "${answer:-s}" in [sSyY]*) return 0 ;; *) return 1 ;; esac
}

sha256() {
	if have sha256sum; then
		sha256sum "$1" | awk '{print $1}'
	else
		shasum -a 256 "$1" | awk '{print $1}'
	fi
}

# verify FICHEIRO HASH_ESPERADO — aborta se o download não corresponder ao
# checksum publicado (download corrompido ou adulterado).
verify() {
	local actual
	actual=$(sha256 "$1")
	if [ -z "$2" ] || [ "$actual" != "$2" ]; then
		fail "checksum inválido para $(basename "$1") (esperado ${2:-?}, obtido $actual)"
		exit 1
	fi
}

download() { curl -fsSL --retry 3 -o "$2" "$1"; }

# unzip_to ZIP DIR — usa o unzip quando existe; senão o Python, que vem em
# quase todos os sistemas (o Ubuntu mínimo não traz unzip).
unzip_to() {
	if have unzip; then
		unzip -q -o "$1" -d "$2"
	else
		python3 -I -c 'import sys, zipfile; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])' "$1" "$2"
	fi
}

os=$(uname -s)
arch=$(uname -m)
case "$os/$arch" in
Linux/x86_64)
	ytdlp_asset=yt-dlp_linux
	triple=x86_64-unknown-linux-gnu
	;;
Linux/aarch64 | Linux/arm64)
	ytdlp_asset=yt-dlp_linux_aarch64
	triple=aarch64-unknown-linux-gnu
	;;
Darwin/arm64)
	ytdlp_asset=yt-dlp_macos
	triple=aarch64-apple-darwin
	;;
Darwin/x86_64)
	ytdlp_asset=yt-dlp_macos
	triple=x86_64-apple-darwin
	;;
*)
	echo "Sistema não suportado por este script: $os/$arch. Instala yt-dlp, deno e ffmpeg à mão." >&2
	exit 1
	;;
esac

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

report() {
	local missing=0
	# O que acabou de ser instalado em BINDIR conta, mesmo que BINDIR ainda
	# não esteja no PATH desta shell (o path_hint já avisou).
	PATH="$BINDIR:$PATH"
	step "Dependências"
	if have yt-dlp; then ok "yt-dlp $(yt-dlp --version 2>/dev/null | head -n 1)"; else fail "yt-dlp"; missing=1; fi
	if have ffmpeg && have ffprobe; then
		ok "$(ffmpeg -version 2>/dev/null | head -n 1 | cut -d' ' -f1-3)"
	else
		fail "ffmpeg/ffprobe"
		missing=1
	fi
	if have deno; then
		ok "$(deno --version 2>/dev/null | head -n 1)"
	elif have node; then
		ok "node $(node --version) (o deno é o recomendado pelo yt-dlp)"
	else
		warn "nenhum runtime JavaScript (deno/node): o YouTube terá menos formatos"
	fi
	return "$missing"
}

path_hint() {
	case ":$PATH:" in
	*":$BINDIR:"*) ;;
	*)
		warn "$BINDIR não está no PATH. Acrescenta ao ~/.zshrc ou ~/.bashrc:"
		echo "       export PATH=\"$BINDIR:\$PATH\""
		;;
	esac
}

install_ytdlp() {
	# Um yt-dlp instalado pelo apt costuma estar meses atrasado — e o
	# YouTube parte as versões antigas. O binário oficial atualiza-se sozinho.
	local base
	if [ "$YTDLP_VERSION" = latest ]; then
		base="https://github.com/yt-dlp/yt-dlp/releases/latest/download"
	else
		base="https://github.com/yt-dlp/yt-dlp/releases/download/$YTDLP_VERSION"
	fi
	download "$base/$ytdlp_asset" "$tmp/$ytdlp_asset"
	download "$base/SHA2-256SUMS" "$tmp/SHA2-256SUMS"
	verify "$tmp/$ytdlp_asset" "$(awk -v f="$ytdlp_asset" '$2 == f {print $1}' "$tmp/SHA2-256SUMS")"
	install -m 0755 "$tmp/$ytdlp_asset" "$BINDIR/yt-dlp"
	ok "yt-dlp $("$BINDIR/yt-dlp" --version) em $BINDIR"
}

install_deno() {
	local zip=deno-$triple.zip
	local base="https://github.com/denoland/deno/releases/download/v$DENO_VERSION"
	download "$base/$zip" "$tmp/$zip"
	download "$base/$zip.sha256sum" "$tmp/$zip.sha256sum"
	verify "$tmp/$zip" "$(awk '{print $1; exit}' "$tmp/$zip.sha256sum")"
	mkdir -p "$tmp/deno"
	unzip_to "$tmp/$zip" "$tmp/deno"
	install -m 0755 "$tmp/deno/deno" "$BINDIR/deno"
	ok "$("$BINDIR/deno" --version | head -n 1) em $BINDIR"
}

install_ffmpeg() {
	local cmd
	if have apt-get; then
		cmd="sudo apt-get update && sudo apt-get install -y ffmpeg"
	elif have pacman; then
		cmd="sudo pacman -S --needed --noconfirm ffmpeg"
	elif have zypper; then
		cmd="sudo zypper install -y ffmpeg"
	elif have brew; then
		cmd="brew install ffmpeg"
	elif have dnf; then
		# O ffmpeg-free do Fedora não traz o x264, de que as conversões
		# para MP4 precisam: é preciso o do RPM Fusion.
		warn "No Fedora instala o ffmpeg do RPM Fusion: https://rpmfusion.org/Howto/Multimedia"
		return 1
	else
		warn "Gestor de pacotes não reconhecido: instala o ffmpeg à mão (https://ffmpeg.org/download.html)."
		return 1
	fi
	if ! confirm "Instalar o ffmpeg com: $cmd ?"; then
		warn "ffmpeg por instalar. Corre: $cmd"
		return 1
	fi
	eval "$cmd"
	ok "$(ffmpeg -version | head -n 1 | cut -d' ' -f1-3)"
}

if [ "$check_only" = 1 ]; then
	report
	exit $?
fi

mkdir -p "$BINDIR"

step "yt-dlp"
install_ytdlp

step "Deno (runtime JavaScript que o yt-dlp usa no YouTube)"
if have deno && [ "$(command -v deno)" != "$BINDIR/deno" ]; then
	ok "já instalado: $(deno --version | head -n 1)"
else
	install_deno
fi

step "ffmpeg"
ffmpeg_ok=1
if have ffmpeg && have ffprobe; then
	ok "já instalado: $(ffmpeg -version | head -n 1 | cut -d' ' -f1-3)"
else
	install_ffmpeg || ffmpeg_ok=0
fi

path_hint
report || true
if [ "$ffmpeg_ok" = 0 ]; then
	exit 1
fi
