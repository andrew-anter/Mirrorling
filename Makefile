BIN := bin
# Verbatim third-party license texts, shipped beside LICENSE and the LGPL/GPL
# texts. `make licenses-check` verifies every block against the module cache.
LICENSE_BUNDLE := LICENSES/THIRD-PARTY.txt
# The LGPL-3.0 doubletake fork lives in its own repo (sibling clone).
VENDOR ?= ../doubletake-samsung
# Absolute, because the links below are created *inside* bin/: a relative
# target has to be written as ../<vendor>, which turns an absolute
# VENDOR=/some/path into the dangling ../..//some/path. abspath normalizes
# both forms, so the default sibling clone and an explicit VENDOR both work.
VENDOR_ABS := $(abspath $(VENDOR))

# Release identity, baked into the launcher image (app/main.go): `mirrorling
# version` reports it. Evaluated per make run so COMMIT matches the built tree.
VERSION := $(shell cat VERSION 2>/dev/null || echo dev)
# Tracked or untracked changes make the build not reproducible from HEAD, so
# the artifact must not claim HEAD's commit as its exact source. Rebuild after
# committing to get a clean hash for a release.
COMMIT := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
DIRTY := $(shell git status --porcelain 2>/dev/null | grep -q . && echo -dirty)
APP_LDFLAGS := -s -w -X main.buildVersion=$(VERSION) -X main.buildCommit=$(COMMIT)$(DIRTY)

# Nested make invocations go through MAKE_BIN, a literal `make`: make 4.4
# honors an exported $ARGV0 (set by the ZCode AppImage) as its own program
# name — $(MAKE) then expands to the AppImage and nested invocations exec it
# instead of make, silently skipping the vendor build. The variable is
# consumed by make itself, so neither $(ARGV0) nor the recipe shell can detect
# the condition: expand plain `make` from PATH unconditionally (MAKEFLAGS and
# the jobserver still propagate to it like any sub-make).
MAKE_BIN := make

.PHONY: all build ui tray app test checksums licenses-check release vet clean

all: build

build:
	env -u ARGV0 $(MAKE_BIN) -C $(VENDOR_ABS)
	mkdir -p $(BIN)
	ln -sf $(VENDOR_ABS)/bin/doubletake $(BIN)/doubletake
	ln -sf $(VENDOR_ABS)/bin/doubletake-ctl $(BIN)/doubletake-ctl
	env -u ARGV0 $(MAKE_BIN) ui tray

# The Go control-panel server (source: ui/, `mirrorling ui` serves it). ui/ is
# built by a parallel workstream and may be absent — the directory check keeps
# make working without it instead of failing on a missing go module.
# It is a Wails app, so CGO is required: the result is dynamically linked
# against the system gtk3 and webkit2gtk-4.1, which are runtime dependencies
# (user-installed, never bundled). The build tags are load-bearing:
#   production   selects Wails' release app/runtime instead of the dev build
#                (internal/app/app_production.go);
#   webkit2_41   selects the libsoup3 pkg-config name webkit2gtk-4.1 (Debian 13:
#                libwebkit2gtk-4.1-0; Arch: webkit2gtk-4.1). Without it Wails
#                asks for the legacy webkit2gtk-4.0, which links on Arch but
#                does not exist on Debian;
#   desktop      is a no-op in Wails v2.17 (nothing is gated on that tag); it
#                is kept so the documented command still matches older docs.
ui:
	@mkdir -p $(BIN)
	@if [ -d ui ]; then cd ui && CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 -ldflags='-s -w' -o ../$(BIN)/mirrorling-ui .; \
	else echo "make: ui/ missing — skipping mirrorling-ui build"; fi

# Tray icon helper (source: tray/). Pure Go: CGO_ENABLED=0, statically linked
# (it is embedded in the single-file distribution too, and must not inherit
# the UI's gtk/webkit runtime deps).
tray:
	@mkdir -p $(BIN)
	@if [ -d tray ]; then cd tray && CGO_ENABLED=0 go build -ldflags='-s -w' -o ../$(BIN)/mirrorling-tray .; \
	else echo "make: tray/ missing — skipping mirrorling-tray build"; fi

