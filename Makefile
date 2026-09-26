# jira — build and install from source.
#
#   make install        build and copy to ~/.local/bin/jira (override with PREFIX=...)
#   make build          build into bin/jira
#   make check          vet + test + build: run it before committing
#   make release        every release binary + checksums.txt into dist/
#   make e2e            bin/jira-e2e, which can be pointed at a fake Jira
#
# Most people don't need this: the README has the one-line installer.
# Releases are cut by CI from conventional commits (see CONTRIBUTING.md).

BINARY   := jira
MODULE   := github.com/ngavilan-dogfy/jira-cli
BUILD    := bin/$(BINARY)
PREFIX   ?= $(HOME)/.local
BINDIR   := $(PREFIX)/bin
# A release version only on a release tag; anything else is a dev build,
# which never nags about updates and reports its commit instead.
VERSION  ?= $(shell git describe --tags --exact-match --match 'v[0-9]*' 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(MODULE)/cmd.Version=$(VERSION)

.PHONY: build install uninstall test vet check release e2e clean

build:
	@command -v go >/dev/null 2>&1 || { echo "Go is not installed: https://go.dev/dl (or: brew install go)"; exit 1; }
	@mkdir -p bin
	go build -ldflags "$(LDFLAGS)" -o $(BUILD) ./cmd/$(BINARY)

# Copy + rename, never overwrite in place: macOS kills a binary rewritten
# over its old inode ("killed: 9"), and a running jira keeps working.
install: build
	@mkdir -p $(BINDIR)
	@cp $(BUILD) $(BINDIR)/.$(BINARY).new && mv -f $(BINDIR)/.$(BINARY).new $(BINDIR)/$(BINARY)
	@echo "installed → $(BINDIR)/$(BINARY) ($(VERSION))"
	@case ":$$PATH:" in *":$(BINDIR):"*) ;; *) \
		echo ""; \
		echo "! $(BINDIR) is not in your PATH, so 'jira' won't be found yet."; \
		echo "  Add this line to your ~/.zshrc (or ~/.bashrc) and open a new terminal:"; \
		echo "    export PATH=\"$(BINDIR):\$$PATH\"";; esac

uninstall:
	rm -f $(BINDIR)/$(BINARY)
	@echo "removed $(BINDIR)/$(BINARY) (your settings stay in ~/.config/jira-cli)"

test:
	go test ./...
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck -s sh install.sh && shellcheck scripts/*.sh; fi

vet:
	go vet ./...

check: vet test build

release:
	scripts/build-release.sh $(VERSION)

# JIRA_E2E_BASE=<fake Jira URL> points setup/doctor at it; see cmd/e2e_hooks.go.
e2e:
	@mkdir -p bin
	go build -tags e2e -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-e2e ./cmd/$(BINARY)

clean:
	rm -rf bin/ dist/
