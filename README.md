# dotboot — Discord Video Bot

Bot de Discord escrito em Go que deteta links de redes sociais (YouTube, Twitter/X, Instagram), descarrega o vídeo com **yt-dlp** e faz upload diretamente no canal.

## Pré-requisitos

- [Docker](https://docs.docker.com/get-docker/) e [Docker Compose](https://docs.docker.com/compose/install/) instalados.
- Um token de bot do Discord (obtido no [Discord Developer Portal](https://discord.com/developers/applications)).

## Configuração

1. **Clona o repositório:**

   ```bash
   git clone <url-do-repositorio>
   cd dotboot
   ```

2. **Cria o ficheiro `.env`** na raiz do projeto com o teu token:

   ```
   DISCORD_TOKEN=o_teu_token_aqui
   ```

   > ⚠️ Nunca faças commit deste ficheiro — ele já está no `.gitignore`.

## Como Rodar

### Iniciar o bot

```bash
docker compose up -d --build
```

- `--build` — reconstrói a imagem (necessário após alterações no código).
- `-d` — corre em segundo plano (detached).

### Ver os logs em tempo real

```bash
docker compose logs -f videobot
```

Deves ver: `Bot está a correr. Pressiona CTRL+C para terminar.`

### Parar o bot

```bash
docker compose down
```

### Reiniciar após alterações no código

```bash
docker compose up -d --build
```

## Estrutura do Projeto

```
dotboot/
├── main.go              # Entrada principal, ligação ao Discord
├── handler.go           # Deteta URLs e processa mensagens
├── downloader.go        # Invoca o yt-dlp para descarregar vídeos
├── go.mod / go.sum      # Dependências Go
├── Dockerfile           # Build multi-stage (Go + runtime)
├── docker-compose.yml   # Orquestração do contentor
├── .env                 # Token do Discord (não versionado)
└── .dockerignore        # Ficheiros excluídos do build Docker
```

## Notas

- O bot reinicia automaticamente se for abaixo (política `restart: unless-stopped`).
- Os vídeos são guardados temporariamente em `/tmp` dentro do contentor e eliminados após o upload.
- O limite de upload do Discord sem Nitro é **25 MB** — o bot respeita este limite.
- Todas as dependências (yt-dlp, ffmpeg, python3) estão dentro do contentor — **não precisas de instalar nada no teu sistema** além do Docker.
