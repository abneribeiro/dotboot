# dotboot — vídeos, música e um estúdio de pistas no browser

O **dotboot** é uma aplicação web local. Descarrega vídeos e música de
qualquer site suportado pelo **yt-dlp** (YouTube, Instagram, X/Twitter,
TikTok, SoundCloud, Facebook…) e **separa músicas em pistas**: vozes, bateria,
baixo, guitarra e piano. As pistas abrem num **estúdio**, onde ouves as
combinações que quiseres (baixo e bateria juntos, por exemplo) e descarregas
só o que escolheres: misturado num único ficheiro de alta qualidade, ou em
ficheiros separados.

Corre no teu computador e abre no browser em `http://localhost:8765`. Nada
vai para a internet, a não ser os downloads que pedes.

## ✨ O que faz

**Estúdio** (depois de separar uma música)
- 🎚️ Uma faixa por pista, cada uma com a sua cor e forma de onda, como num
  editor de música
- 🔇 **M** (mute), **S** (solo) e volume em cada pista; play/pause,
  saltar para qualquer ponto ao clicar na onda, repetir
- ☑️ Marca as pistas e escolhe como as queres:
  - **Juntar num ficheiro**: as marcadas, misturadas num só WAV, FLAC ou
    MP3 320k (ex: `Música (baixo + bateria).wav`)
  - **Em ficheiros separados**: as marcadas, uma por ficheiro (ZIP)
  - **O que estou a ouvir**: a mistura do momento (solos, mutes e volumes)
  - **Todas as pistas**: cada uma no seu ficheiro (ZIP)
- Os volumes dos faders entram na mistura, para o ficheiro soar como no
  estúdio (desliga-se em "Com os volumes do estúdio")
- Teclado: `espaço` toca e pausa, `1`–`6` solo de cada pista,
  `shift+1`–`6` mute, `←`/`→` andam 5 segundos

**Separação**
- A partir de um link ou de um ficheiro do computador (arrastar e largar)
- 4 modelos: 6 pistas (com guitarra e piano), 4 pistas (mais limpo), voz +
  instrumental (karaoke, rápido) e voz + instrumental na qualidade máxima
- Separar só um trecho (`00:30`–`01:30`)

**Vídeo e música**
- 📱 Vídeo sempre em **MP4 com H.264 + AAC**, que o iPhone, o Android e o
  WhatsApp abrem sem conversão (ou o original, se preferires)
- 🎞️ Melhor qualidade, ou uma resolução máxima (720p num vídeo vertical dá
  720x1280)
- 🎵 Música em MP3 ou M4A, com título, artista e capa
- 📜 Playlists e canais inteiros, vários links de uma vez, recortes
- Vês o vídeo ou ouves a música na própria página, antes de descarregar

**Trabalhos e biblioteca**
- Fila com progresso em tempo real: percentagem, velocidade, tempo
  restante, log de cada trabalho, cancelar e repetir
- Os trabalhos correm um de cada vez e continuam com a página fechada
- A biblioteca guarda as separações e os downloads até os apagares

## 🚀 Instalação

```bash
git clone https://github.com/abneribeiro/dotboot
cd dotboot
make setup
```

O `make setup` pergunta como queres instalar:

| | Docker (recomendado) | Nativa |
|---|---|---|
| Comando | `make docker-build && make install-docker` | `make deps && make install` |
| Precisa de | Docker | Go 1.26+ e Node 22+ (o `make deps` instala yt-dlp, deno e ffmpeg) |
| Separação de músicas | incluída | `make deps-stems` (precisa de um compilador C) |
| Espaço | ~3GB (sobretudo o PyTorch) | ~1.5GB com a separação |

As duas instalam o comando `dotboot` em `~/.local/bin` (muda com
`make install PREFIX=/usr/local`). Se o comando não for encontrado,
acrescenta essa pasta ao `PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.zshrc   # ou ~/.bashrc
```

## 🎯 Como usar

```bash
dotboot          # arranca e abre a página; ctrl+c para parar
```

Com a instalação Docker há mais alguns comandos:

```bash
dotboot up       # fica a correr em segundo plano (e volta a arrancar com o Docker)
dotboot down     # para
dotboot open     # abre a página
dotboot logs -f  # mostra o log
dotboot status
```

Na página:

1. **Novo**: escolhe Vídeo, Música ou Separar, cola os links (ou arrasta
   uma música) e carrega em Descarregar ou Separar.
2. **Trabalhos**: acompanha o progresso. No fim, "Abrir no estúdio" ou
   "Descarregar".
3. **Estúdio**: ouve, faz solo e mute, marca as pistas que queres e
   descarrega-as juntas ou em separado.
4. **Biblioteca**: tudo o que já fizeste. **Definições**: cookies, espaço
   ocupado, tema e diagnóstico.

Os ficheiros chegam-te pelo browser, para a pasta de transferências (no WSL,
as Transferências do Windows). No dotboot ficam guardados na pasta de dados
até os apagares na Biblioteca.

### Os modelos de separação

| Modelo | Pistas | Para quê | Tempo no CPU* |
|---|---|---|---|
| `6s` (default) | vozes, bateria, baixo, guitarra, piano, outros | Separar tudo; o piano é a pista mais fraca | ~1× a duração |
| `4s` | vozes, bateria, baixo, outros | Mais limpo; guitarra e piano ficam em "outros" | ~5× |
| `voz` | vozes, instrumental | Karaoke e voz a capella (MDX-Net) | ~1× |
| `voz-hq` | vozes, instrumental | O mesmo na melhor qualidade (BS-RoFormer) | ~20×, só para trechos |

