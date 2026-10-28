#!/usr/bin/env bash
# Assistente pós-clone (make setup): deixa o dotboot pronto a usar.
#
#   make setup                     pergunta o que fazer em cada passo
#   make setup YES=1 CLI=native BOT=1 STEMS=0
#                                  sem perguntas (CLI=native|docker|none)
#
# Nunca sobrescreve o .env: só acrescenta ou altera o que for respondido.
set -euo pipefail

cd "$(dirname "$0")/.."

BINDIR=${BINDIR:-$HOME/.local/bin}
IMAGE=${IMAGE:-dotboot:latest}
YES=${YES:-0}
interactive=1
if [ "$YES" = 1 ] || [ ! -t 0 ]; then
	interactive=0
fi

have() { command -v "$1" >/dev/null 2>&1; }
step() { printf '\n\033[1m==> %s\033[0m\n' "$*"; }

# ask "pergunta" s|n — devolve 0 para sim. Sem terminal, vale o default.
ask() {
	local answer hint="[s/N]"
	if [ "$2" = s ]; then
		hint="[S/n]"
	fi
	if [ "$interactive" = 0 ]; then
		[ "$2" = s ]
		return
	fi
	read -r -p "$1 $hint " answer
	case "${answer:-$2}" in [sSyY]*) return 0 ;; *) return 1 ;; esac
}

step "1/5 Pré-requisitos"
docker_ok=0
if have docker && docker compose version >/dev/null 2>&1; then
	docker_ok=1
	echo "  ✅ $(docker --version)"
else
	echo "  ⚠️  Docker com o plugin compose não encontrado: o bot e a CLI via Docker precisam dele"
	echo "     (https://docs.docker.com/get-docker/)."
fi
go_ok=0
if have go; then
	go_ok=1
	echo "  ✅ $(go version)"
else
	echo "  ℹ️  Go não encontrado: só é preciso para compilar a CLI nativa (https://go.dev/dl/)."
fi

step "2/5 Configuração (.env)"
scripts/configure-env.sh

step "3/5 CLI (dotboot download / stems)"
cli=${CLI:-}
if [ -z "$cli" ]; then
	default=none
	if [ "$go_ok" = 1 ]; then
		default=native
	elif [ "$docker_ok" = 1 ]; then
		default=docker
	fi
	cli=$default
	if [ "$interactive" = 1 ]; then
		echo "  1) native  binário em $BINDIR + yt-dlp, deno e ffmpeg no host (precisa de Go)"
		echo "  2) docker  wrapper que corre tudo num container (só precisa de Docker)"
		echo "  3) none    não instalar a CLI"
		read -r -p "Como queres usar a CLI? [$default] " answer
		case "${answer:-$default}" in
		1 | native) cli=native ;;
		2 | docker) cli=docker ;;
		*) cli=none ;;
		esac
	fi
fi

image_built=0
case "$cli" in
native)
	if [ "$go_ok" = 0 ]; then
		echo "  ❌ A CLI nativa precisa do Go. Instala-o ou escolhe CLI=docker." >&2
		exit 1
	fi
	make deps
	make install
	;;
docker)
	if [ "$docker_ok" = 0 ]; then
		echo "  ❌ A CLI via Docker precisa do Docker." >&2
		exit 1
	fi
	make docker-build
	image_built=1
	make install-docker
	;;
none) echo "  CLI não instalada (podes voltar a correr make setup)." ;;
*)
	echo "  CLI inválida: $cli (usa native, docker ou none)" >&2
	exit 2
	;;
esac

step "4/5 Imagens Docker e bot"
if [ "$docker_ok" = 1 ]; then
	if [ "$image_built" = 0 ]; then
		make docker-build
	fi

	if [ "${STEMS:-}" = 1 ] || { [ -z "${STEMS:-}" ] && ask "Construir também a imagem de stems (~1.5GB, separa guitarra/baixo/voz)?" n; }; then
		make stems-build
	fi

	if scripts/configure-env.sh --check-token 2>/dev/null; then
		if [ "${BOT:-}" = 1 ] || { [ -z "${BOT:-}" ] && ask "Arrancar o bot agora?" s; }; then
			make up
		fi
	else
		echo "  ℹ️  Sem DISCORD_TOKEN no .env: o bot fica por arrancar (make env, depois make up)."
	fi
else
	echo "  ℹ️  Sem Docker: o bot pode correr com \`dotboot bot\` (CLI nativa) ou depois de instalares o Docker."
fi

step "5/5 Diagnóstico"
case "$cli" in
native) "$BINDIR/dotboot" doctor || true ;;
docker) docker run --rm "$IMAGE" doctor || true ;;
*) if [ "$go_ok" = 1 ]; then make doctor || true; fi ;;
esac

echo
echo "Pronto. Os comandos todos: make help"
