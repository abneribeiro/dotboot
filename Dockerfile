# ============================================================
# Multi-stage Dockerfile: produção, desenvolvimento e stems
# ============================================================
#
# As imagens base estão escritas por extenso nos FROM (sem ARG) para o
# Dependabot as conseguir atualizar.

# ============================================================
# Base stage: dependências Go (camada em cache)
# ============================================================
FROM golang:1.27-alpine AS base

# Permite que o Go descarregue automaticamente a versão do toolchain
ENV GOTOOLCHAIN=auto

WORKDIR /src

# Copia os ficheiros de dependências primeiro para aproveitar
# o cache de camadas do Docker (só reinstala se mudarem).
COPY go.mod go.sum ./
RUN go mod download

# ============================================================
# Builder stage: binário estático
# ============================================================
FROM base AS builder

# Versão embutida no binário (dotboot version); o Makefile passa o git describe.
ARG VERSION=dev
ARG COMMIT=
ARG DATE=

# CGO_ENABLED=0 garante um binário sem dependências de C,
# compatível com qualquer imagem Linux.
COPY cmd/ ./cmd/
COPY internal/ ./internal/
RUN CGO_ENABLED=0 go build -trimpath \
        -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
        -o /dotboot ./cmd/dotboot

# ============================================================
# Tools stage: yt-dlp verificado
# ============================================================
FROM debian:trixie-slim AS tools

ARG TARGETARCH

# O yt-dlp parte sempre que o YouTube muda: é a dependência que envelhece mais
# depressa nesta imagem. Está fixado (builds reprodutíveis) e verificado contra
# o SHA2-256SUMS da própria release. `make update-ytdlp` reescreve esta linha
# com a última versão e reconstrói a imagem.
ARG YTDLP_VERSION=2026.10.26

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# O binário standalone é auto-contido (dispensa Python) e já traz o yt-dlp-ejs,
# os scripts que resolvem os desafios JavaScript do YouTube.
RUN set -eux; \
    case "${TARGETARCH:-amd64}" in \
        amd64) asset=yt-dlp_linux ;; \
        arm64) asset=yt-dlp_linux_aarch64 ;; \
        *) echo "arquitetura não suportada: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    release="https://github.com/yt-dlp/yt-dlp/releases/download/${YTDLP_VERSION}"; \
    cd /tmp; \
    curl -fsSLO "${release}/${asset}"; \
    curl -fsSLO "${release}/SHA2-256SUMS"; \
    grep " ${asset}\$" SHA2-256SUMS | sha256sum -c -; \
    install -m 0755 "${asset}" /usr/local/bin/yt-dlp; \
    /usr/local/bin/yt-dlp --version

# ============================================================
# Deno stage: runtime JavaScript do yt-dlp
# ============================================================
# Desde 2025.11 o yt-dlp precisa de um runtime JS para resolver os desafios
# do YouTube; sem ele há menos formatos ou erros. O Deno é o que o yt-dlp usa
# por omissão, sem nenhuma flag.
FROM denoland/deno:bin-2.9.8 AS deno

# ============================================================
# Runtime stage: o que a produção precisa para correr
# ============================================================
FROM debian:trixie-slim AS runtime

# - ca-certificates: certificados TLS para ligações HTTPS
# - ffmpeg: merge, conversão e compressão (traz também o ffprobe)
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg && \
    rm -rf /var/lib/apt/lists/*

COPY --from=tools /usr/local/bin/yt-dlp /usr/local/bin/yt-dlp
COPY --from=deno /deno /usr/local/bin/deno

# Utilizador sem privilégios, COM home: é lá que o yt-dlp guarda a cache (o
# player do YouTube e as soluções dos desafios JS). Sem uma home gravável a
# cache ficava desligada em silêncio e cada download repetia esse trabalho.
RUN useradd --system --uid 10001 --create-home --home-dir /home/dotboot dotboot
WORKDIR /home/dotboot

# ============================================================
# Production stage: imagem final do bot
# ============================================================
FROM runtime AS production

COPY --from=builder /dotboot /dotboot

USER dotboot

# O healthcheck lê a mesma configuração do bot (DOTBOOT_TEMP_DIR incluído), por
# isso o caminho do heartbeat nunca dessincroniza; e o heartbeat só avança
# enquanto o gateway do Discord responde.
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD ["/dotboot", "healthcheck"]

# O CMD é o subcomando por omissão e pode ser substituído,
# ex: docker run dotboot download -o /videos <url>
ENTRYPOINT ["/dotboot"]
CMD ["bot"]

# ============================================================
# Development stage: live reload com Air
# ============================================================
# Debian, como a produção: mesmo ffmpeg, mesmo yt-dlp, mesmo Deno — um bug
# que só aparece com o ffmpeg do Alpine não é um bug que interesse caçar.
FROM golang:1.27-trixie AS development

RUN apt-get update && \
    apt-get install -y --no-install-recommends ffmpeg && \
    rm -rf /var/lib/apt/lists/*

COPY --from=tools /usr/local/bin/yt-dlp /usr/local/bin/yt-dlp
COPY --from=deno /deno /usr/local/bin/deno

# Air fixado por versão (o install.sh do master instalava o que lá estivesse).
RUN go install github.com/air-verse/air@v1.67.4

WORKDIR /app

# Comando padrão: executa Air para live reload com configuração inline
CMD ["air", \
  "--build.cmd", "go build -o ./tmp/main ./cmd/dotboot", \
  "--build.bin", "./tmp/main", \
  "--build.args_bin", "bot", \
  "--build.delay", "1000", \
  "--build.exclude_dir", "tmp", \
  "--build.include_ext", "go", \
  "--misc.clean_on_exit", "true"]
