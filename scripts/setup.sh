#!/usr/bin/env bash
# Assistente pós-clone (make setup): deixa o dotboot pronto a usar.
#
#   make setup                     pergunta o que fazer em cada passo
#   make setup YES=1 CLI=docker    sem perguntas (CLI=native|docker)
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

step "1/4 Pré-requisitos"
docker_ok=0
if have docker && docker info >/dev/null 2>&1; then
	docker_ok=1
	echo "  ✅ $(docker --version)"
else
	echo "  ℹ️  Docker não encontrado (ou sem permissão): só é preciso para a instalação via Docker."
fi
go_ok=0
if have go; then
	go_ok=1
	echo "  ✅ $(go version)"
else
	echo "  ℹ️  Go não encontrado: só é preciso para compilar a instalação nativa (https://go.dev/dl/)."
fi

step "2/4 Instalação"
cli=${CLI:-}
if [ -z "$cli" ]; then
	# Docker por omissão: traz tudo sem instalar nada no sistema.
	default=native
	if [ "$docker_ok" = 1 ]; then
		default=docker
	fi
	cli=$default
	if [ "$interactive" = 1 ]; then
		echo "  1) docker  tudo numa imagem (~300MB); nada instalado no sistema"
		echo "  2) native  binário em $BINDIR + yt-dlp, deno e ffmpeg no sistema (precisa de Go)"
		read -r -p "Como queres instalar? [$default] " answer
		case "${answer:-$default}" in
		1 | docker) cli=docker ;;
		2 | native) cli=native ;;
		esac
	fi
fi

case "$cli" in
docker)
	if [ "$docker_ok" = 0 ]; then
		echo "  ❌ A instalação via Docker precisa do Docker (https://docs.docker.com/get-docker/)." >&2
		exit 1
	fi
	make docker-build
	make install-docker
	;;
native)
	if [ "$go_ok" = 0 ]; then
		echo "  ❌ A instalação nativa precisa do Go. Instala-o ou usa CLI=docker." >&2
		exit 1
	fi
	make deps
	make install
	;;
*)
	echo "  CLI inválida: $cli (usa docker ou native)" >&2
	exit 2
	;;
esac

step "3/4 Configuração"
scripts/configure-env.sh

step "4/4 Diagnóstico"
# O doctor sai com 1 quando falta algo: o relatório já diz o quê.
"$BINDIR/dotboot" doctor || true

echo
echo "Pronto. Abre a interface com: dotboot"
echo "(se o comando não for encontrado, acrescenta $BINDIR ao PATH)"
