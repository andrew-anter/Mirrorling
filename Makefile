BIN := bin
# The LGPL-3.0 doubletake fork lives in its own repo (sibling clone).
VENDOR ?= ../doubletake-samsung

.PHONY: all build install-user clean

all: build

build:
	$(MAKE) -C $(VENDOR)
	mkdir -p $(BIN)
	ln -sf ../$(VENDOR)/bin/doubletake $(BIN)/doubletake
	ln -sf ../$(VENDOR)/bin/doubletake-ctl $(BIN)/doubletake-ctl

clean:
	rm -rf $(BIN)
	$(MAKE) -C $(VENDOR) clean
