BIN := bin
# The LGPL-3.0 doubletake fork lives in its own repo (sibling clone).
VENDOR ?= ../doubletake-samsung

# Nested make invocations go through MAKE_BIN, a literal `make`: make 4.4
# honors an exported $ARGV0 (set by the ZCode AppImage) as its own program
# name — $(MAKE) then expands to the AppImage and nested invocations exec it
# instead of make, silently skipping the vendor build. The variable is
# consumed by make itself, so neither $(ARGV0) nor the recipe shell can detect
# the condition: expand plain `make` from PATH unconditionally (MAKEFLAGS and
# the jobserver still propagate to it like any sub-make).
MAKE_BIN := make

.PHONY: all build ui tray app vet clean

all: build

build:
	env -u ARGV0 $(MAKE_BIN) -C $(VENDOR)
	mkdir -p $(BIN)
	ln -sf ../$(VENDOR)/bin/doubletake $(BIN)/doubletake
	ln -sf ../$(VENDOR)/bin/doubletake-ctl $(BIN)/doubletake-ctl
	env -u ARGV0 $(MAKE_BIN) ui tray

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
	env -u ARGV0 $(MAKE_BIN) -C $(VENDOR) doubletake-release doubletake-ctl-release
	env -u ARGV0 $(MAKE_BIN) ui tray
	@test -f $(BIN)/samcast-ui || { echo "make: bin/samcast-ui missing — build the control panel first (check ui/ build output above)"; exit 1; }
	@test -f $(BIN)/samcast-tray || { echo "make: bin/samcast-tray missing — build the tray helper first (check tray/ build output above)"; exit 1; }
	rm -rf app/assets && mkdir -p app/assets/bin
	# re-create the embed anchor FIRST: until the copies land, the anchor is
	# the only thing keeping `go build` working — and it keeps the ~25 MB of
	# copied binaries ignored if the build is interrupted mid-target
	printf '*\n!.gitignore\n' > app/assets/.gitignore
	cp samcast samcast-picker app/assets/
	cp $(VENDOR)/doubletake $(VENDOR)/doubletake-ctl app/assets/bin/
	cp $(BIN)/samcast-ui $(BIN)/samcast-tray app/assets/bin/
	cd app && CGO_ENABLED=0 go build -ldflags='-s -w' -o ../dist/samcast .

vet:
	@cd app && go vet ./...
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
	env -u ARGV0 $(MAKE_BIN) -C $(VENDOR) clean
