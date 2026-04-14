BINARY := stratus
INSTALL_DIR := $(shell go env GOPATH)/bin

.PHONY: build install dev dev-frontend dev-backend clean release

## build: build frontend + Go binary (output: ./stratus)
build:
	cd frontend && npm run build
	go build -o $(BINARY) ./cmd/stratus

## install: build frontend + Go binary and install to GOPATH/bin
install:
	cd frontend && npm run build
	go build -o $(INSTALL_DIR)/$(BINARY) ./cmd/stratus
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"

## dev: start Go (air) + Vite dev server with hot reload; open http://localhost:5173
dev:
	@command -v air >/dev/null 2>&1 || { echo "ERROR: 'air' not installed. Run: go install github.com/air-verse/air@latest"; exit 1; }
	@echo "==> Starting Stratus dev loop (backend :41777, frontend :5173)"
	@echo "==> Open http://localhost:5173 — edits to .go or .svelte files hot reload automatically"
	@STRATUS_DEV=1 STRATUS_PORT=41777 bash -c 'trap "kill 0" INT TERM EXIT; air & (cd frontend && npm run dev) & wait'

## dev-backend: run only Go backend with air hot reload (no Vite)
dev-backend:
	@command -v air >/dev/null 2>&1 || { echo "ERROR: 'air' not installed. Run: go install github.com/air-verse/air@latest"; exit 1; }
	STRATUS_DEV=1 STRATUS_PORT=41777 air

## dev-frontend: run only Vite dev server (frontend iteration without backend)
dev-frontend:
	cd frontend && npm run dev

## clean: remove build artifacts
clean:
	rm -f $(BINARY)

## release: bump version, build frontend, commit static assets, tag, push, and create GitHub Release
##   usage: make release VERSION=x.y.z
release:
	@test -n "$(VERSION)" || (echo "ERROR: VERSION is required. Usage: make release VERSION=x.y.z"; exit 1)
	@echo "$(VERSION)" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$$' || (echo "ERROR: VERSION must be in x.y.z format"; exit 1)
	@echo "==> Bumping version to $(VERSION)"
	sed -i 's/const Version = ".*"/const Version = "$(VERSION)"/' cmd/stratus/version.go
	@echo "==> Building frontend"
	cd frontend && npm run build
	@echo "==> Staging static assets and version"
	git add cmd/stratus/static/ cmd/stratus/version.go
	@git diff --cached --stat
	@echo "==> Committing"
	git commit -m "release: v$(VERSION)"
	@echo "==> Tagging v$(VERSION)"
	git tag v$(VERSION)
	@echo "==> Pushing"
	git push && git push origin v$(VERSION)
	@echo "==> Creating GitHub Release"
	gh release create v$(VERSION) --title "v$(VERSION)" --generate-notes
	@echo "Released v$(VERSION)"
