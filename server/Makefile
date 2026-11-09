# Lumen — Bauen und Ausliefern
#
# make            baut Lumen für diesen Rechner
# make relay      baut den Vermittler für den Fernzugriff
# make test       alle Tests
# make run        startet Lumen mit einem Datenordner im Projekt
# make docker     baut die Docker-Abbilder
# make release    baut für alle Plattformen (braucht Cross-Compiler, s.u.)

VERSION ?= 0.1.0
LDFLAGS  = -s -w -X main.version=$(VERSION)
GOFLAGS  = -mod=vendor -trimpath

BIN     = lumen
RELAY   = lumen-relay
DIST    = dist

.PHONY: all relay test run tidy clean docker release help

all: $(BIN)

$(BIN):
	CGO_ENABLED=1 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/lumen
	@echo "Fertig: ./$(BIN) ($$(du -h $(BIN) | cut -f1)) — Oberfläche ist mit drin."

# Der Vermittler braucht weder SQLite noch FFmpeg — darum ohne cgo, und
# damit läuft er auf jeder Linux-Maschine, egal wie alt deren Bibliotheken
# sind.
relay: $(RELAY)

$(RELAY):
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(RELAY) ./cmd/lumen-relay
	@echo "Fertig: ./$(RELAY) ($$(du -h $(RELAY) | cut -f1)) — läuft auf dem Mietserver."

test:
	go test $(GOFLAGS) ./...

run: $(BIN)
	./$(BIN) --data ./lumen-data --addr :8097 -v

tidy:
	go mod tidy && go mod vendor

clean:
	rm -rf $(BIN) $(RELAY) $(DIST) lumen-data

# Vom Wurzelverzeichnis aus bauen, damit LICENSE mit ins Abbild kommt —
# die AGPL verlangt, dass der Lizenztext jede Kopie begleitet.
docker:
	docker build -f Dockerfile -t lumen:$(VERSION) -t lumen:latest ..
	docker build -f Dockerfile.relay -t lumen-relay:$(VERSION) -t lumen-relay:latest ..

# ---------------------------------------------------------------- Ausliefern
#
# Lumen bindet SQLite über C ein. Das macht den Server schnell und sparsam,
# heisst aber: für eine andere Plattform braucht es deren C-Übersetzer.
#
# Der bequemste Weg ist Zig — ein Programm, das für alle Ziele übersetzen
# kann (https://ziglang.org/download/). Ist es installiert, genügt:
#
#     make release
#
# Ohne Zig baut `make` die Fassung für diesen Rechner, und das Docker-Abbild
# deckt Linux auf amd64 und arm64 ab.

ZIG := $(shell command -v zig 2>/dev/null)

# Gebaut wird in einen eigenen Ordner je Plattform, und daraus wird ein
# Paket mit Lizenz und Lesemich. Eine nackte Binärdatei weiterzugeben wäre
# gegen die AGPL: der Lizenztext muss jede Kopie begleiten.
define build_target
	@echo "→ $(1)/$(2)"
	@mkdir -p $(DIST)/$(BIN)-$(1)-$(2)
	@GOOS=$(1) GOARCH=$(2) CGO_ENABLED=1 CC="$(3)" \
		go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-$(1)-$(2)/$(BIN)$(4) ./cmd/lumen
	@GOOS=$(1) GOARCH=$(2) CGO_ENABLED=0 \
		go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-$(1)-$(2)/$(RELAY)$(4) ./cmd/lumen-relay
	@cp ../LICENSE ../README.md $(DIST)/$(BIN)-$(1)-$(2)/
	@cd $(DIST) && tar czf $(BIN)-$(1)-$(2).tar.gz $(BIN)-$(1)-$(2) && rm -rf $(BIN)-$(1)-$(2)
endef

release:
ifndef ZIG
	@echo "Für mehrere Plattformen wird Zig gebraucht: https://ziglang.org/download/"
	@echo "Ohne Zig: 'make' baut für diesen Rechner, 'make docker' für Linux."
	@exit 1
endif
	$(call build_target,linux,amd64,zig cc -target x86_64-linux-musl,)
	$(call build_target,linux,arm64,zig cc -target aarch64-linux-musl,)
	$(call build_target,linux,arm,zig cc -target arm-linux-musleabihf,)
	$(call build_target,darwin,arm64,zig cc -target aarch64-macos,)
	$(call build_target,darwin,amd64,zig cc -target x86_64-macos,)
	$(call build_target,windows,amd64,zig cc -target x86_64-windows-gnu,.exe)
	@cd $(DIST) && for f in *.tar.gz; do sha256sum "$$f" >> pruefsummen.txt; done
	@echo; ls -lh $(DIST)

help:
	@sed -n '2,9p' Makefile | sed 's/^# \?//'
