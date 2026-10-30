# dotboot — vídeos e música no terminal

O **dotboot** descarrega vídeos e música de qualquer site suportado pelo
**yt-dlp** (YouTube, X/Twitter, Instagram, TikTok, SoundCloud, Facebook…),
na melhor qualidade e num formato que toca em todo o lado: o iPhone, o
Android, o Windows, o macOS e o WhatsApp. Tem uma **interface de terminal**
(TUI) para usar no dia a dia e **subcomandos** para scripts.

```
 ▶ dotboot  v1.0.0                                    ⣾ a correr · 1 na fila
 vídeos e música no terminal
 ──────────────────────────────────────────────────────────────────────────
 O que queres fazer?

 ▸ 1  🎬 Descarregar vídeo
         MP4 que toca em todo o lado · playlists · recortes
   2  🎵 Extrair áudio
         MP3 ou M4A com título e capa
   3  📋 Trabalhos
   4  🩺 Diagnóstico
   5  ⚙️  Definições
```

## ✨ O que faz

**Vídeo**
- 📱 Sempre **MP4 com H.264 + AAC**, o formato que o iPhone, o Android e o
  WhatsApp abrem sem conversão (com a opção de guardar o original)
- 🎞️ Melhor qualidade por omissão, ou uma resolução máxima (720p num vídeo
  vertical dá 720x1280)
- 📜 Playlists e canais inteiros, vários URLs de uma vez, ficheiros de URLs
- ✂️ Recortes (`01:30`–`02:00`)
- ⏯️ **Continuar**: repetir o pedido salta o que já foi descarregado e retoma o
  que ficou a meio, incluindo conversões interrompidas

**Áudio**
- 🎵 MP3 ou M4A, com título, artista e capa nos metadados

**Interface**
- Formulários com validação, fila de trabalhos com barra de progresso,
  velocidade e tempo restante, log de cada trabalho, cancelar e repetir
- Diagnóstico do que está instalado, e definições gravadas

## 🚀 Instalação

```bash
git clone https://github.com/abneribeiro/dotboot
cd dotboot
make setup
```

O `make setup` pergunta como queres instalar, configura a pasta dos downloads
e no fim corre o diagnóstico. Há duas formas:

| | Docker (recomendado) | Nativa |
|---|---|---|
| Comando | `make docker-build && make install-docker` | `make deps && make install` |
| Precisa de | Docker | Go 1.26+ (o `make deps` instala yt-dlp, deno e ffmpeg) |
| Espaço | ~300MB | o binário + yt-dlp, deno e ffmpeg |

As duas instalam o comando `dotboot` em `~/.local/bin` (muda com
`make install PREFIX=/usr/local`). Se o comando não for encontrado,
acrescenta essa pasta ao `PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # ou ~/.bashrc
```

**Via Docker**, o `dotboot` é um pequeno script (`scripts/dotboot-docker`) que
corre a imagem `dotboot:latest` com o teu utilizador e a tua home montada no
mesmo caminho: os ficheiros ficam teus, e `~`, caminhos absolutos e as
definições funcionam como na versão nativa. Também monta a pasta de saída e a
dos cookies das Definições e, no WSL, os discos do Windows (`/mnt/c`…). Uma
pasta fora destas, escolhida depois de abrir o dotboot, só fica acessível
depois de o fechar e voltar a abrir.

**Outras formas** (só a CLI nativa; as dependências instalam-se à parte com o
`scripts/install-deps.sh` que vem no archive):

```bash
# Última release do GitHub (enquanto o repo for privado, com o gh autenticado)
gh api repos/abneribeiro/dotboot/contents/scripts/install.sh \
  -H 'Accept: application/vnd.github.raw' | bash

# go install (com o repo privado: gh auth setup-git e GOPRIVATE)
GOPRIVATE=github.com/abneribeiro/* go install github.com/abneribeiro/dotboot/cmd/dotboot@latest
```

## 🎯 Como usar

### A interface (TUI)

```bash
dotboot
```

| Ecrã | Teclas |
|---|---|
| Menu | `↑`/`↓` ou `1`–`5` para escolher, `enter` abrir, `tab` trabalhos, `q` sair |
| Formulários | `enter` seguinte, `shift+tab` anterior, `esc` cancelar |
| Trabalhos | `enter` detalhes e log, `c` cancelar, `r` repetir, `o` abrir a pasta, `x` limpar os terminados, `esc` menu |

Os trabalhos correm um de cada vez, por ordem, enquanto continuas a usar a
interface. Sair (`q` ou `ctrl+c`) com trabalhos a correr pede confirmação e
cancela-os; o que ficou a meio retoma-se ao repetir.

### Os subcomandos (scripts)

`dotboot download -h` mostra a ajuda completa. As flags
podem vir antes ou depois dos URLs.