\* Medido num portátil: 30 segundos de música demoraram 35s (`6s`), 2min40s
(`4s`), 40s (`voz`) e 11min (`voz-hq`).

A separação usa o [audio-separator](https://github.com/nomadkaraoke/python-audio-separator)
(modelos HT-Demucs, MDX-Net e BS-RoFormer) e corre no CPU. Cada modelo
descarrega-se na primeira utilização (50 a 650MB). As pistas ficam
guardadas em FLAC (sem perdas), e as exportações partem delas.

### O formato de vídeo: MP4 que o telemóvel lê

O yt-dlp prefere AV1/VP9 + Opus, que dão `.webm`/`.mkv`, ficheiros que o
iPhone não abre. O dotboot garante MP4 com H.264 + AAC em duas camadas:

1. **Pede os codecs certos** ao yt-dlp. No YouTube até 1080p, no Instagram,
   no TikTok e no X isto resolve tudo, **sem recodificar**.
2. **Verifica o que chegou** e corrige. Acima de 1080p o YouTube só tem
   VP9/AV1, e aí o vídeo é recodificado (demora, e o dotboot avisa).

> **Dica**: 1080p é o ponto doce no YouTube: a maior resolução sem
> recodificação. Para o ficheiro tal como vem, escolhe o formato *Original*.

**Repetir** um download que falhou retoma o que ficou a meio (`.part`) e
salta o que já tinha chegado.

## ⚙️ Configuração

Não é preciso configurar nada. Para mudar alguma coisa, na instalação
nativa as variáveis ficam em `~/.config/dotboot/config.env`. Com o wrapper
Docker, passam-se como variáveis de ambiente.

| Variável | O que é | Default |
|---|---|---|
| `DOTBOOT_ADDR` | Onde o servidor escuta | `127.0.0.1:8765` |
| `DOTBOOT_PORT` | Porta no host (só no wrapper Docker) | `8765` |
| `DOTBOOT_DATA_DIR` | Separações, downloads e envios | `~/.local/share/dotboot` (Docker: volume `dotboot-data`) |
| `DOTBOOT_COOKIES_FILE` | Cookies (formato Netscape) para sites que pedem login | também se enviam em Definições |
| `DOTBOOT_MODELS_DIR` | Modelos da separação | `~/.cache/dotboot/models` (Docker: volume `dotboot-models`) |
| `DOTBOOT_TEMP_DIR` | Trabalho intermédio | default do sistema |
| `DOTBOOT_JS_RUNTIMES` | Força o runtime JS do yt-dlp (ex: `node`) | deno, ou node |
| `DOTBOOT_LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` | `info` |

Vê o `.env.example`, que explica cada uma.

**Segurança**: o servidor só aceita ligações deste computador e só responde
a `localhost`. Os pedidos de outros sites abertos no mesmo browser são
recusados. Não há contas nem palavras-passe, por isso não o exponhas à
rede.

## 🐛 Problemas comuns

O primeiro passo é sempre **Definições → Diagnóstico**: diz o que falta, se
o yt-dlp está desatualizado e como resolver.

- **"Confirma que não és um robô" / login pedido**: o YouTube desconfia de
  alguns IPs. Exporta os cookies de um browser com sessão iniciada e envia-os
  em Definições.
- **Um site que funcionava deixou de funcionar**: o yt-dlp está
  desatualizado. Docker: `make update-ytdlp`; nativo: `yt-dlp -U`.
- **A conversão no fim demora muito**: a fonte não tinha H.264 (acima de
  1080p no YouTube). Escolhe 1080p, ou o formato Original.
- **A separação é lenta**: é o CPU. A primeira vez também descarrega o
  modelo.
- **"não foi possível escutar em 127.0.0.1:8765"**: outra aplicação usa a
  porta. Muda-a com `DOTBOOT_PORT=8790 dotboot` (Docker) ou
  `dotboot -addr 127.0.0.1:8790`.
- **`dotboot: command not found`**: falta `~/.local/bin` no `PATH` (ver
  Instalação).

## 🔧 Desenvolvimento

```bash
make run                      # compila a página e arranca o servidor a partir do código
make web-dev                  # Vite com hot reload (:5173) + servidor Go (:8765)
make check                    # Go (gofmt, vet, testes com -race) + página (svelte-check, prettier, vitest)
make test-integration-docker  # yt-dlp, ffmpeg e audio-separator reais, dentro da imagem
make release-snapshot         # binários de todas as plataformas em dist/
make release VERSION=v1.0.0   # cria a tag; o GitHub Actions publica a release
```

```
cmd/dotboot/          Entrada: arranca o servidor e abre o browser
internal/web/         Servidor: API JSON, eventos (SSE), fila, exportações e a página embutida
internal/store/       Pasta de dados: separações, downloads, uploads
internal/jobs/        Motor: valida, executa e relata o progresso dos trabalhos
internal/downloader/  yt-dlp e ffmpeg (download, garantia de MP4)
internal/stems/       audio-separator e ffmpeg (separação, mistura, forma de onda)
internal/doctor/      Diagnóstico
internal/config/      Configuração
web/                  A página: SvelteKit + Svelte 5 + Tailwind CSS 4
scripts/              setup, dependências, wrapper Docker, instalação
```

O yt-dlp da imagem está fixado no `Dockerfile` e é verificado contra o
checksum da release. `make update-ytdlp` muda para a última versão. O resto
(Go, npm, imagens base, actions) é atualizado pelo Dependabot.

## 📄 Licença

Ainda sem licença: fornecido como-está, para fins educacionais e pessoais.
