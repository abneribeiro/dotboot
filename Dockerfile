# ============================================================
# Fase 1: Build (Construtor)
# Compila o binário Go num ambiente isolado.
# ============================================================
FROM golang:1.24-alpine AS builder

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
# - curl: para descarregar o binário do yt-dlp
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        ffmpeg \
        python3 \
        curl && \
    rm -rf /var/lib/apt/lists/*

# Descarrega o binário mais recente do yt-dlp diretamente do GitHub
# e dá-lhe permissões de execução.
RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp \
        -o /usr/local/bin/yt-dlp && \
    chmod +x /usr/local/bin/yt-dlp

# Copia o binário compilado da fase de build.
COPY --from=builder /dotboot /dotboot

# Define o comando de arranque.
ENTRYPOINT ["/dotboot"]
