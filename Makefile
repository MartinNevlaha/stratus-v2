BINARY := stratus
INSTALL_DIR := $(shell go env GOPATH)/bin

.PHONY: build install dev clean

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
