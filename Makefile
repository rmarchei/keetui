APP      := keetui
MODULE   := github.com/rmarchei/keetui
CMD      := ./cmd/keetui
DIST     := dist

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOIMAGE  := golang:1.24-alpine
LDFLAGS  := -s -w -X main.version=$(VERSION)

PLATFORMS := darwin-amd64 darwin-arm64 linux-amd64 linux-arm64

# Auto-detect container runtime
CONTAINER_RUNTIME := $(shell command -v docker 2>/dev/null || command -v podman 2>/dev/null)
ifeq ($(CONTAINER_RUNTIME),)
  $(error No container runtime found. Install docker or podman.)
endif

.PHONY: all clean $(PLATFORMS)

all: $(PLATFORMS)

define BUILD_PLATFORM
$(1): $(DIST)/$(APP)-$(1)
$(DIST)/$(APP)-$(1):
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

clean:
	rm -rf $(DIST)