```bash
# Vídeo em MP4, na melhor qualidade (para $DOTBOOT_OUTPUT_DIR ou o diretório atual)
dotboot download "https://youtube.com/watch?v=xxxx"

# Playlist inteira, limitada a 720p, para ~/Videos/aulas
dotboot download -q 720 -o ~/Videos/aulas "https://youtube.com/playlist?list=yyyy"

# Só o áudio, em MP3 com título e capa
dotboot download -formato mp3 -o musica "https://youtube.com/watch?v=xxxx"

# Um recorte (o nome do ficheiro leva o intervalo)
dotboot download -start 01:30 -end 02:00 https://x.com/user/status/123

# Ficheiro com um URL por linha
dotboot download -a urls.txt

# O que está instalado e o que falta
dotboot doctor
```

> No zsh, URLs com `?` (ex: `watch?v=...`) têm de ir entre aspas.

### O formato de vídeo: MP4 que o telemóvel lê

O yt-dlp prefere AV1/VP9 + Opus, que dão `.webm`/`.mkv` — ficheiros que o
iPhone não abre. O dotboot garante MP4 com H.264 + AAC em duas camadas:

1. **Pede os codecs certos** ao yt-dlp. No YouTube até 1080p, no Instagram, no
   TikTok e no X isto resolve tudo, **sem recodificar**.
2. **Verifica o que chegou** e corrige: acima de 1080p o YouTube só tem
   VP9/AV1, e aí o vídeo é recodificado (demora, e o dotboot avisa).

> **Dica**: 1080p é o ponto doce no YouTube — a maior resolução sem
> recodificação. Para guardar o ficheiro tal como vem, escolhe o formato
> *original* (`-formato original`).

**Continuar**: cada pasta guarda o registo do que já foi descarregado
(`.dotboot-archive.txt`, e `.dotboot-archive-audio.txt` para o áudio).
Repetir o pedido descarrega só o que falta; downloads interrompidos (`.part`)
e conversões que ficaram por fazer (`.dotboot-pendentes.txt`) são retomados.

## ⚙️ Configuração

As definições ficam em `~/.config/dotboot/config.env`. Editam-se na TUI
(**Definições**) ou com `make config`; um `.env` no diretório atual e as
variáveis de ambiente também valem (estas têm sempre prioridade).

| Variável | O que é | Default |
|---|---|---|
| `DOTBOOT_OUTPUT_DIR` | Pasta base: usada sem `-o`, e raiz de um `-o` relativo | diretório atual |
| `DOTBOOT_COOKIES_FILE` | Cookies (formato Netscape) para sites que pedem login | — |
| `DOTBOOT_TEMP_DIR` | Ficheiros temporários dos downloads | default do sistema |
| `DOTBOOT_JS_RUNTIMES` | Força o runtime JS do yt-dlp (ex: `node`) | deno, ou node |
| `DOTBOOT_LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` (subcomandos) | `info` |

O `~` é expandido em todos os caminhos. Ver o `.env.example` comentado.

## 🐛 Problemas comuns

O primeiro passo é sempre o **Diagnóstico** (na TUI) ou `dotboot doctor`: diz
o que falta, se o yt-dlp está desatualizado e como resolver.

- **"Confirma que não és um robô" / login pedido**: o YouTube desconfia de
  alguns IPs. Exporta os cookies de um browser com sessão iniciada e indica o
  ficheiro em Definições (ou `-cookies`).
- **Um site que funcionava deixou de funcionar**: o yt-dlp está
  desatualizado. Docker: `make update-ytdlp`; nativo: `yt-dlp -U`.
- **A conversão no fim demora muito**: a fonte não tinha H.264 (acima de
  1080p no YouTube). Escolhe 1080p, ou o formato original.
- **`dotboot: command not found`**: falta `~/.local/bin` no `PATH` (ver
  Instalação).

## 🔧 Desenvolvimento

```bash
make run                      # abre a TUI a partir do código
make check                    # gofmt, vet e testes com -race (o mesmo que o CI)
make test-integration-docker  # yt-dlp e ffmpeg reais, dentro da imagem
make release-snapshot         # binários de todas as plataformas em dist/
make release VERSION=v1.0.0   # cria a tag; o GitHub Actions publica a release
```

```
cmd/dotboot/          Entrada: TUI ou subcomandos
internal/tui/         Interface (Bubble Tea, Lip Gloss, Bubbles, Huh)
internal/cli/         Subcomando download
internal/jobs/        Motor partilhado: valida, executa e relata o progresso
internal/downloader/  yt-dlp e ffmpeg (download, garantia de MP4)
internal/doctor/      Diagnóstico
internal/config/      Configuração e config.env
scripts/              setup, dependências, wrapper Docker, instalação
```

O yt-dlp da imagem está fixado no `Dockerfile` e é verificado contra o
checksum da release; `make update-ytdlp` muda para a última versão. O resto
(Go, imagens base, actions) é atualizado pelo Dependabot.

## 📄 Licença

Ainda sem licença: fornecido como-está, para fins educacionais e pessoais.
