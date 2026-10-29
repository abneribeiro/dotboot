# dotboot — instalar, configurar, testar e correr.
#
#   make              mostra os targets
#   make setup        configura tudo depois do clone (assistente)
#
# Variáveis úteis: PREFIX/BINDIR (onde instalar a CLI), IMAGE, VERSION (por
# omissão, o git describe).

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

BINARY          := dotboot
PKG             := ./cmd/dotboot
BIN_DIR         := bin
PREFIX          ?= $(HOME)/.local
BINDIR          ?= $(PREFIX)/bin
DOCKER_CLI_NAME ?= dotboot

IMAGE ?= dotboot:latest

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

GORELEASER_IMAGE ?= goreleaser/goreleaser:v2.19.0

# DOCKER_BUILD_FLAGS acrescenta opções ao docker build, ex: --network host
# numa máquina onde o DNS falhe dentro do build.
DOCKER_BUILD_FLAGS ?=
DOCKER_BUILD := docker build $(DOCKER_BUILD_FLAGS) \
	--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg DATE=$(DATE)

# Avisa quando o diretório de instalação não está no PATH.
define path_hint
case ":$$PATH:" in *":$(BINDIR):"*) ;; \
  *) echo "⚠️  $(BINDIR) não está no PATH. Acrescenta ao ~/.zshrc ou ~/.bashrc:"; \
     echo "     export PATH=\"$(BINDIR):\$$PATH\"" ;; esac
endef

.PHONY: help setup config doctor run deps deps-stems build install install-docker uninstall \
	fmt vet lint test test-race cover test-integration test-integration-docker check \
	docker-build update-ytdlp release-snapshot release clean

##@ Começar

help: ## Mostra esta ajuda
	@awk 'BEGIN {FS = ":.*## "; printf "Uso: make \033[36m<target>\033[0m [VAR=valor]\n"} \
		/^##@/ {printf "\n\033[1m%s\033[0m\n", substr($$0, 5)} \
		/^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-24s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: ## Assistente pós-clone: instala a CLI (nativa ou Docker) e configura-a
	@BINDIR="$(BINDIR)" IMAGE="$(IMAGE)" scripts/setup.sh

config: ## Cria ou edita ~/.config/dotboot/config.env (pasta de saída, cookies)
	@scripts/configure-env.sh

run: ## Abre a TUI a partir do código (go run)
	go run $(PKG)

doctor: build ## Verifica dependências e configuração
	@$(BIN_DIR)/$(BINARY) doctor

##@ CLI no host

deps: ## Instala yt-dlp e deno em BINDIR, e o ffmpeg pelo gestor de pacotes
	@BINDIR="$(BINDIR)" scripts/install-deps.sh

deps-stems: ## Instala o audio-separator (separação de stems) com o uv
	@BINDIR="$(BINDIR)" scripts/install-deps.sh --stems

build: ## Compila bin/dotboot com a versão embutida
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)

install: build ## Instala a CLI nativa em BINDIR (default ~/.local/bin)
	@mkdir -p "$(BINDIR)"
	@if [ -f "$(BINDIR)/$(BINARY)" ] && [ "$$(head -c 2 "$(BINDIR)/$(BINARY)")" = "#!" ]; then \
		echo "ℹ️  $(BINDIR)/$(BINARY) era o wrapper Docker e passa a ser a CLI nativa."; fi
	install -m 0755 $(BIN_DIR)/$(BINARY) "$(BINDIR)/$(BINARY)"
	@echo "✅ $$("$(BINDIR)/$(BINARY)" version)"
	@$(path_hint)

install-docker: ## Instala o wrapper que corre tudo via Docker (DOCKER_CLI_NAME=dotboot)
	@mkdir -p "$(BINDIR)"
	install -m 0755 scripts/dotboot-docker "$(BINDIR)/$(DOCKER_CLI_NAME)"
	@echo "✅ $(BINDIR)/$(DOCKER_CLI_NAME) → imagem $(IMAGE) (constrói-a com: make docker-build)"
	@$(path_hint)