# Single-file distribution: dist/mirrorling — one static x86-64 executable
# embedding the whole appliance (mirrorling, mirrorling-picker, the fork's static
# doubletake/doubletake-ctl, mirrorling-ui, mirrorling-tray) plus the license files
# the launcher can print (`mirrorling --licenses`). Explicit-only: `make` still
# just builds the dev tree.
app:
	env -u ARGV0 $(MAKE_BIN) -C $(VENDOR_ABS) doubletake-release doubletake-ctl-release
	env -u ARGV0 $(MAKE_BIN) ui tray
	@test -f $(BIN)/mirrorling-ui || { echo "make: bin/mirrorling-ui missing — build the control panel first (check ui/ build output above)"; exit 1; }
	@test -f $(BIN)/mirrorling-tray || { echo "make: bin/mirrorling-tray missing — build the tray helper first (check tray/ build output above)"; exit 1; }
	@test -f LICENSE && test -d LICENSES && test -f $(LICENSE_BUNDLE) && test -f THIRD-PARTY-NOTICES.md || { echo "make: LICENSE, LICENSES/ (with THIRD-PARTY.txt) and THIRD-PARTY-NOTICES.md must exist — they ship inside the image"; exit 1; }
	rm -rf app/assets && mkdir -p app/assets/bin
	# re-create the embed anchor FIRST: until the copies land, the anchor is
	# the only thing keeping `go build` working — and it keeps the ~25 MB of
	# copied binaries ignored if the build is interrupted mid-target
	printf '*\n!.gitignore\n' > app/assets/.gitignore
	cp mirrorling mirrorling-picker app/assets/
	cp $(VENDOR_ABS)/doubletake $(VENDOR_ABS)/doubletake-ctl app/assets/bin/
	cp $(BIN)/mirrorling-ui $(BIN)/mirrorling-tray app/assets/bin/
	cp LICENSE THIRD-PARTY-NOTICES.md app/assets/
	cp -R LICENSES app/assets/LICENSES
	mkdir -p dist
	cd app && CGO_ENABLED=0 go build -ldflags='$(APP_LDFLAGS)' -o ../dist/mirrorling .

# Focused test suites. The Go packages test their own logic; tests/cli.sh (when
# present) covers the bash CLI, picker and portal/session handling.
test:
	@if [ -d ui ]; then cd ui && go test ./...; else echo "make: ui/ missing — skipping mirrorling-ui tests"; fi
	@if [ -d tray ]; then cd tray && go test ./...; else echo "make: tray/ missing — skipping mirrorling-tray tests"; fi
	@if [ -f tests/cli.sh ]; then bash tests/cli.sh; else echo "make: tests/cli.sh missing — skipping CLI tests"; fi

# Checksums for the built image; run after `make app` (dist/mirrorling exists).
# The digest is recorded against the bare name `mirrorling`, not `dist/mirrorling`,
# so `sha256sum -c SHA256SUMS` works in the download directory where the
# release asset lands next to the checksum file.
checksums:
	@test -f dist/mirrorling || { echo "make: dist/mirrorling missing — run 'make app' first"; exit 1; }
	cd dist && sha256sum mirrorling > SHA256SUMS
	cd dist && sha256sum -c SHA256SUMS
	@cat dist/SHA256SUMS

