BIN := bin
# The LGPL-3.0 doubletake fork lives in its own repo (sibling clone).
VENDOR ?= ../doubletake-samsung

.PHONY: all build ui vet install-user clean

all: build

build:
	$(MAKE) -C $(VENDOR)
	mkdir -p $(BIN)
	ln -sf ../$(VENDOR)/bin/doubletake $(BIN)/doubletake
	ln -sf ../$(VENDOR)/bin/doubletake-ctl $(BIN)/doubletake-ctl
	$(MAKE) ui

# The Go control-panel server (source: ui/, `samcast ui` serves it). ui/ is
# built by a parallel workstream and may be absent — the directory check keeps
# make working without it instead of failing on a missing go module.
ui:
	@mkdir -p $(BIN)
	@if [ -d ui ]; then cd ui && go build -o ../$(BIN)/samcast-ui .; \
	else echo "make: ui/ missing — skipping samcast-ui build"; fi

vet:
	@if [ -d ui ]; then cd ui && go vet ./...; \
	else echo "make: ui/ missing — nothing to vet"; fi

clean:
	rm -rf $(BIN)
	$(MAKE) -C $(VENDOR) clean
