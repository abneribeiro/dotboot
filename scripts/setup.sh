#!/usr/bin/env bash
# Assistente pós-clone (make setup): deixa o dotboot pronto a usar.
#
#   make setup                         pergunta como instalar
#   make setup YES=1 INSTALL=docker    sem perguntas (INSTALL=docker|native)
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

step "1/3 Pré-requisitos"
docker_ok=0
if have docker && docker info >/dev/null 2>&1; then
	docker_ok=1
	echo "  ✅ $(docker --version)"
else
	echo "  ℹ️  Docker não encontrado (ou sem permissão): só é preciso para a instalação via Docker."
fi
native_ok=1
for tool in go npm; do
	if have "$tool"; then
		case "$tool" in
		go) echo "  ✅ $(go version)" ;;
		npm) echo "  ✅ node $(node --version), npm $(npm --version)" ;;
		esac
	else
		native_ok=0
		echo "  ℹ️  $tool não encontrado: só é preciso para compilar a instalação nativa."
	fi
done

step "2/3 Instalação"
mode=${INSTALL:-${CLI:-}}
if [ -z "$mode" ]; then
	# Docker por omissão: traz tudo (incluindo a separação de músicas) sem
	# instalar nada no sistema.
	default=native
	if [ "$docker_ok" = 1 ]; then
		default=docker
	fi
	mode=$default
	if [ "$interactive" = 1 ]; then
		echo "  1) docker  tudo numa imagem (~3GB), incluindo a separação; nada instalado no sistema"
		echo "  2) native  binário em $BINDIR + yt-dlp, deno e ffmpeg no sistema (precisa de Go e Node);"
		echo "             a separação instala-se à parte com make deps-stems"
		read -r -p "Como queres instalar? [$default] " answer
		case "${answer:-$default}" in
		1 | docker) mode=docker ;;
		2 | native) mode=native ;;
		esac
	fi
fi

case "$mode" in
docker)
	if [ "$docker_ok" = 0 ]; then
		echo "  ❌ A instalação via Docker precisa do Docker (https://docs.docker.com/get-docker/)." >&2
		exit 1
	fi
	make docker-build
	make install-docker
	;;
native)
	if [ "$native_ok" = 0 ]; then
		echo "  ❌ A instalação nativa precisa do Go (https://go.dev/dl/) e do Node (https://nodejs.org)." >&2
		echo "     Ou usa INSTALL=docker." >&2
		exit 1
	fi
	make deps
	make install
	if [ "$interactive" = 1 ]; then
		read -r -p "Instalar também a separação de músicas (~1.5GB, precisa de um compilador C)? [s/N] " answer
		case "$answer" in [sSyY]*) make deps-stems ;; esac
	fi
	;;
*)
	echo "  Instalação inválida: $mode (usa docker ou native)" >&2
	exit 2
	;;
esac

step "3/3 Pronto"
echo "  Arranca com: dotboot"
echo "  Abre a página no browser (http://localhost:8765). Em Definições → Diagnóstico"
echo "  vês se falta alguma coisa."
if [ "$mode" = docker ]; then
	echo "  Para o deixar sempre ligado em segundo plano: dotboot up (e dotboot down para parar)."
fi
echo "  (se o comando não for encontrado, acrescenta $BINDIR ao PATH)"
