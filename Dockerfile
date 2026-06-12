# ============================================================
# Multi-stage Dockerfile for both Production and Development
# ============================================================

# ============================================================
# Base stage: Common dependencies
# ============================================================
FROM golang:1.24-alpine AS base

# Permite que o Go descarregue automaticamente a versão do toolchain
ENV GOTOOLCHAIN=auto

WORKDIR /src

# Copia os ficheiros de dependências primeiro para aproveitar
# o cache de camadas do Docker (só reinstala se mudarem).
COPY go.mod go.sum ./
RUN go mod download

# ============================================================
# Builder stage: Compile the binary
# ============================================================
FROM base AS builder

# Copia o código-fonte e compila o binário estático.
# CGO_ENABLED=0 garante um binário sem dependências de C,
# compatível com qualquer imagem Linux.
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -o /dotboot ./cmd/dotboot

# ============================================================
# Development stage: Live reload with Air
# ============================================================
FROM base AS development

# Instala dependências necessárias para desenvolvimento:
# - git: para Air e dependências Go
# - ca-certificates: certificados TLS
# - ffmpeg: conversão e merge de vídeo/áudio
# - python3: necessário para yt-dlp
# - py3-pip: para instalar yt-dlp
# - curl: para instalar Air
RUN apk add --no-cache \
    git \
    ca-certificates \
    ffmpeg \
    python3 \
    py3-pip \
    curl

# Instala a versão mais recente do yt-dlp
RUN pip3 install --no-cache-dir --break-system-packages -U yt-dlp

# Instala Air para live reload
RUN curl -sSfL https://raw.githubusercontent.com/cosmtrek/air/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

# Define o diretório de trabalho
WORKDIR /app

# Adiciona o diretório bin do Go ao PATH para encontrar o Air
ENV PATH="$PATH:$(go env GOPATH)/bin"

# Comando padrão: executa Air para live reload com configuração inline
CMD ["air", \
  "--build.cmd", "go build -o ./tmp/main ./cmd/dotboot", \
  "--build.bin", "./tmp/main", \
  "--build.args_bin", "bot", \
  "--build.delay", "1000", \
  "--build.exclude_dir", "tmp", \
  "--build.include_ext", "go", \
  "--misc.clean_on_exit", "true"]

# ============================================================
# Production stage: Minimal runtime image
# ============================================================
FROM debian:bookworm-slim AS production

# Instala as dependências de sistema:
# - ca-certificates: certificados TLS para ligações HTTPS
# - ffmpeg: conversão e merge de vídeo/áudio
# - python3: necessário para alguns extractors do yt-dlp
# - python3-pip: para instalar yt-dlp via pip
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        ffmpeg \
        python3 \
        python3-pip && \
    rm -rf /var/lib/apt/lists/*

# Instala a versão mais recente do yt-dlp via pip
# --no-cache-dir: evita cache do pip para sempre baixar a versão mais recente
# -U: força atualização para a versão mais nova disponível
# --break-system-packages: necessário para Debian Bookworm (PEP 668)
RUN pip3 install --no-cache-dir --break-system-packages -U yt-dlp

# Copia o binário compilado da fase de build.
COPY --from=builder /dotboot /dotboot

# Adiciona um health check para monitorizar o estado do bot
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD pgrep -f dotboot > /dev/null || exit 1

# Define o comando de arranque. O CMD é o subcomando por omissão e pode
# ser substituído, ex: docker run dotboot download -o /videos <url>
ENTRYPOINT ["/dotboot"]
CMD ["bot"]
