# dotboot — Discord Video Bot + Downloader CLI

Bot de Discord escrito em Go que descarrega vídeos de redes sociais (YouTube, X/Twitter, Instagram, Facebook e qualquer site suportado pelo **yt-dlp**) e faz upload diretamente no canal com o comando `/baixar`. Inclui também um **modo terminal** (`dotboot download`) para descarregar vídeos, playlists e canais inteiros para uma pasta local, sem os limites do Discord.

## ✨ Funcionalidades

### Bot Discord (`dotboot bot`)
- 📥 Download de vídeos de qualquer site suportado pelo yt-dlp
- ✂️ Recorte de vídeos com timestamps (`inicio` e `fim`)
- 📏 Limite de upload detetado por servidor (20MB sem boost, 50MB no Nível 2,
  100MB no Nível 3) — sem recomprimir à toa em servidores impulsionados
- 🗜️ Compressão dirigida ao tamanho: o bitrate é calculado a partir da duração
  e do limite, reduzindo a resolução até o vídeo caber
- 🚦 Limite de downloads simultâneos configurável
- 🔄 Retry automático com backoff exponencial
- 🛑 Encerramento gracioso: downloads em curso terminam antes do shutdown

### Modo CLI (`dotboot download`)
- 🎞️ Melhor qualidade disponível por omissão — sem limite de tamanho nem
  compressão; a flag `-q` limita a resolução quando quiseres (ex: `-q 720`)
- 📜 Playlists e canais inteiros num só comando
- 📄 Ficheiro de URLs (`-a urls.txt`) e vários URLs de uma vez
- ⏯️ **Continuar**: re-executar o mesmo comando salta vídeos já descarregados
  (registo em `.dotboot-archive.txt`) e retoma downloads interrompidos (`.part`)
- 📁 Caminho base por variável de ambiente (`DOTBOOT_OUTPUT_DIR`); a flag `-o`
  escolhe o destino — relativo cria uma subpasta dentro da base, absoluto ignora-a

## 🚀 Início Rápido

### Pré-requisitos

