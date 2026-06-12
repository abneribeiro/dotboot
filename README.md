# dotboot — Discord Video Bot + Downloader CLI

Bot de Discord escrito em Go que descarrega vídeos de redes sociais (YouTube, X/Twitter, Instagram, Facebook e qualquer site suportado pelo **yt-dlp**) e faz upload diretamente no canal com o comando `/baixar`. Inclui também um **modo terminal** (`dotboot download`) para descarregar vídeos, playlists e canais inteiros para uma pasta local, sem os limites do Discord.

## ✨ Funcionalidades

### Bot Discord (`dotboot bot`)
- 📥 Download de vídeos de qualquer site suportado pelo yt-dlp
- ✂️ Recorte de vídeos com timestamps (`inicio` e `fim`)
- 🗜️ Compressão automática para respeitar o limite de 25MB do Discord
- 🚦 Limite de downloads simultâneos configurável
- 🔄 Retry automático com backoff exponencial
- 🛑 Encerramento gracioso: downloads em curso terminam antes do shutdown

### Modo CLI (`dotboot download`)
- 🎞️ Melhor qualidade disponível — sem limite de 25MB nem compressão
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

> As flags têm de vir **antes** dos URLs.

```bash
# Um vídeo para o diretório atual
dotboot download https://youtube.com/watch?v=xxxx

# Playlist ou canal inteiro para uma pasta
dotboot download -o ~/Videos https://youtube.com/playlist?list=yyyy

# Ficheiro com um URL por linha
dotboot download -a urls.txt -o ~/Videos

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
├── cmd/dotboot/main.go      # Entrypoint + subcomandos (bot | download)
├── internal/
│   ├── config/              # Configuração via variáveis de ambiente
│   ├── downloader/          # yt-dlp + ffmpeg (sem dependência do Discord)
│   ├── bot/                 # Sessão Discord, handler /baixar, shutdown
│   └── cli/                 # Modo terminal
├── go.mod / go.sum          # Dependências Go
├── Dockerfile               # Build multi-stage (prod + dev)
├── docker-compose.yml       # Orquestração com profiles
├── .env.example             # Template de configuração
└── README.md                # Esta documentação
```

## ⚙️ Configuração Avançada

### Variáveis de Ambiente

| Variável | Descrição | Default |
|----------|-----------|---------|
| `DISCORD_TOKEN` | Token do bot Discord (só para `dotboot bot`) | — (obrigatório) |
| `DOTBOOT_MAX_FILE_SIZE_MB` | Limite de upload do Discord em MB | `25` |
| `DOTBOOT_DOWNLOAD_TIMEOUT` | Timeout por download no bot (sintaxe Go) | `10m` |
| `DOTBOOT_MAX_CONCURRENT` | Downloads simultâneos no bot | `3` |
| `DOTBOOT_SHUTDOWN_GRACE` | Espera por downloads em curso no shutdown | `30s` |
| `DOTBOOT_OUTPUT_DIR` | Caminho base do CLI: default sem `-o`, raiz de `-o` relativos | diretório atual |
| `DOTBOOT_TEMP_DIR` | Pasta temporária do bot | default do sistema |
| `DOTBOOT_LOG_LEVEL` | `debug` \| `info` \| `warn` \| `error` | `info` |

Os limites de tamanho, timeout e concorrência aplicam-se **apenas ao bot**; o
modo CLI descarrega sempre na melhor qualidade e sem limites de tamanho.

## 🐛 Troubleshooting

### Bot não conecta

1. Verifique se o token está correto no `.env`
2. Confirme que o bot tem as permissões necessárias no servidor
3. Verifique os logs: `docker compose logs videobot`

### Download falha

- **"URL não suportado"**: Verifique se o link é válido
- **"Vídeo privado"**: O vídeo não é acessível publicamente
- **"Live sem DVR"**: Streams ao vivo sem rewind não suportam recorte
- **"Timeout"**: Vídeo muito longo; aumente `DOTBOOT_DOWNLOAD_TIMEOUT` ou use o modo CLI

### Erro de upload (bot)

- **"Demasiado grande"**: Mesmo com compressão, excede o limite — use `dotboot download`
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

O modo de desenvolvimento usa [Air](https://github.com/cosmtrek/air) para recompilar automaticamente:

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

Este projeto é fornecido como-está para fins educacionais e de desenvolvimento.

---

> ⚠️ **Nunca commite o ficheiro `.env`** - ele contém o token secreto do bot!

> 🐳 **Apenas Docker é necessário** - todas as dependências estão no container!
