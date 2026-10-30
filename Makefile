BIN := bin
# The LGPL-3.0 doubletake fork lives in its own repo (sibling clone).
VENDOR ?= ../doubletake-samsung

# Recursive $(MAKE) goes through `env -u ARGV0`: this machine's make 4.4
# honors an exported $ARGV0 (set by the ZCode AppImage) as its own program
# name, which breaks nested invocations.
.PHONY: all build ui tray app vet install-user clean

all: build

build:
	env -u ARGV0 $(MAKE) -C $(VENDOR)
	mkdir -p $(BIN)
	ln -sf ../$(VENDOR)/bin/doubletake $(BIN)/doubletake
	ln -sf ../$(VENDOR)/bin/doubletake-ctl $(BIN)/doubletake-ctl
	env -u ARGV0 $(MAKE) ui tray

# The Go control-panel server (source: ui/, `samcast ui` serves it). ui/ is
# built by a parallel workstream and may be absent — the directory check keeps
# make working without it instead of failing on a missing go module. Built
# static (-ldflags '-s -w', CGO off) so it can be embedded in the single-file
# distribution (`make app`).
ui:
	@mkdir -p $(BIN)
	@if [ -d ui ]; then cd ui && CGO_ENABLED=0 go build -ldflags='-s -w' -o ../$(BIN)/samcast-ui .; \
	else echo "make: ui/ missing — skipping samcast-ui build"; fi

# Tray icon helper (source: tray/, `samcast tray`). Same static rules as ui/ —
# it is embedded in the single-file distribution too.
tray:
	@mkdir -p $(BIN)
	@if [ -d tray ]; then cd tray && CGO_ENABLED=0 go build -ldflags='-s -w' -o ../$(BIN)/samcast-tray .; \
	else echo "make: tray/ missing — skipping samcast-tray build"; fi

# Single-file distribution: dist/samcast — one static x86-64 executable
# embedding the whole appliance (samcast, samcast-picker, the fork's static
# doubletake/doubletake-ctl, samcast-ui). Explicit-only: `make` still just
# builds the dev tree.
app:
	env -u ARGV0 $(MAKE) -C $(VENDOR) doubletake-release doubletake-ctl-release
	env -u ARGV0 $(MAKE) ui tray
	rm -rf app/assets && mkdir -p app/assets/bin
	cp samcast samcast-picker app/assets/
	cp $(VENDOR)/doubletake $(VENDOR)/doubletake-ctl app/assets/bin/
	cp $(BIN)/samcast-ui $(BIN)/samcast-tray app/assets/bin/
	# re-create the embed anchor (needed when assets/ is empty at commit time)
	printf '*\n!.gitignore\n' > app/assets/.gitignore
	cd app && CGO_ENABLED=0 go build -ldflags='-s -w' -o ../dist/samcast .

vet:
	@if [ -d ui ]; then cd ui && go vet ./...; \
	else echo "make: ui/ missing — nothing to vet"; fi
	@if [ -d tray ]; then cd tray && go vet ./...; \
	else echo "make: tray/ missing — nothing to vet"; fi

clean:
	rm -rf $(BIN)
	rm -rf dist
	rm -rf app/assets
	mkdir -p app/assets
	printf '*\n!.gitignore\n' > app/assets/.gitignore
	env -u ARGV0 $(MAKE) -C $(VENDOR) clean
