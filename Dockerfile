# ============================================================
# Fase 1: Build (Construtor)
# Compila o binário Go num ambiente isolado.
# ============================================================
FROM golang:1.25-alpine AS builder

# Permite que o Go descarregue automaticamente a versão do toolchain
# especificada no go.mod, caso seja diferente da versão da imagem base.
ENV GOTOOLCHAIN=auto

WORKDIR /src

# Copia os ficheiros de dependências primeiro para aproveitar
# o cache de camadas do Docker (só reinstala se mudarem).
COPY go.mod go.sum ./

RUN go mod download

# Copia o código-fonte e compila o binário estático.
# CGO_ENABLED=0 garante um binário sem dependências de C,
# compatível com qualquer imagem Linux.
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /dotboot .

# ============================================================
# Fase 2: Runtime (Execução)
# Imagem leve com apenas as dependências necessárias.
# ============================================================
FROM debian:bookworm-slim

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

# Define o comando de arranque.
ENTRYPOINT ["/dotboot"]
