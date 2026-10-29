# ============================================================
# Multi-stage Dockerfile: uma só imagem com tudo (dotboot:latest)
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
# Stems deps stage: o ambiente Python da separação de stems
# ============================================================
# Python 3.12, a versão que o audio-separator e o PyTorch suportam melhor.
# O diffq (dependência do Demucs) só existe em código fonte, por isso este
# estágio tem compilador; a imagem final recebe só o venv, sem o gcc.
FROM python:3.12-slim-trixie AS stems-deps

ARG AUDIO_SEPARATOR_VERSION=0.47.1
# O audio-separator 0.47 importa o audioread sem o declarar como dependência
# (o librosa deixou de o trazer): sem ele, falha logo ao arrancar.
ARG AUDIOREAD_VERSION=3.1.0

RUN apt-get update && \
    apt-get install -y --no-install-recommends build-essential && \
    rm -rf /var/lib/apt/lists/*

RUN python -m venv /opt/stems
ENV PATH=/opt/stems/bin:$PATH

# O PyTorch vem do índice CPU: o do PyPI traz ~3GB de bibliotecas CUDA que,
# sem uma GPU NVIDIA, não servem para nada. O torchvision vem do mesmo índice
# e na mesma instalação: o do PyPI é compilado contra outro torch, e os
# modelos MDX-Net falhavam com "operator torchvision::nms does not exist".
RUN pip install --no-cache-dir --index-url https://download.pytorch.org/whl/cpu torch torchvision && \
    pip install --no-cache-dir "audio-separator[cpu]==${AUDIO_SEPARATOR_VERSION}" \
        "audioread==${AUDIOREAD_VERSION}"

# ============================================================
# App stage: a imagem final (TUI, download e stems)
# ============================================================
# ~3GB, sobretudo o PyTorch: é o preço da separação de stems, e uma só
# imagem deixa a TUI fazer tudo pelo wrapper sem instalar nada no host.
FROM python:3.12-slim-trixie AS app

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates ffmpeg && \
    rm -rf /var/lib/apt/lists/*

COPY --from=stems-deps /opt/stems /opt/stems
ENV PATH=/opt/stems/bin:$PATH
RUN audio-separator --version

COPY --from=tools /usr/local/bin/yt-dlp /usr/local/bin/yt-dlp
COPY --from=deno /deno /usr/local/bin/deno
COPY --from=builder /dotboot /dotboot

# /models com 1777: o wrapper corre o container com o uid de quem o chama, e
# um volume novo herda as permissões deste diretório.
RUN useradd --system --uid 10001 --create-home --home-dir /home/dotboot dotboot && \
    install -d -m 1777 /models
ENV DOTBOOT_MODELS_DIR=/models
VOLUME /models

USER dotboot
WORKDIR /home/dotboot

# Sem argumentos abre a TUI (com -it); ex: docker run --rm -it dotboot:latest
ENTRYPOINT ["/dotboot"]