- [Docker](https://docs.docker.com/get-docker/) e [Docker Compose](https://docs.docker.com/compose/install/) — ou, para correr localmente: Go 1.24+, `yt-dlp` e `ffmpeg` no PATH
- Token de bot do Discord ([Discord Developer Portal](https://discord.com/developers/applications)) — apenas para o modo bot

### Configuração

1. **Clone o repositório:**
   ```bash
   git clone <url-do-repositorio>
   cd dotboot
   ```

2. **Configure as variáveis de ambiente:**
   ```bash
   cp .env.example .env
   # Edite o .env e adicione o seu DISCORD_TOKEN
   ```

### Produção (bot via Docker)

```bash
# Iniciar o bot
docker compose up -d --build

# Ver logs
docker compose logs -f videobot

# Parar o bot
docker compose down
```

### Desenvolvimento (com live reload)

```bash
# Iniciar com Air para live reload
docker compose --profile dev up --build

# Ver logs do desenvolvimento
docker compose logs -f videobot-dev
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

`dotboot help` (ou `dotboot download -h`) mostra a ajuda completa.

> As flags têm de vir **antes** dos URLs.

```bash
# Um vídeo para o diretório atual
dotboot download https://youtube.com/watch?v=xxxx

# Playlist ou canal inteiro para uma pasta
dotboot download -o ~/Videos https://youtube.com/playlist?list=yyyy

# Ficheiro com um URL por linha
dotboot download -a urls.txt -o ~/Videos

# Limitar a qualidade a 720p de altura (sem -q descarrega a melhor disponível).
# Sites que não expõem a resolução dos formatos continuam a funcionar.
dotboot download -q 720 -o aulas https://youtube.com/watch?v=xxxx

# Recorte de um vídeo (um único URL)
dotboot download -start 01:30 -end 02:00 https://x.com/user/status/123

# Com um caminho base definido, -o relativo cria uma subpasta dentro dele
# (aqui grava em ~/Videos/musica; um -o absoluto ignora a base)
DOTBOOT_OUTPUT_DIR=~/Videos dotboot download -o musica https://youtube.com/watch?v=xxxx

# Sem Go instalado: via Docker, com a pasta de destino montada
docker compose run --rm -v ~/Videos:/videos videobot download -o /videos <url>
```

#### CLI via Docker de qualquer pasta (wrapper)

Sem `yt-dlp`/`ffmpeg` instalados no host, a imagem do bot serve de CLI a partir
de qualquer pasta com um pequeno wrapper no PATH (ex: `~/.local/bin/dotboot`,
com `chmod +x`):

```sh
#!/bin/sh
# Pasta base no host; -o relativo (ex: -o aulas) cria subpastas dentro dela.
BASE="${DOTBOOT_OUTPUT_DIR:-$HOME/Videos}"

TTY=""
[ -t 0 ] && TTY="-t"

# A pasta atual é montada em /cwd para -a urls.txt funcionar.
exec docker run --rm -i $TTY \
  --network host \
  -v "$BASE:/videos" \
  -v "$PWD:/cwd" \
  -w /cwd \
  -e DOTBOOT_OUTPUT_DIR=/videos \
  dotboot-videobot:latest "$@"
```

Constrói a imagem uma vez com `docker compose build videobot` e repete o build
sempre que o código mudar. Com o wrapper usa sempre `-o` **relativo** — um
caminho absoluto referiria o sistema de ficheiros do container, não o do host.

**Continuar downloads**: o modo CLI guarda um registo dos vídeos já
descarregados em `<pasta>/.dotboot-archive.txt`. Re-executar o mesmo comando
(após uma interrupção, ou quando a playlist tem vídeos novos) descarrega apenas
o que falta. Downloads interrompidos a meio (CTRL+C) deixam ficheiros `.part`
que são retomados automaticamente.

Os ficheiros são gravados como `Título do Vídeo [id].ext`. Em sistemas de
ficheiros restritivos, considere o `--restrict-filenames` do yt-dlp (não ativado
por omissão).

## 📁 Estrutura do Projeto

```
dotboot/
├── cmd/dotboot/main.go      # Entrypoint + subcomandos (bot | download | help)
├── internal/
│   ├── config/              # Configuração via variáveis de ambiente
│   ├── downloader/          # yt-dlp + ffmpeg (sem dependência do Discord)
│   │                        #   args.go, downloader.go, compress.go,
│   │                        #   validate.go, errors.go
│   ├── bot/                 # Sessão Discord, handler /baixar, limites,
│   │                        #   retries, shutdown
│   └── cli/                 # Modo terminal (parsing de flags + orquestração)
├── *_test.go                # Testes de tabela, ao lado do código que testam
├── go.mod / go.sum          # Dependências Go
├── Dockerfile               # Build multi-stage (prod + dev)
├── docker-compose.yml       # Orquestração com profiles
├── .dockerignore            # Exclusões do contexto de build
├── .env.example             # Template de configuração
└── README.md                # Esta documentação
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
| `DOTBOOT_SHUTDOWN_GRACE` | Espera por downloads em curso no shutdown | `30s` |
| `DOTBOOT_OUTPUT_DIR` | Caminho base do CLI: default sem `-o`, raiz de `-o` relativos | diretório atual |
| `DOTBOOT_TEMP_DIR` | Pasta temporária do bot | default do sistema |
| `DOTBOOT_LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` | `info` |

Os limites de tamanho, timeout e concorrência aplicam-se **apenas ao bot**; o
modo CLI não tem limite de tamanho nem compressão. Por omissão o CLI também
descarrega na melhor qualidade disponível — a flag `-q` é que limita a
resolução quando quiseres (ex: `-q 720`).

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
duração e do orçamento disponível, e a resolução desce (720p, depois 480p) até
o ficheiro caber. Quando nem assim é possível — tipicamente vídeos longos, em
que sobrariam menos de 150 kbps de vídeo — a mensagem de erro diz-te de quanto
seria preciso e sugere um recorte com `inicio`/`fim` ou o modo terminal.

`DOTBOOT_MAX_FILE_SIZE_MB` só **baixa** este limite; nunca o aumenta, porque o
Discord recusaria o upload à mesma.

## 🐛 Troubleshooting

### Bot não conecta

1. Verifique se o token está correto no `.env` (sem o prefixo `Bot `)
2. Confirme que o bot tem as permissões necessárias no servidor
3. Verifique os logs: `docker compose logs videobot`

### O comando `/baixar` não aparece

Comandos globais podem demorar até 1 hora a propagar depois do primeiro
registo. Para testar de imediato, defina `DOTBOOT_GUILD_ID` com o ID do seu
servidor — o registo passa a ser instantâneo.

### Download falha

- **"URL não suportado"**: Verifique se o link é válido
- **"Vídeo privado"**: O vídeo não é acessível publicamente
- **"Live sem DVR"**: Streams ao vivo sem rewind não suportam recorte
- **"Timeout"**: Vídeo muito longo; aumente `DOTBOOT_DOWNLOAD_TIMEOUT` ou use o modo CLI

### Erro de upload (bot)

- **"Demasiado grande"**: Mesmo com compressão, excede o limite do servidor.
  A mensagem indica o bitrate que seria necessário — faça um recorte com
  `inicio`/`fim`, impulsione o servidor, ou use `dotboot download`
- **"Timeout"**: Conexão lenta, tente novamente
- **"Rate limit"**: Muitos uploads, aguarde alguns segundos

### Performance

```bash
# Ver uso de recursos
docker stats

# Ver logs detalhados
DOTBOOT_LOG_LEVEL=debug docker compose up

# Reiniciar bot se necessário
docker compose restart videobot
```

## 🔧 Desenvolvimento

### Build e testes locais

```bash
go build -o dotboot ./cmd/dotboot
go vet ./...
go test ./...
```

### Live Reload

O modo de desenvolvimento usa [Air](https://github.com/air-verse/air) para recompilar automaticamente:

```bash
docker compose --profile dev up
```

### Estrutura Docker

- **Produção** (`target: production`): Imagem mínima Debian + binário (`ENTRYPOINT /dotboot`, `CMD bot`)
- **Desenvolvimento** (`target: development`): Alpine + Go + Air + código fonte

### Debugging

```bash
# Acesso ao container
docker compose exec videobot sh

# Testar yt-dlp manualmente
docker compose exec videobot yt-dlp --version

# Atualizar o yt-dlp (quebra sempre que o YouTube muda): reconstruir a imagem
docker compose build --build-arg YTDLP_VERSION=latest videobot
docker compose up -d videobot

# Verificar dependências
docker compose exec videobot ffmpeg -version
```

## 🚀 Deploy

### Docker Hub / Registry

```bash
# Build para produção
docker build --target production -t seu-registry/dotboot:latest .

# Push para registry
docker push seu-registry/dotboot:latest
```

### Variáveis de Ambiente em Produção

```bash
# Docker run direto (bot)
docker run -d --name dotboot \
  -e DISCORD_TOKEN=seu_token_aqui \
  seu-registry/dotboot:latest

# Download via terminal com a mesma imagem
docker run --rm -v ~/Videos:/videos seu-registry/dotboot:latest \
  download -o /videos https://youtube.com/playlist?list=yyyy

# Ou via docker-compose com override
echo "DISCORD_TOKEN=seu_token" > .env.production
docker compose -f docker-compose.yml --env-file .env.production up -d
```

## 📄 Licença

Este projeto não tem ficheiro de licença: é fornecido como-está, para fins
educacionais e de desenvolvimento.

---

> ⚠️ **Nunca commite o ficheiro `.env`** - ele contém o token secreto do bot!

> 🐳 **Apenas Docker é necessário** - todas as dependências estão no container!
