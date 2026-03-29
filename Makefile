BINARY := stratus
INSTALL_DIR := $(shell go env GOPATH)/bin

.PHONY: build install dev clean release

## build: build frontend + Go binary (output: ./stratus)
build:
	cd frontend && npm run build
	go build -o $(BINARY) ./cmd/stratus

## install: build frontend + Go binary and install to GOPATH/bin
install:
	cd frontend && npm run build
	go build -o $(INSTALL_DIR)/$(BINARY) ./cmd/stratus
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"

## dev: build frontend only (for iterating on UI without Go rebuild)
dev:
	cd frontend && npm run dev

## clean: remove build artifacts
clean:
	rm -f $(BINARY)

## release: bump version, build frontend, commit static assets, tag and push
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
	@echo "Released v$(VERSION)"
