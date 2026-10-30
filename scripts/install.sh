#!/usr/bin/env bash
# Instala a última release do dotboot (o binário, com a página embutida) sem
# clonar o repo.
#
# Com o repo privado, através do gh autenticado (gh auth login):
#   gh api repos/abneribeiro/dotboot/contents/scripts/install.sh \
#     -H 'Accept: application/vnd.github.raw' | bash
#
# Quando o repo for público:
#   curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotboot/main/scripts/install.sh | bash
#
# Variáveis: DOTBOOT_VERSION (tag, default a última release), BINDIR (default
# ~/.local/bin), DOTBOOT_DEPS=1 para instalar logo yt-dlp, deno e ffmpeg.
set -euo pipefail

REPO=abneribeiro/dotboot
BINDIR=${BINDIR:-$HOME/.local/bin}
version=${DOTBOOT_VERSION:-}

have() { command -v "$1" >/dev/null 2>&1; }

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*)
	echo "Sistema não suportado: $(uname -s). No Windows, descarrega o .zip da página de releases." >&2
	exit 1
	;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*)
	echo "Arquitetura não suportada: $(uname -m)" >&2
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
	# Sem gh só dá para repos públicos: os assets de um repo privado exigem a API autenticada.
	if [ -z "$version" ]; then
		version=$(curl -fsSI "https://github.com/$REPO/releases/latest" | tr -d '\r' |
			grep -i '^location:' | sed 's#.*/tag/##') || true
	fi
	if [ -z "$version" ]; then
		echo "Não foi possível encontrar a última release. Com o repo privado, instala e autentica o gh (gh auth login)." >&2
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
	echo "Checksum inválido para $asset: o download está corrompido ou foi adulterado." >&2
	exit 1
fi

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$BINDIR"
install -m 0755 "$tmp/dotboot" "$BINDIR/dotboot"
echo "✅ $("$BINDIR/dotboot" version) instalado em $BINDIR"

case ":$PATH:" in
*":$BINDIR:"*) ;;
*) echo "⚠️  Acrescenta $BINDIR ao PATH: export PATH=\"$BINDIR:\$PATH\"" ;;
esac

# As dependências (yt-dlp, deno, ffmpeg): o script vem no próprio archive.
# Quando corre via "| bash" o stdin é o script, por isso pergunta-se ao /dev/tty.
want_deps=${DOTBOOT_DEPS:-}
if [ -z "$want_deps" ]; then
	if ! have yt-dlp || ! have ffmpeg; then
		printf "Instalar agora yt-dlp, deno e ffmpeg? [S/n] " >/dev/tty 2>/dev/null || true
		if read -r answer </dev/tty 2>/dev/null; then
			case "${answer:-s}" in [sSyY]*) want_deps=1 ;; esac
		fi
	fi
fi
if [ "$want_deps" = 1 ]; then
	BINDIR=$BINDIR bash "$tmp/scripts/install-deps.sh" </dev/tty
fi

echo
echo "Pronto. Arranca com: dotboot   (abre a página no browser; Definições → Diagnóstico diz o que falta)"
