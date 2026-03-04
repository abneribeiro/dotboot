# dotboot — Discord Video Bot

Bot de Discord escrito em Go que detecta links de redes sociais (YouTube, Twitter/X, Instagram), descarrega vídeos com **yt-dlp** e faz upload diretamente no canal usando o comando `/baixar`.

## ✨ Funcionalidades

- 📥 Download de vídeos de YouTube, X/Twitter, Instagram
- ✂️ Recorte de vídeos com timestamps (`inicio` e `fim`)
- 🗜️ Compressão automática para respeitar limite de 25MB do Discord
- 🔄 Retry automático com backoff exponencial
- 🐳 Funciona 100% via Docker (zero instalações no sistema)
- 🔧 Configuração unificada para desenvolvimento e produção

## 🚀 Início Rápido

### Pré-requisitos

- [Docker](https://docs.docker.com/get-docker/) e [Docker Compose](https://docs.docker.com/compose/install/)
- Token de bot do Discord ([Discord Developer Portal](https://discord.com/developers/applications))

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

### Produção

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

No Discord, use o comando `/baixar` com os seguintes parâmetros:

- **url** (obrigatório): Link do vídeo
- **inicio** (opcional): Tempo de início no formato `MM:SS` ou `HH:MM:SS`
- **fim** (opcional): Tempo de fim no formato `MM:SS` ou `HH:MM:SS`

### Exemplos:

```
/baixar url:https://youtu.be/dQw4w9WgXcQ
/baixar url:https://youtu.be/dQw4w9WgXcQ inicio:00:30 fim:01:30
/baixar url:https://twitter.com/user/status/123456789
```

## 📁 Estrutura do Projeto

```
dotboot/
├── main.go              # Entrada principal + configuração Discord
├── handler.go           # Processamento de comandos slash
├── downloader.go        # Integração com yt-dlp + compressão
├── go.mod / go.sum      # Dependências Go
├── Dockerfile           # Build multi-stage (prod + dev)
├── docker-compose.yml   # Orquestração com profiles
├── .env.example         # Template de configuração
├── .gitignore           # Exclusões do Git
└── README.md            # Esta documentação
```

## ⚙️ Configuração Avançada

### Variáveis de Ambiente

| Variável | Descrição | Obrigatório |
|----------|-----------|-------------|
| `DISCORD_TOKEN` | Token do bot Discord | ✅ |

### Limites e Timeouts

- **Tamanho máximo**: 25MB (limite do Discord)
- **Timeout de download**: 10 minutos
- **Compressão automática**: Para vídeos >20MB
- **Retry automático**: Até 5 tentativas com backoff exponencial

## 🐛 Troubleshooting

### Bot não conecta

1. Verifique se o token está correto no `.env`
2. Confirme que o bot tem as permissões necessárias no servidor
3. Verifique os logs: `docker compose logs videobot`

### Download falha

- **"URL não suportado"**: Verifique se o link é válido
- **"Vídeo privado"**: O vídeo não é acessível publicamente
- **"Live sem DVR"**: Streams ao vivo sem rewind não suportam recorte
- **"Timeout"**: Vídeo muito longo, tente com URL mais curto

### Erro de upload

- **"Demasiado grande"**: Mesmo com compressão, excede 25MB
- **"Timeout"**: Conexão lenta, tente novamente
- **"Rate limit"**: Muitos uploads, aguarde alguns segundos

### Performance

```bash
# Ver uso de recursos
docker stats

# Ver logs detalhados
docker compose logs -f videobot

# Reiniciar bot se necessário
docker compose restart videobot
```

## 🔧 Desenvolvimento

### Live Reload

O modo de desenvolvimento usa [Air](https://github.com/cosmtrek/air) para recompilar automaticamente:

```bash
# Desenvolvimento com hot reload
docker compose --profile dev up

# Rebuilds automáticos quando código muda
```

### Estrutura Docker

- **Produção** (`target: production`): Imagem mínima Debian + binário
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
# Docker run direto
docker run -d --name dotboot \
  -e DISCORD_TOKEN=seu_token_aqui \
  seu-registry/dotboot:latest

# Ou via docker-compose com override
echo "DISCORD_TOKEN=seu_token" > .env.production
docker compose -f docker-compose.yml --env-file .env.production up -d
```

## 📄 Licença

Este projeto é fornecido como-está para fins educacionais e de desenvolvimento.

---

> ⚠️ **Nunca commite o ficheiro `.env`** - ele contém o token secreto do bot!

> 🐳 **Apenas Docker é necessário** - todas as dependências estão no container!
