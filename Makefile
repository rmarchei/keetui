APP      := keetui
MODULE   := github.com/rmarchei/keetui
CMD      := ./cmd/keetui
DIST     := dist

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOIMAGE  := golang:1.26-alpine
LDFLAGS  := -s -w -X main.version=$(VERSION)

PLATFORMS := darwin-amd64 darwin-arm64 linux-amd64 linux-arm64

# Auto-detect container runtime (docker, podman, or container); override with CONTAINER_RUNTIME=
CONTAINER_RUNTIME ?= $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null || command -v container 2>/dev/null)

HOST_OS   := $(shell uname -s | tr '[:upper:]' '[:lower:]')
HOST_ARCH := $(shell uname -m | sed 's/x86_64/amd64/')

CROSS_PLATFORMS := $(filter-out $(HOST_OS)-$(HOST_ARCH),$(PLATFORMS))

PREFIX   ?= /usr/local

.PHONY: build test all clean install $(CROSS_PLATFORMS)

build: $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH)

$(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH): $(shell find cmd internal -name '*.go')
	@mkdir -p $(DIST)
	@if [ -n "$(CONTAINER_RUNTIME)" ]; then \
		$(CONTAINER_RUNTIME) run --rm \
			-v $(CURDIR):/src \
			-w /src \
			-e CGO_ENABLED=0 \
			-e GOOS=$(HOST_OS) \
			-e GOARCH=$(HOST_ARCH) \
			$(GOIMAGE) \
			go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH) $(CMD); \
	else \
		CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH) $(CMD); \
	fi

test:
	@if [ -n "$(CONTAINER_RUNTIME)" ]; then \
		$(CONTAINER_RUNTIME) run --rm \
			-v $(CURDIR):/src \
			-w /src \
			$(GOIMAGE) \
			go test ./...; \
	else \
		go test ./...; \
	fi

all: build $(CROSS_PLATFORMS)

define BUILD_PLATFORM
$(1): $(DIST)/$(APP)-$(1)
$(DIST)/$(APP)-$(1):
	@if [ -z "$(CONTAINER_RUNTIME)" ]; then echo "Error: No container runtime found. Install docker or podman." >&2; exit 1; fi
	@mkdir -p $(DIST)
	$(CONTAINER_RUNTIME) run --rm \
		-v $(CURDIR):/src \
		-w /src \
		-e CGO_ENABLED=0 \
		-e GOOS=$(word 1,$(subst -, ,$(1))) \
		-e GOARCH=$(word 2,$(subst -, ,$(1))) \
		$(GOIMAGE) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(APP)-$(1) $(CMD)
endef

$(foreach p,$(CROSS_PLATFORMS),$(eval $(call BUILD_PLATFORM,$(p))))

install: $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH)
	install -d $(PREFIX)/bin
	install -m 755 $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH) $(PREFIX)/bin/$(APP)

clean:
	rm -rf $(DIST)
