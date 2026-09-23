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
.PHONY: help build ui embed test test-go test-ui lint fmt vet run desktop desktop-run clean \
        release packages deb rpm macapp windows-installer docker brand tidy check

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

# The native desktop app (Wails) lives in its own module so the server stays a
# pure-Go, CGO-free build. Windows needs no C toolchain; macOS needs Xcode's
# command-line tools; Linux needs libgtk-3-dev and libwebkit2gtk-4.1-dev.
DESKTOP_TAGS := desktop,production
DESKTOP_OUT  := $(BIN)/knott-desktop
ifeq ($(OS),Windows_NT)
  DESKTOP_LDFLAGS := -H windowsgui
  DESKTOP_OUT     := $(BIN)/KNOTT.exe
else
  UNAME_S := $(shell uname -s)
  ifeq ($(UNAME_S),Linux)
    DESKTOP_TAGS := desktop,production,webkit2_41
  endif
endif

desktop: ui embed ## Build the native desktop app into bin/
	@if [ "$(OS)" = "Windows_NT" ]; then cd desktop && go run github.com/tc-hib/go-winres@v0.3.3 make --in winres/winres.json --out rsrc --arch amd64,arm64; fi
	cd desktop && CGO_ENABLED=$(if $(filter Windows_NT,$(OS)),0,1) go build -trimpath -tags $(DESKTOP_TAGS) \
	  -ldflags '$(LDFLAGS) $(DESKTOP_LDFLAGS)' -o ../$(DESKTOP_OUT) .
	@echo "built $(DESKTOP_OUT)"

desktop-run: desktop ## Build and open the desktop app
	./$(DESKTOP_OUT)

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

macapp: desktop build ## Assemble KNOTT.app and a .dmg (macOS)
	bash build/macos/make-app.sh "$(VERSION)" $(DESKTOP_OUT) $(BIN)/knott

windows-installer: ## Build the Windows installer (needs NSIS; run after `make desktop`)
	mkdir -p $(DIST)/win && cp $(BIN)/KNOTT.exe $(DIST)/win/ && GOOS=windows go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/win/knott.exe ./cmd/knott
	cp LICENSE NOTICE $(DIST)/win/
	makensis -DVERSION=$(patsubst v%,%,$(VERSION)) -DSOURCE=$(abspath $(DIST)/win) -DOUTFILE=$(abspath $(DIST))/KNOTT-$(patsubst v%,%,$(VERSION))-windows-x64-setup.exe build/windows/installer.nsi

docker: ## Build the container image
	docker build -f build/docker/Dockerfile -t knott:$(VERSION) -t knott:latest .

clean: ## Remove build output
	rm -rf $(BIN) $(DIST) $(UI_DIST)
	rm -rf $(EMBED_DIST)/assets $(EMBED_DIST)/index.html $(EMBED_DIST)/favicon.svg