# Verify the shipped third-party bundle is complete and verbatim. The module
# set is re-derived from ui/go.mod, tray/go.mod and the modules recorded in the
# built fork binary; every module must have a cache LICENSE/COPYING/NOTICE file
# (a miss fails instead of being skipped) and its block in the bundle must equal
# that file byte for byte. The Go standard library block is checked the same
# way, preferring $(go env GOROOT)/LICENSE and falling back to the packaged copy
# some distributions install under /usr/share/licenses.
licenses-check:
	@test -s $(LICENSE_BUNDLE) || { echo "make: $(LICENSE_BUNDLE) missing or empty"; exit 1; }
	@set -eu; \
	export LC_ALL=C; \
	bundle='$(LICENSE_BUNDLE)'; \
	vendor='$(VENDOR_ABS)'; \
	gmc="$$(go env GOMODCACHE)"; \
	sep='================================================================================'; \
	tmp="$$(mktemp -d)"; \
	trap 'rm -rf "$$tmp"' 0 1 2 15; \
	: > "$$tmp/mods"; \
	for dir in ui tray; do \
	  [ -f "$$dir/go.mod" ] || { echo "make: licenses-check: $$dir/go.mod missing"; exit 1; }; \
	  ( cd "$$dir" && go mod edit -json > "$$tmp/mod.json" ) || { echo "make: licenses-check: cannot read $$dir/go.mod"; exit 1; }; \
	  awk ' \
	    /"Require"[[:space:]]*:/ { req=1; next } \
	    req && /"Path"[[:space:]]*:/ { \
	      line=$$0; sub(/.*"Path"[[:space:]]*:[[:space:]]*"/, "", line); sub(/".*/, "", line); path=line; next \
	    } \
	    req && /"Version"[[:space:]]*:/ { \
	      line=$$0; sub(/.*"Version"[[:space:]]*:[[:space:]]*"/, "", line); sub(/".*/, "", line); print path "@" line; next \
	    } \
	    req && /^[[:space:]]*\][[:space:]]*,?[[:space:]]*$$/ { req=0 } \
	  ' "$$tmp/mod.json" >> "$$tmp/mods"; \
	done; \
	[ -f "$$vendor/doubletake" ] || { echo "make: licenses-check: $$vendor/doubletake missing — run make first"; exit 1; }; \
	go version -m "$$vendor/doubletake" > "$$tmp/doubletake.mods" || { echo "make: licenses-check: cannot read build info from $$vendor/doubletake"; exit 1; }; \
	awk -F'\t' '$$2 == "dep" { print $$3 "@" $$4 }' "$$tmp/doubletake.mods" >> "$$tmp/mods"; \
	sort -u "$$tmp/mods" -o "$$tmp/mods"; \
	[ -s "$$tmp/mods" ] || { echo "make: licenses-check: no modules found"; exit 1; }; \
	mods="$$(cat "$$tmp/mods")"; \
	extract() { \
	  awk -v want="$$1" -v sep="$$sep" ' \
	    $$0 == "MODULE: " want { state=1; next } \
	    state == 1 && /^LICENSE: / { state=2; next } \
	    state == 2 && $$0 == sep { body=1; state=0; next } \
	    body { \
	      if ($$0 == sep) { pending=1; next } \
	      if (pending) { \
	        if ($$0 ~ /^MODULE: /) { pending=0; exit } \
	        print sep; pending=0 \
	      } \
	      print \
	    } \
	    END { if (pending) print sep } \
	  ' "$$bundle"; \
	}; \
	for m in $$mods; do \
	  p="$${m%@*}"; v="$${m##*@}"; d="$$gmc/$$p@$$v"; \
	  [ -d "$$d" ] || { echo "make: licenses-check: module cache missing $$m ($$d) — run go mod download"; exit 1; }; \
	  find "$$d" -maxdepth 1 -type f \
	    \( -iname 'LICENSE*' -o -iname 'LICENCE*' -o -iname 'COPYING*' \
	       -o -iname 'NOTICE*' -o -iname 'UNLICENSE*' -o -iname 'UNLICENCE*' \) \
	    > "$$tmp/files.unsorted"; \
	  sort "$$tmp/files.unsorted" -o "$$tmp/files"; \
	  [ -s "$$tmp/files" ] || { echo "make: licenses-check: no LICENSE/COPYING/NOTICE text for $$m"; exit 1; }; \
	  grep -Fqx "MODULE: $$m" "$$bundle" || { echo "make: licenses-check: bundle has no block for $$m"; exit 1; }; \
	  : > "$$tmp/want"; \
	  while IFS= read -r f; do \
	    [ -s "$$f" ] || { echo "make: licenses-check: empty license file $$f"; exit 1; }; \
	    cat "$$f" >> "$$tmp/want"; \
	  done < "$$tmp/files"; \
	  extract "$$m" > "$$tmp/got"; \
	  cmp -s "$$tmp/want" "$$tmp/got" || { echo "make: licenses-check: license text mismatch for $$m"; exit 1; }; \
	done; \
	go_license="$$(go env GOROOT)/LICENSE"; \
	if [ ! -f "$$go_license" ]; then \
	  for cand in /usr/share/licenses/go/LICENSE /usr/local/go/LICENSE; do \
	    if [ -f "$$cand" ]; then go_license="$$cand"; break; fi; \
	  done; \
	fi; \
	[ -f "$$go_license" ] || { echo "make: licenses-check: Go standard library LICENSE not found"; exit 1; }; \
	grep -Fqx "MODULE: Go standard library" "$$bundle" || { echo "make: licenses-check: bundle has no Go standard library block"; exit 1; }; \
	extract "Go standard library" > "$$tmp/got"; \
	cmp -s "$$go_license" "$$tmp/got" || { echo "make: licenses-check: Go standard library text mismatch"; exit 1; }; \
	echo "make: licenses-check: $$(grep -c '^MODULE: ' "$$bundle") license blocks verified against the module cache"

# Public artifact: build the image, then record its digest.
release: app checksums
	@echo "release artifacts:"
	@echo "  $(abspath dist/mirrorling)"
	@echo "  $(abspath dist/SHA256SUMS)"

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
	env -u ARGV0 $(MAKE_BIN) -C $(VENDOR_ABS) clean
