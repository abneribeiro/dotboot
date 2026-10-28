# dotboot — Discord Video Bot + Downloader CLI

Bot de Discord escrito em Go que descarrega vídeos de redes sociais (YouTube, X/Twitter, Instagram, TikTok, Facebook e qualquer site suportado pelo **yt-dlp**) e faz upload diretamente no canal com o comando `/baixar`. Inclui também uma **CLI** para descarregar vídeos, playlists e canais inteiros para uma pasta local, sem os limites do Discord, e para **separar músicas em pistas** (vozes, bateria, baixo, guitarra, piano).

## ✨ Funcionalidades

### Bot Discord (`dotboot bot`)
- 📥 Download de vídeos de qualquer site suportado pelo yt-dlp
- 📱 Anexos sempre em MP4 (H.264 8 bits + AAC), que tocam inline no Discord e no iPhone
- ✂️ Recorte de vídeos com timestamps (`inicio` e `fim`)
- 📏 Limite de upload detetado por servidor (20MB sem boost, 50MB no Nível 2,
  100MB no Nível 3) — sem recomprimir à toa em servidores impulsionados
- 🗜️ Compressão dirigida ao tamanho: o bitrate é calculado a partir da duração
  e do limite, reduzindo a resolução (pelo lado menor, por isso os vídeos
  verticais ficam nítidos) até o vídeo caber
- 🚦 Limite de downloads simultâneos e por utilizador
- 🛡️ Recusa endereços da rede local; allowlist de domínios opcional
- 🍪 Cookies opcionais para a verificação "não és um robô" do YouTube
- 💬 Erros explicados: quando o site recusa, a mensagem diz porquê
- 🛑 Encerramento gracioso: downloads em curso terminam antes do shutdown
- ❤️ Healthcheck real: o container só está *healthy* com o Discord ligado

### CLI (`dotboot download`)
- 📱 **Sempre MP4 (H.264 + AAC)** — o formato que o iPhone, o iOS Fotos e o
  Android abrem sem qualquer conversão (`-formato original` desliga a garantia)
- 🎵 **Só o áudio** em MP3 ou M4A, com título e capa (`-formato mp3`)
- 🎞️ Melhor qualidade disponível por omissão; `-q 720` limita a resolução
  (também em vídeos verticais: dá 720x1280)
- 📜 Playlists e canais inteiros num só comando
- 📄 Ficheiro de URLs (`-a urls.txt`) e vários URLs de uma vez
- ⏯️ **Continuar**: re-executar o mesmo comando salta o que já foi descarregado
  e retoma o que ficou a meio — incluindo conversões para MP4 interrompidas
- 📁 Caminho base por variável de ambiente (`DOTBOOT_OUTPUT_DIR`)

### Stems (`dotboot stems`)
- 🎸 Separa uma música (URL ou ficheiro) em **vozes, bateria, baixo,
  guitarra, piano e outros**
- 🎤 Modo karaoke: vozes + instrumental, com o melhor modelo para a voz
- 🎧 Faixa de acompanhamento: `-sem guitarra` gera a música sem a guitarra,
  para tocar por cima

## 🚀 Instalação

O caminho mais curto, depois de clonar:

```bash
git clone https://github.com/abneribeiro/dotboot
cd dotboot
make setup
```

O `make setup` é um assistente: cria o `.env` (pede o token do bot sem o
mostrar), pergunta como queres a CLI, constrói as imagens Docker, arranca o
bot e no fim corre o `dotboot doctor`, que diz o que falta e como resolver.
Sem perguntas: `make setup YES=1 CLI=native BOT=1 STEMS=0`.

`make` sozinho mostra todos os comandos.

### Pré-requisitos