uninstall: ## Remove a CLI de BINDIR
	rm -f "$(BINDIR)/$(BINARY)"

##@ Qualidade

fmt: ## Formata o código (gofmt)
	gofmt -w cmd internal

vet: ## go vet
	go vet ./...

lint: ## Falha se houver código por formatar ou avisos do vet
	@unformatted="$$(gofmt -l cmd internal)"; \
	if [ -n "$$unformatted" ]; then echo "Por formatar (make fmt):"; echo "$$unformatted"; exit 1; fi
	go vet ./...
	go vet -tags integration ./...

test: ## Testes unitários (rápidos, sem rede)
	go test ./...

test-race: ## Testes com o detetor de corridas
	go test -race ./...

cover: ## Cobertura (coverage.out + total)
	go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

test-integration: ## Testes de integração no host (precisa de yt-dlp, ffmpeg, audio-separator e rede)
	go test -tags integration -v -timeout 30m ./internal/downloader ./internal/stems

test-integration-docker: docker-build ## Testes de integração dentro da imagem (downloads reais e separação)
	CGO_ENABLED=0 go test -c -tags integration -o $(BIN_DIR)/downloader.test ./internal/downloader
	CGO_ENABLED=0 go test -c -tags integration -o $(BIN_DIR)/stems.test ./internal/stems
	docker run --rm --entrypoint /tests/downloader.test \
		-v "$(CURDIR)/$(BIN_DIR):/tests:ro" $(IMAGE) \
		-test.v -test.timeout 20m $(if $(RUN),-test.run '$(RUN)')
	docker run --rm --entrypoint /tests/stems.test \
		-v "$(CURDIR)/$(BIN_DIR):/tests:ro" -v dotboot-models:/models $(IMAGE) \
		-test.v -test.timeout 30m

check: lint test-race ## lint + testes com -race (o mesmo que o CI)

##@ Docker

docker-build: ## Constrói a imagem com tudo (IMAGE=dotboot:latest, ~3GB)
	$(DOCKER_BUILD) --target app -t $(IMAGE) .

update-ytdlp: ## Fixa a última versão do yt-dlp no Dockerfile e reconstrói a imagem
	@tag="$$(curl -fsSI https://github.com/yt-dlp/yt-dlp/releases/latest | tr -d '\r' \
		| grep -i '^location:' | sed 's#.*/tag/##')"; \
	if [ -z "$$tag" ]; then echo "não foi possível descobrir a última versão do yt-dlp" >&2; exit 1; fi; \
	sed -i.bak "s/^ARG YTDLP_VERSION=.*/ARG YTDLP_VERSION=$$tag/" Dockerfile && rm -f Dockerfile.bak; \
	echo "yt-dlp fixado em $$tag no Dockerfile"
	$(MAKE) docker-build

##@ Releases

release-snapshot: ## Binários de todas as plataformas em dist/, sem publicar
	docker run --rm --user "$$(id -u):$$(id -g)" \
		-e HOME=/tmp -e GOCACHE=/tmp/go-build -e GOMODCACHE=/gomod \
		-v "$$(go env GOMODCACHE):/gomod" -v "$(CURDIR):/src" -w /src \
		$(GORELEASER_IMAGE) release --snapshot --clean

release: ## Cria e envia a tag VERSION=vX.Y.Z (o GitHub Actions publica a release)
	@if ! [[ "$(VERSION)" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$$ ]]; then \
		echo "usa: make release VERSION=v0.1.0" >&2; exit 1; fi
	@if [ -n "$$(git status --porcelain)" ]; then echo "há alterações por commitar" >&2; exit 1; fi
	git tag -a $(VERSION) -m "dotboot $(VERSION)"
	git push origin $(VERSION)

clean: ## Remove bin/, dist/ e a cobertura
	rm -rf $(BIN_DIR) dist coverage.out
