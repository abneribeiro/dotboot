#!/usr/bin/env bash
# Cria ou atualiza o ficheiro de configuração do dotboot
# (~/.config/dotboot/config.env, o mesmo que a TUI edita em Definições).
# Só altera as chaves a que se responder; o resto do ficheiro fica igual.
#
#   scripts/configure-env.sh                 interativo (make config)
#   YES=1 DOTBOOT_OUTPUT_DIR=… scripts/configure-env.sh   sem perguntas
set -euo pipefail

CONFIG_FILE=${DOTBOOT_CONFIG_FILE:-${XDG_CONFIG_HOME:-$HOME/.config}/dotboot/config.env}

# get_key KEY → valor atual (vazio se ausente).
get_key() {
	[ -f "$CONFIG_FILE" ] || return 0
	sed -n "s/^$1=//p" "$CONFIG_FILE" | tail -n 1 | sed 's/^"\(.*\)"$/\1/'
}

# set_key KEY VALUE → substitui a linha KEY= ou acrescenta-a; vazio apaga-a.
# O valor passa pelo ambiente e não pelo sed/awk -v, que interpretariam
# barras e "&". Valores com espaços vão entre aspas, para o dotboot os ler.
set_key() {
	local value=$2 tmp
	case "$value" in *[[:space:]]* | *'#'*) value="\"$value\"" ;; esac
	mkdir -p "$(dirname "$CONFIG_FILE")"
	touch "$CONFIG_FILE"
	tmp=$(mktemp "$CONFIG_FILE.XXXXXX")
	K="$1" V="$value" awk '
		BEGIN { k = ENVIRON["K"]; v = ENVIRON["V"]; done = 0 }
		!done && $0 ~ ("^#?[ \t]*" k "=") { if (v != "\"\"" && v != "") print k "=" v; done = 1; next }
		{ print }
		END { if (!done && v != "") print k "=" v }
	' "$CONFIG_FILE" >"$tmp"
	chmod 600 "$tmp"
	mv "$tmp" "$CONFIG_FILE"
}

expand() {
	# A shell não expande o ~ dentro de uma resposta: compara-se o literal.
	# shellcheck disable=SC2088
	case "$1" in
	"~") echo "$HOME" ;;
	"~/"*) echo "$HOME/${1#"~/"}" ;;
	*) echo "$1" ;;
	esac
}

echo "Configuração: $CONFIG_FILE"

# Sem terminal (CI, make setup YES=1), aplica-se o que vier do ambiente.
if [ "${YES:-0}" = 1 ] || [ ! -t 0 ]; then
	for key in DOTBOOT_OUTPUT_DIR DOTBOOT_COOKIES_FILE; do
		if [ -n "${!key:-}" ]; then
			set_key "$key" "$(expand "${!key}")"
			echo "✅ $key definido"
		fi
	done
	exit 0
fi

output=$(get_key DOTBOOT_OUTPUT_DIR)
default_output=${output:-$HOME/Videos}
read -r -p "Pasta base dos downloads [$default_output]: " answer
answer=$(expand "${answer:-$default_output}")
set_key DOTBOOT_OUTPUT_DIR "$answer"

cookies=$(get_key DOTBOOT_COOKIES_FILE)
echo
echo "Ficheiro de cookies (formato Netscape), para vídeos que pedem login ou a"
read -r -p "verificação \"não és um robô\". Enter = ${cookies:-nenhum}: " answer
if [ -n "$answer" ]; then
	answer=$(expand "$answer")
	if [ ! -f "$answer" ]; then
		echo "⚠️  $answer não existe; ignorado."
	else
		set_key DOTBOOT_COOKIES_FILE "$answer"
	fi
fi

echo
echo "✅ Gravado. Também se edita na TUI, em Definições."
