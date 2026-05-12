BINARY := continuum-plugin-whmcs-login
GO ?= go

.PHONY: build test test-go test-web clean web-build web-install

build: web-build
	$(GO) build -o $(BINARY) ./cmd/continuum-plugin-whmcs-login

web-install:
	cd web && pnpm install --frozen-lockfile

web-build:
	cd web && pnpm install --frozen-lockfile && pnpm run build

test: test-go

test-go:
	$(GO) test ./...

test-web:
	cd web && pnpm run test --run

clean:
	rm -f $(BINARY)
	rm -rf web/dist
