APP      := keetui
MODULE   := github.com/rmarchei/keetui
CMD      := ./cmd/keetui
DIST     := dist

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOIMAGE  := golang:1.27.4
LINTIMAGE := golangci/golangci-lint:latest
LDFLAGS  := -s -w -X main.version=$(VERSION)

PLATFORMS := darwin-amd64 darwin-arm64 linux-amd64 linux-arm64

# Use the installed Go toolchain by default. Opt into a running container
# runtime explicitly, for example: make CONTAINER_RUNTIME=podman
CONTAINER_RUNTIME ?=

HOST_OS   := $(shell uname -s | tr '[:upper:]' '[:lower:]')
HOST_ARCH := $(shell uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')

GO_SOURCES := $(shell find cmd internal -name '*.go') go.mod go.sum Makefile

PREFIX   ?= /usr/local

.PHONY: build test lint check all clean install $(PLATFORMS)

build: $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH)

test:
	@if [ -n "$(CONTAINER_RUNTIME)" ]; then \
		$(CONTAINER_RUNTIME) run --rm \
			-v "$(CURDIR):/src" \
			-w /src \
			$(GOIMAGE) \
			go test -race ./...; \
	else \
		CGO_ENABLED=1 go test -race ./...; \
	fi

lint:
	@if [ -n "$(CONTAINER_RUNTIME)" ]; then \
		$(CONTAINER_RUNTIME) run --rm \
			-v "$(CURDIR):/src" \
			-w /src \
			$(GOIMAGE) \
			sh -c 'test -z "$$(gofmt -l .)" || (echo "Files not formatted:"; gofmt -l .; exit 1)'; \
		$(CONTAINER_RUNTIME) run --rm \
			-v "$(CURDIR):/src" \
			-w /src \
			$(LINTIMAGE) \
			golangci-lint run ./...; \
	else \
		test -z "$$(gofmt -l .)" || (echo "Files not formatted:"; gofmt -l .; exit 1); \
		golangci-lint run ./...; \
	fi

check: lint test

all: $(PLATFORMS)

# Share one recipe between native builds and cross-compilation.
define BUILD_PLATFORM
$(1): $(DIST)/$(APP)-$(1)
$(DIST)/$(APP)-$(1): $(GO_SOURCES)
	@mkdir -p $(DIST)
	@if [ -n "$(CONTAINER_RUNTIME)" ]; then \
		$(CONTAINER_RUNTIME) run --rm \
			-v "$(CURDIR):/src" \
			-w /src \
			-e CGO_ENABLED=0 \
			-e GOOS=$(word 1,$(subst -, ,$(1))) \
			-e GOARCH=$(word 2,$(subst -, ,$(1))) \
			$(GOIMAGE) \
			go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(APP)-$(1) $(CMD); \
	else \
		CGO_ENABLED=0 GOOS=$(word 1,$(subst -, ,$(1))) GOARCH=$(word 2,$(subst -, ,$(1))) \
			go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(APP)-$(1) $(CMD); \
	fi
endef

$(foreach p,$(PLATFORMS),$(eval $(call BUILD_PLATFORM,$(p))))

install: $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH)
	install -d $(PREFIX)/bin
	install -m 755 $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH) $(PREFIX)/bin/$(APP)

clean:
	rm -rf $(DIST)
