APP      := keetui
MODULE   := github.com/rmarchei/keetui
CMD      := ./cmd/keetui
DIST     := dist

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOIMAGE  := golang:1.24.13-alpine
LDFLAGS  := -s -w -X main.version=$(VERSION)

PLATFORMS := darwin-amd64 darwin-arm64 linux-amd64 linux-arm64

# Auto-detect container runtime (only required for cross-platform builds)
CONTAINER_RUNTIME := $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null)

PREFIX   ?= /usr/local

.PHONY: all clean install $(PLATFORMS)

all: $(PLATFORMS)

define BUILD_PLATFORM
$(1): $(DIST)/$(APP)-$(1)
$(DIST)/$(APP)-$(1):
	@if [ -z "$(CONTAINER_RUNTIME)" ]; then echo "Error: No container runtime found. Install docker or podman." >&2; exit 1; fi
	@mkdir -p $(DIST)
	$(CONTAINER_RUNTIME) run --rm \
		-v $(CURDIR):/src:Z \
		-w /src \
		-e CGO_ENABLED=0 \
		-e GOOS=$(word 1,$(subst -, ,$(1))) \
		-e GOARCH=$(word 2,$(subst -, ,$(1))) \
		$(GOIMAGE) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o $(DIST)/$(APP)-$(1) $(CMD)
endef

$(foreach p,$(PLATFORMS),$(eval $(call BUILD_PLATFORM,$(p))))

HOST_OS   := $(shell uname -s | tr '[:upper:]' '[:lower:]')
HOST_ARCH := $(shell uname -m | sed 's/x86_64/amd64/')

install: $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH)
	install -d $(PREFIX)/bin
	install -m 755 $(DIST)/$(APP)-$(HOST_OS)-$(HOST_ARCH) $(PREFIX)/bin/$(APP)

clean:
	rm -rf $(DIST)
