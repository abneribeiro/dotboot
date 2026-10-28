#!/usr/bin/env bash
# Cria ou atualiza o .env a partir do .env.example. Nunca apaga o que já lá
# está: só altera as chaves a que o utilizador responder, e nunca mostra o
# token.
#
#   scripts/configure-env.sh                 interativo (make env)
#   scripts/configure-env.sh --check-token   falha se o DISCORD_TOKEN não estiver definido
#   YES=1 DISCORD_TOKEN=… scripts/configure-env.sh   sem perguntas: aplica o que vier do ambiente
set -euo pipefail

cd "$(dirname "$0")/.."

ENV_FILE=.env
EXAMPLE=.env.example
PLACEHOLDER=your_discord_bot_token_here

# get_key KEY → valor atual no .env (vazio se ausente ou comentado).
get_key() {
	[ -f "$ENV_FILE" ] || return 0
	sed -n "s/^$1=//p" "$ENV_FILE" | tail -n 1
}

# set_key KEY VALUE → substitui a linha KEY= (ou #KEY=) ou acrescenta-a no fim.
# O valor passa pelo ambiente e não pelo sed/awk -v, que interpretariam
# barras e "&" dentro de um token.
set_key() {
	local tmp
	tmp=$(mktemp "$ENV_FILE.XXXXXX")
	K="$1" V="$2" awk '
		BEGIN { k = ENVIRON["K"]; v = ENVIRON["V"]; done = 0 }
		!done && $0 ~ ("^#?[ \t]*" k "=") { print k "=" v; done = 1; next }
		{ print }
		END { if (!done) print k "=" v }
	' "$ENV_FILE" >"$tmp"
	chmod 600 "$tmp"
	mv "$tmp" "$ENV_FILE"
}

token_ok() {
	local token
	token=$(get_key DISCORD_TOKEN)
	[ -n "$token" ] && [ "$token" != "$PLACEHOLDER" ]
}

if [ "${1:-}" = "--check-token" ]; then
	if token_ok; then
		exit 0
	fi
	echo "❌ DISCORD_TOKEN não está definido no $ENV_FILE. Configura-o com: make env" >&2
	exit 1
fi

if [ ! -f "$ENV_FILE" ]; then
	cp "$EXAMPLE" "$ENV_FILE"
	# Tem o token do bot: só o dono o lê.
	chmod 600 "$ENV_FILE"
	echo "✅ Criado $ENV_FILE a partir de $EXAMPLE"
else
	echo "ℹ️  $ENV_FILE já existe: só muda o que responderes (Enter mantém o valor atual)."
fi

# Sem terminal (CI, make setup YES=1), aplica-se o que vier do ambiente.
if [ "${YES:-0}" = 1 ] || [ ! -t 0 ]; then
	for key in DISCORD_TOKEN DOTBOOT_GUILD_ID DOTBOOT_OUTPUT_DIR DOTBOOT_COOKIES_FILE; do
		if [ -n "${!key:-}" ]; then
			set_key "$key" "${!key}"
			echo "✅ $key definido a partir do ambiente"
		fi
	done
	exit 0
fi

echo
if token_ok; then
	read -r -s -p "Token do bot Discord [já definido; Enter mantém]: " token
else
	echo "O token está no Discord Developer Portal → a tua aplicação → Bot → Reset Token."
	read -r -s -p "Token do bot Discord (Enter para saltar; só é preciso para o bot): " token
fi
echo
if [ -n "$token" ]; then
	# O "Bot " é acrescentado pelo dotboot; colado com o token dava "Bot Bot …".
	set_key DISCORD_TOKEN "${token#Bot }"
	echo "✅ Token guardado."
fi

guild=$(get_key DOTBOOT_GUILD_ID)
echo
echo "ID do servidor para o /baixar aparecer logo (útil em testes; vazio = todos os servidores,"
read -r -p "o que pode demorar até 1 hora) [${guild:-vazio}]: " answer
if [ -n "$answer" ]; then
	set_key DOTBOOT_GUILD_ID "$answer"
fi

output=$(get_key DOTBOOT_OUTPUT_DIR)
default_output=${output:-$HOME/Videos}
echo
read -r -p "Pasta base dos downloads da CLI [$default_output]: " answer
answer=${answer:-$default_output}
# A shell não expande o ~ dentro de uma resposta: compara-se o ~ literal.
# shellcheck disable=SC2088
case "$answer" in
"~") answer=$HOME ;;
"~/"*) answer="$HOME/${answer#"~/"}" ;;
esac
if [ "$answer" != "$output" ]; then
	set_key DOTBOOT_OUTPUT_DIR "$answer"
fi

echo
echo "✅ $ENV_FILE atualizado. As restantes opções estão comentadas no próprio ficheiro."