- **Bot**: [Docker](https://docs.docker.com/get-docker/) com o plugin compose,
  e um token de bot do [Discord Developer Portal](https://discord.com/developers/applications)
- **CLI nativa**: Go (para compilar); o `make deps` instala o resto
- **CLI via Docker**: só o Docker

### As quatro formas de instalar a CLI

| Forma | Comando | Precisa de |
|---|---|---|
| Nativa, a partir do clone | `make deps && make install` | Go |
| Via Docker (nada no host) | `make docker-build && make install-docker` | Docker |
| Release do GitHub | `scripts/install.sh` (ver abaixo) | `gh` autenticado enquanto o repo for privado |
| `go install` | `go install github.com/abneribeiro/dotboot/cmd/dotboot@latest` | Go |

Todas instalam o comando `dotboot` em `~/.local/bin` (muda com
`make install PREFIX=/usr/local`).

**Nativa.** O `make deps` instala o **yt-dlp** e o **deno** em `~/.local/bin`,
sem sudo, verificando o checksum publicado em cada release, e o **ffmpeg**
pelo gestor de pacotes do sistema (pede confirmação antes do sudo). O deno é
o runtime JavaScript com que o yt-dlp resolve os desafios do YouTube; sem
ele há menos formatos ou erros.

**Via Docker.** O wrapper (`scripts/dotboot-docker`) corre a CLI dentro da
imagem `dotboot:latest`, com os ficheiros a pertencerem-te. Caminhos
absolutos (`-o ~/Videos`, um ficheiro de música) são montados no mesmo sítio
dentro do container; os relativos ficam dentro de `$DOTBOOT_OUTPUT_DIR`
(default `~/Videos`). Aceita também `dotboot <url>` sem o `download`.

**Release.** Cada tag `vX.Y.Z` publica binários para Linux, macOS e Windows
(amd64 e arm64). Para instalar a última sem clonar:

```bash
# Enquanto o repo for privado (precisa de: gh auth login)
gh api repos/abneribeiro/dotboot/contents/scripts/install.sh \
  -H 'Accept: application/vnd.github.raw' | bash

# Quando for público
curl -fsSL https://raw.githubusercontent.com/abneribeiro/dotboot/main/scripts/install.sh | bash
```

O script confere o `checksums.txt` e oferece-se para instalar as dependências.

**go install.** Com o repo privado, o Go precisa de saber que não deve ir ao
proxy público:

```bash
gh auth setup-git
GOPRIVATE=github.com/abneribeiro/* go install github.com/abneribeiro/dotboot/cmd/dotboot@latest
```

### O bot (Docker)

```bash
make env      # cria/edita o .env (token, servidor de testes, pasta da CLI)
make up       # constrói e arranca em segundo plano
make ps       # estado, incluindo o healthcheck
make logs     # segue os logs
make down     # para
```

## 🎯 Como Usar

### No Discord

Use o comando `/baixar` com os seguintes parâmetros:

- **url** (obrigatório): Link do vídeo
- **inicio** (opcional): Tempo de início no formato `MM:SS` ou `HH:MM:SS`
- **fim** (opcional): Tempo de fim no formato `MM:SS` ou `HH:MM:SS`

```
/baixar url:https://youtu.be/dQw4w9WgXcQ
/baixar url:https://youtu.be/dQw4w9WgXcQ inicio:00:30 fim:01:30
/baixar url:https://twitter.com/user/status/123456789
```

### No terminal

`dotboot help`, `dotboot download -h` e `dotboot stems -h` mostram a ajuda
completa. As flags podem vir antes ou depois dos URLs.

```bash
# Um vídeo para o diretório atual (ou para $DOTBOOT_OUTPUT_DIR)
dotboot download "https://youtube.com/watch?v=xxxx"

# Playlist ou canal inteiro para uma pasta
dotboot download -o ~/Videos "https://youtube.com/playlist?list=yyyy"

# Ficheiro com um URL por linha
dotboot download -a urls.txt -o ~/Videos

# Limitar a resolução a 720p (num vídeo vertical dá 720x1280)
dotboot download -q 720 -o aulas "https://youtube.com/watch?v=xxxx"

# Só o áudio, em MP3 com título e capa
dotboot download -formato mp3 -o musica "https://youtube.com/watch?v=xxxx"

# O ficheiro tal como o site o serve (pode ser .webm/.mkv): sem conversão
dotboot download -formato original "https://youtube.com/watch?v=xxxx"

# Recorte de um vídeo (um único URL); o nome leva o intervalo
dotboot download -start 01:30 -end 02:00 https://x.com/user/status/123

# Vídeo que pede login ou a verificação "não és um robô"
dotboot download -cookies ~/cookies.txt "https://youtube.com/watch?v=xxxx"

# Ver o que falta instalar ou configurar
dotboot doctor
```

> No zsh, URLs com `?` (ex: `watch?v=...`) têm de ir entre aspas, ou a shell
> tenta expandi-los como glob.

**Continuar downloads**: o modo CLI guarda um registo dos vídeos já
descarregados em `<pasta>/.dotboot-archive.txt` (`.dotboot-archive-audio.txt`
para MP3/M4A). Re-executar o mesmo comando — depois de uma interrupção, ou
quando a playlist tem vídeos novos — descarrega apenas o que falta. Downloads
interrompidos (CTRL+C) deixam ficheiros `.part` que são retomados, e as
conversões para MP4 que ficaram por fazer (listadas em `.dotboot-pendentes.txt`)
são feitas na execução seguinte.

Os ficheiros são gravados como `Título do Vídeo [id].mp4`; um recorte fica
`Título do Vídeo [id] (01.30-02.00).mp4`, para não se confundir com o vídeo
completo.

### 🎸 Separar uma música em pistas (`dotboot stems`)

```bash
# Todas as pistas: vozes, bateria, baixo, guitarra, piano e outros
dotboot stems "https://youtube.com/watch?v=xxxx"

# Para praticar guitarra: a guitarra isolada e a música sem ela
dotboot stems -so guitarra -sem guitarra -o praticar ~/Musica/song.mp3

# Karaoke: só o instrumental, com o melhor modelo para a voz
dotboot stems -modelo voz -so instrumental "https://youtube.com/watch?v=xxxx"

# Um pedaço, em FLAC
dotboot stems -formato flac -start 01:00 -end 02:30 "https://youtube.com/watch?v=xxxx"
```

Cada música fica numa pasta: `<saída>/<título>/guitarra.mp3`,
`baixo.mp3`, `sem-guitarra.mp3`…

| `-modelo` | Pistas | Notas |
|---|---|---|
| `6s` (default) | vozes, bateria, baixo, guitarra, piano, outros | HT-Demucs 6 pistas; o piano é a mais fraca |
| `4s` | vozes, bateria, baixo, outros | HT-Demucs afinado: mais limpo, sem guitarra |
| `voz` | vozes, instrumental | BS-RoFormer, o melhor para isolar a voz |

A separação usa o [audio-separator](https://github.com/nomadkaraoke/python-audio-separator)
e corre no CPU: conta com alguns minutos por música. Os modelos descarregam-se
na primeira utilização de cada um para `$DOTBOOT_MODELS_DIR`. Para instalar:

- **Docker** (recomendado): `make stems-build` — a imagem `dotboot-stems`
  (~3GB, com o PyTorch) é usada pelo wrapper quando corres `dotboot stems`.
  Os modelos ficam no volume `dotboot-models`.
- **Nativa**: `make deps-stems` instala o audio-separator com o
  [uv](https://docs.astral.sh/uv/), num Python 3.12 só dele. Precisa de um
  compilador C (`sudo apt install build-essential`).

### 📱 Formato de saída: MP4 que o telemóvel lê

Por omissão **todos os downloads saem em MP4 com vídeo H.264 e áudio AAC** — a
única combinação que o iPhone (incluindo a app Fotos), o Android e o player
inline do Discord reproduzem sem conversão nenhuma da tua parte.

Isto não é automático no yt-dlp: por omissão ele prefere AV1/VP9 + Opus, que
produzem `.webm` ou `.mkv`. O dotboot garante o MP4 em duas camadas:

1. **Pedir os codecs certos** (`-S <res>,vcodec:h264,acodec:aac` mais
   `--merge-output-format mp4`). No YouTube até 1080p, no Instagram, no TikTok
   e no X isto resolve tudo — o ficheiro chega já em MP4/H.264, **sem
   recodificar e sem perder qualidade**.
2. **Verificar e corrigir** o que aterrou, com `ffprobe` + `ffmpeg`. É esta
   camada que apanha as fontes que simplesmente não têm H.264 — acima de 1080p
   o YouTube só serve VP9/AV1. Só aí há recodificação, e o dotboot avisa-te
   antes de a começar.

| Situação | O que acontece | Custo |
|---|---|---|
| Fonte já em H.264/AAC (a maioria) | usa-se tal e qual | um `ffprobe` |
| Codecs certos, contentor errado (`.webm`/`.mkv`) | remux para `.mp4` | segundos, sem perdas |
| Só o áudio incompatível (Opus) | recodifica-se só o áudio | rápido |
| Fonte sem H.264 (YouTube >1080p, AV1) | recodifica-se o vídeo | lento, com aviso |

Se preferires a qualidade máxima bruta e não te importas de converter à mão,
`-formato original` devolve o comportamento do yt-dlp puro.

> **Dica**: `-q 1080` é o ponto doce no YouTube — é a maior resolução com H.264
> disponível, por isso descarrega no máximo de qualidade *sem* recodificar.
> Acima disso a conversão é inevitável e pode demorar vários minutos.

## 📁 Estrutura do Projeto

```
dotboot/
├── cmd/dotboot/main.go      # Entrypoint + subcomandos (bot | download | stems |
│                            #   doctor | healthcheck | version | help)
├── internal/
│   ├── config/              # Configuração via variáveis de ambiente
│   ├── downloader/          # yt-dlp + ffmpeg (sem dependência do Discord)
│   ├── stems/               # Separação de stems (audio-separator + ffmpeg)
│   ├── bot/                 # Sessão Discord, /baixar, limites, retries,
│   │                        #   heartbeat e shutdown
│   ├── cli/                 # Modo terminal: download e stems
│   └── doctor/              # Diagnóstico do ambiente
├── scripts/                 # setup, .env, dependências, wrapper Docker, install
├── Makefile                 # make help
├── Dockerfile               # production, development e stems
├── docker-compose.yml       # Bot (default) e live reload (--profile dev)
├── .goreleaser.yaml         # Binários das releases
├── .github/                 # CI, release por tag e Dependabot
└── .env.example             # Template de configuração
```

## ⚙️ Configuração Avançada

### Variáveis de Ambiente

| Variável | Descrição | Default |
|----------|-----------|---------|
| `DISCORD_TOKEN` | Token do bot Discord (só para `dotboot bot`) | — (obrigatório) |
| `DOTBOOT_GUILD_ID` | Regista o comando só neste servidor (propagação instantânea) | — (registo global) |
| `DOTBOOT_MAX_FILE_SIZE_MB` | Teto opcional do upload, em MB (só baixa o limite) | — (limite do servidor) |
| `DOTBOOT_DOWNLOAD_TIMEOUT` | Timeout por download no bot (sintaxe Go) | `10m` |
| `DOTBOOT_MAX_CONCURRENT` | Downloads simultâneos no bot | `3` |
| `DOTBOOT_MAX_PER_USER` | Downloads simultâneos de cada utilizador do bot | `2` |
| `DOTBOOT_ALLOWED_DOMAINS` | Domínios aceites pelo bot, separados por vírgulas | — (qualquer site público) |
| `DOTBOOT_SHUTDOWN_GRACE` | Espera por downloads em curso no shutdown | `30s` |
| `DOTBOOT_OUTPUT_DIR` | Caminho base do CLI: default sem `-o`, raiz de `-o` relativos | diretório atual |
| `DOTBOOT_TEMP_DIR` | Pasta temporária do bot e dos stems | default do sistema |
| `DOTBOOT_COOKIES_FILE` | Cookies (formato Netscape) para sites que pedem login | — |
| `DOTBOOT_JS_RUNTIMES` | Força o runtime JS do yt-dlp (ex: `node`) | deno, ou node |
| `DOTBOOT_MODELS_DIR` | Modelos da separação de stems | `~/.cache/dotboot/models` |
| `DOTBOOT_LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` | `info` |

Os limites de tamanho, timeout e concorrência aplicam-se **apenas ao bot**; o
modo CLI não tem limite de tamanho nem compressão. O `~` é expandido em todos
os caminhos.

### Limite de upload do Discord

O bot deteta o limite em cada pedido, a partir do nível de boost do servidor
onde o `/baixar` foi usado:

| Servidor | Limite |
|----------|--------|
| Sem boost ou Nível 1 | 20 MB |
| Boost Nível 2 | 50 MB |
| Boost Nível 3 | 100 MB |
| Mensagem direta (DM) | 20 MB |

> O limite gratuito passou de 10MB para 20MB a 13 de agosto de 2026.

Vídeos acima do limite são recodificados: o bitrate é calculado a partir da
duração e do orçamento disponível, e a resolução desce (720p, depois 480p,
contadas pelo lado menor) até o ficheiro caber. Quando nem assim é possível —
tipicamente vídeos longos, em que sobrariam menos de 150 kbps de vídeo — a
mensagem de erro diz-te de quanto seria preciso e sugere um recorte com
`inicio`/`fim` ou o modo terminal.

`DOTBOOT_MAX_FILE_SIZE_MB` só **baixa** este limite; nunca o aumenta, porque o
Discord recusaria o upload à mesma.

## 🐛 Troubleshooting

Primeiro passo, sempre: `dotboot doctor` (ou `make doctor`). Diz o que falta,
se o yt-dlp está desatualizado e como resolver cada coisa.

### Bot não conecta

1. Verifique se o token está correto no `.env` (sem o prefixo `Bot `): `make env`
2. Confirme que o bot tem as permissões necessárias no servidor
3. Verifique os logs: `make logs`

### O comando `/baixar` não aparece

Comandos globais podem demorar até 1 hora a propagar depois do primeiro
registo. Para testar de imediato, defina `DOTBOOT_GUILD_ID` com o ID do seu
servidor — o registo passa a ser instantâneo.

### Download falha

- **"Confirma que não és um robô" / login pedido**: o YouTube desconfia de IPs
  de servidores. Exporta os cookies de um browser com sessão iniciada e aponta
  `DOTBOOT_COOKIES_FILE` para eles (ou usa `-cookies` na CLI)
- **"URL não suportado"**: Verifique se o link é válido
- **"Vídeo privado"**: O vídeo não é acessível publicamente
- **"Live sem DVR"**: Streams ao vivo sem rewind não suportam recorte
- **"Timeout"**: Vídeo muito longo; aumente `DOTBOOT_DOWNLOAD_TIMEOUT` ou use o modo CLI
- **Falhas novas em sites que funcionavam**: o yt-dlp está desatualizado.
  Docker: `make update-ytdlp && make up`; nativo: `yt-dlp -U`

### O container aparece como *unhealthy*

O healthcheck (`dotboot healthcheck`) só passa enquanto o gateway do Discord
responde. Se a ligação cair e não voltar em 10 minutos, o bot sai com erro e o
Docker reinicia-o (`restart: unless-stopped`).

### O download está muito lento no fim

Se a mensagem `🔄 ... a converter para H.264` aparecer, a fonte não tinha H.264
e o vídeo está a ser recodificado para poder tocar no telemóvel. Para evitar:
use `-q 1080` (a maior resolução com H.264 no YouTube) ou `-formato original`
se não precisar de MP4.

### Erro de upload (bot)

- **"Demasiado grande"**: Mesmo com compressão, excede o limite do servidor.
  A mensagem indica o bitrate que seria necessário — faça um recorte com
  `inicio`/`fim`, impulsione o servidor, ou use `dotboot download`
- **"Timeout"**: Conexão lenta, tente novamente
- **"Rate limit"**: Muitos uploads, aguarde alguns segundos

## 🔧 Desenvolvimento

```bash
make check                    # gofmt, vet e testes com -race (o mesmo que o CI)
make test                     # testes unitários (rápidos, sem rede)
make test-integration-docker  # yt-dlp e ffmpeg reais, dentro da imagem
make test-integration-stems   # separação real, dentro da imagem de stems
make dev                      # bot com live reload (Air)
make release-snapshot         # binários de todas as plataformas em dist/
```

O yt-dlp da imagem está fixado no `Dockerfile` (`ARG YTDLP_VERSION`) e é
verificado contra o checksum da release. `make update-ytdlp` fixa a última
versão e reconstrói a imagem; o resto das dependências (Go, imagens base,
actions) é atualizado pelo Dependabot.

### Releases

```bash
make release VERSION=v0.1.0
```

Cria e envia a tag; o GitHub Actions publica os binários (GoReleaser) com o
`checksums.txt`.

## 🚀 Deploy

### Docker Hub / Registry

```bash
make docker-build IMAGE=seu-registry/dotboot:latest
docker push seu-registry/dotboot:latest
```

### Docker run direto

```bash
# Bot. O --stop-timeout cobre os 30s de graça do encerramento: com o default
# (10s) o Docker matava os downloads em curso.
docker run -d --name dotboot --init --restart unless-stopped --stop-timeout 45 \
  -e DISCORD_TOKEN=seu_token_aqui \
  seu-registry/dotboot:latest

# CLI com a mesma imagem. O --user faz os ficheiros serem teus (sem ele, o
# utilizador interno do container não tem permissão para escrever na pasta).
docker run --rm --user "$(id -u):$(id -g)" -e HOME=/tmp \
  -v ~/Videos:/videos seu-registry/dotboot:latest \
  download -o /videos "https://youtube.com/playlist?list=yyyy"
```

## 📄 Licença

Este projeto não tem ficheiro de licença: é fornecido como-está, para fins
educacionais e de desenvolvimento.

---

> ⚠️ **Nunca commite o ficheiro `.env`** - ele contém o token secreto do bot!
