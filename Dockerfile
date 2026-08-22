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

# Instala Air para live reload. O repositório mudou de cosmtrek/air para
# air-verse/air; o URL antigo já não é a fonte oficial.
RUN curl -sSfL https://raw.githubusercontent.com/air-verse/air/master/install.sh | sh -s -- -b /go/bin

# Define o diretório de trabalho
WORKDIR /app

# O ENV do Docker não faz substituição de comandos: $(go env GOPATH) entraria
# no PATH como texto literal. GOPATH é /go nas imagens golang oficiais.
ENV PATH="/go/bin:${PATH}"

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
# - ffmpeg: conversão e merge de vídeo/áudio (traz também o ffprobe, usado
#   para medir duração e resolução antes de comprimir)
# - curl: para descarregar o yt-dlp (abaixo)
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        ffmpeg \
        curl && \
    rm -rf /var/lib/apt/lists/*

# Instala o yt-dlp a partir do binário standalone oficial: é auto-contido
# (dispensa Python na imagem) e suporta "yt-dlp -U".
#
# O yt-dlp parte sempre que o YouTube muda, por isso a versão instalada é a
# dependência que envelhece mais depressa nesta imagem. Reconstruir a imagem
# é a forma de a atualizar; como a camada é cacheada, passa-se
# --build-arg YTDLP_VERSION=<tag> (ou um valor diferente) para a invalidar.
ARG YTDLP_VERSION=latest
RUN set -eux; \
    if [ "$YTDLP_VERSION" = "latest" ]; then \
        url="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_linux"; \
    else \
        url="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}/yt-dlp_linux"; \
    fi; \
    curl -fsSL "$url" -o /usr/local/bin/yt-dlp; \
    chmod 0755 /usr/local/bin/yt-dlp; \
    yt-dlp --version

# Copia o binário compilado da fase de build.
COPY --from=builder /dotboot /dotboot

# Utilizador sem privilégios. O bot só escreve em TempDir (/tmp por omissão).
RUN useradd --system --uid 10001 --no-create-home dotboot
USER dotboot

# O bot atualiza /tmp/dotboot.alive a cada 30s enquanto está ligado ao
# Discord. Verificar a idade desse ficheiro diz se o bot está mesmo vivo —
# ao contrário de um "pgrep dotboot", que só confirmava o PID 1 (e que aliás
# nunca funcionou: o debian-slim não traz o pgrep).
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD find /tmp/dotboot.alive -newermt '-90 seconds' 2>/dev/null | grep -q . || exit 1

# Define o comando de arranque. O CMD é o subcomando por omissão e pode
# ser substituído, ex: docker run dotboot download -o /videos <url>
ENTRYPOINT ["/dotboot"]
CMD ["bot"]
