BIN := bin
VENDOR := vendor/doubletake

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
