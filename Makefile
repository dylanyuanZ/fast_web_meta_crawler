
# Project metadata
MODULE   := github.com/dylanyuanZ/fast_web_meta_crawler
BIN_DIR  := bin

# All cmd entry points (auto-discovered from cmd/ subdirectories)
CMDS     := $(notdir $(wildcard cmd/*))
BINARIES := $(addprefix $(BIN_DIR)/,$(CMDS))

# All Go source files (for dependency tracking)
GO_SOURCES := $(shell find cmd/ src/ -name '*.go' 2>/dev/null)

# Go build flags
GO       := go
GOFLAGS  := -trimpath
LDFLAGS  := -s -w

# ──────────────────────────────────────────────
# Targets
# ──────────────────────────────────────────────

.PHONY: all clean $(CMDS)

## Build all binaries into bin/
all: $(BINARIES)

## Build individual binary: make crawler / make probe / make killold
$(CMDS): %: $(BIN_DIR)/%

$(BIN_DIR)/%: $(GO_SOURCES)
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -ldflags '$(LDFLAGS)' -o $@ ./cmd/$*

## Remove all build artifacts
clean:
	rm -rf $(BIN_DIR)
