# Makefile for DragonScale Genkit

# Go parameters
GOCMD=go
GOTEST=$(GOCMD) test
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOLINT=golangci-lint run
PKG=./...

# Binaries
BINARY_NAME=dragonscale

.PHONY: all build test lint fmt clean mod tidy coverage coverhtml tools

all: build

build:
	$(GOBUILD) -o $(BINARY_NAME) .

run:
	$(GOCMD) run .

test:
	$(GOTEST) -v $(PKG)

coverage:
	$(GOTEST) -coverprofile=coverage.out $(PKG)
	@echo "Coverage report generated at coverage.out"

coverhtml: coverage
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "HTML coverage report generated at coverage.html"

lint:
	$(GOLINT) || true

fmt:
	$(GOFMT) $(PKG)

clean:
	$(GOCLEAN)
	@rm -f $(BINARY_NAME) coverage.out coverage.html

mod:
	$(GOMOD) download

modtidy:
	$(GOMOD) tidy

# Install recommended dev tools
TOOLS = golang.org/x/tools/cmd/goimports github.com/golangci/golangci-lint/cmd/golangci-lint

tools:
	@for tool in $(TOOLS); do \
		$(GOCMD) install $$tool@latest; \
	done
