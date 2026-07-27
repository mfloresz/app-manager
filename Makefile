BIN    := ap-manager
VERSION ?= dev
LDFLAGS := -s -w -X main.Version=$(VERSION)

.PHONY: build linux-arm64 linux-armv7 darwin-amd64 darwin-arm64 android android-armv7 all compress run clean

build: ## Compila para linux/amd64
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-linux-amd64-$(VERSION) ./cmd/ap-manager

linux-arm64: ## Compila para linux/arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-linux-arm64-$(VERSION) ./cmd/ap-manager

linux-armv7: ## Compila para linux/arm (v7)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-linux-armv7-$(VERSION) ./cmd/ap-manager

darwin-amd64: ## Compila para darwin/amd64 (Intel)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-darwin-amd64-$(VERSION) ./cmd/ap-manager

darwin-arm64: ## Compila para darwin/arm64 (Apple Silicon)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-darwin-arm64-$(VERSION) ./cmd/ap-manager

android: ## Compila para android/arm64 (Termux)
	CGO_ENABLED=0 GOOS=android GOARCH=arm64 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BIN)-android-arm64-$(VERSION) ./cmd/ap-manager

## android-armv7: Requiere NDK o CGO habilitado con cross-compiler.
## No funciona en CI sin el NDK de Android.
android-armv7:
	CGO_ENABLED=1 \
	CC=armv7a-linux-androideabi21-clang \
	CXX=armv7a-linux-androideabi21-clang++ \
	GOOS=android GOARCH=arm GOARM=7 \
	go build -trimpath -ldflags="$(LDFLAGS)" \
		-o bin/$(BIN)-android-armv7-$(VERSION) ./cmd/ap-manager

all: build linux-arm64 linux-armv7 darwin-amd64 darwin-arm64 android android-armv7 ## Compila para todas las plataformas

compress: ## Comprime los binarios con UPX (máxima compresión)
	@echo "Comprimiendo binario con UPX..."
	@if command -v upx >/dev/null 2>&1; then \
		upx --best --lzma bin/$(BIN)-*; \
		echo "Compresión completada"; \
		ls -lh bin/; \
	else \
		echo "Error: UPX no está instalado. Instálalo con: apt install upx-ucl o brew install upx"; \
		exit 1; \
	fi

run: ## Ejecuta el binario compilado para linux/amd64
	./bin/$(BIN)-linux-amd64-$(VERSION)

clean: ## Limpia los binarios compilados
	rm -f bin/$(BIN)-*

dev: ## Ejecuta en modo desarrollo (go run)
	go run ./cmd/ap-manager

help: ## Muestra esta ayuda
	@echo "Uso: make <target>"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
