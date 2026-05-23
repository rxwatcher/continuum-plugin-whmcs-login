BINARY := silo-plugin-whmcs-login
GO ?= go
PNPM ?= pnpm

.PHONY: build test test-go test-web clean web-build web-install

build: web-build
	$(GO) build -o $(BINARY) ./cmd/silo-plugin-whmcs-login

web-install:
	cd web && $(PNPM) install --frozen-lockfile

web-build:
	cd web && $(PNPM) install --frozen-lockfile && $(PNPM) run build

test: test-go test-web

test-go:
	$(GO) test ./...

test-web:
	cd web && $(PNPM) run test --run

clean:
	rm -f $(BINARY)
	rm -rf web/dist
