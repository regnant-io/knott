# KNOTT — build, test and release.
#
# The only hard requirements are Go and Node. Everything else (Docker, the
# packaging tools) is needed only for the target that uses it.

SHELL      := /bin/sh
MODULE     := github.com/regnant/knott
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE       ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
              -X main.version=$(VERSION) \
              -X main.commit=$(COMMIT) \
              -X main.date=$(DATE)

UI_SRC     := apps/designer
UI_DIST    := $(UI_SRC)/dist
EMBED_DIST := internal/ui/dist
BIN        := bin
DIST       := dist

# Every platform a KNOTT release is published for.
PLATFORMS  := windows/amd64 windows/arm64 \
              darwin/amd64 darwin/arm64 \
              linux/amd64 linux/arm64

.DEFAULT_GOAL := build
.PHONY: help build ui embed test test-go test-ui lint fmt vet run desktop clean \
        release packages deb rpm appimage macapp docker brand tidy check

help: ## Show the available targets
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

# ─── Build ────────────────────────────────────────────────────────────────────

ui: ## Build the web console
	npm --prefix $(UI_SRC) ci --no-audit --no-fund
	npm --prefix $(UI_SRC) run build

embed: ## Stage the built console for go:embed
	@test -f $(UI_DIST)/index.html || { echo "run 'make ui' first"; exit 1; }
	rm -rf $(EMBED_DIST)/assets $(EMBED_DIST)/index.html $(EMBED_DIST)/favicon.svg
	cp -R $(UI_DIST)/. $(EMBED_DIST)/

build: ## Build knott for this machine (console included if built)
	@test -f $(UI_DIST)/index.html && $(MAKE) embed || \
	  echo "note: no console build found — binary will serve API only"
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/knott ./cmd/knott
	@echo "built $(BIN)/knott $(VERSION)"

services: ## Build the per-service binaries for a distributed deployment
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN)/ ./cmd/...

run: build ## Build and run KNOTT
	$(BIN)/knott serve --open

desktop: build ## Build and run KNOTT in a desktop window
	$(BIN)/knott desktop

brand: ## Regenerate every brand asset from the mark geometry
	python tools/brand/generate.py

# ─── Quality ──────────────────────────────────────────────────────────────────

check: fmt vet test ## Everything CI runs

test: test-go test-ui ## Run all tests

test-go: ## Run the Go tests
	go test ./...

test-ui: ## Run the console tests
	npm --prefix $(UI_SRC) test

vet: ## Run go vet
	go vet ./...

fmt: ## Format the Go sources
	gofmt -w $(shell git ls-files '*.go')

tidy: ## Tidy go.mod
	go mod tidy

# ─── Release ──────────────────────────────────────────────────────────────────

release: ui embed ## Cross-compile release archives for every platform
	@rm -rf $(DIST) && mkdir -p $(DIST)
	@for platform in $(PLATFORMS); do \
	  os=$${platform%/*}; arch=$${platform#*/}; \
	  ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	  out="$(DIST)/knott_$(VERSION)_$${os}_$${arch}"; \
	  mkdir -p "$$out"; \
	  echo "  building $$os/$$arch"; \
	  GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 \
	    go build -trimpath -ldflags '$(LDFLAGS)' -o "$$out/knott$$ext" ./cmd/knott || exit 1; \
	  cp LICENSE NOTICE README.md "$$out/"; \
	  mkdir -p "$$out/ai-decision-engine"; \
	  cp services/ai-decision-engine/main.py "$$out/ai-decision-engine/"; \
	  if [ "$$os" = "windows" ] && command -v zip >/dev/null 2>&1; then \
	    (cd $(DIST) && zip -qr "$$(basename $$out).zip" "$$(basename $$out)") || exit 1; \
	  else \
	    tar -C $(DIST) -czf "$$out.tar.gz" "$$(basename $$out)" || exit 1; \
	  fi; \
	  rm -rf "$$out"; \
	done
	@cd $(DIST) && sha256sum * > SHA256SUMS 2>/dev/null || shasum -a 256 * > SHA256SUMS
	@ls -1 $(DIST)

packages: deb rpm ## Build the Linux packages (needs nfpm)

deb: ## Build a .deb (needs nfpm)
	VERSION=$(VERSION) nfpm package -f build/nfpm.yaml -p deb -t $(DIST)/

rpm: ## Build an .rpm (needs nfpm)
	VERSION=$(VERSION) nfpm package -f build/nfpm.yaml -p rpm -t $(DIST)/

macapp: ## Assemble KNOTT.app for macOS
	bash build/macos/make-app.sh "$(VERSION)"

appimage: ## Assemble a Linux AppImage (needs appimagetool)
	bash build/linux/make-appimage.sh "$(VERSION)"

docker: ## Build the container image
	docker build -f build/docker/Dockerfile -t knott:$(VERSION) -t knott:latest .

clean: ## Remove build output
	rm -rf $(BIN) $(DIST) $(UI_DIST)
	rm -rf $(EMBED_DIST)/assets $(EMBED_DIST)/index.html $(EMBED_DIST)/favicon.svg
