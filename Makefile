# Mappa dell'Avventura: app per Mac e Windows (Wails + SQLite)
# Scrivi "make" nel Terminale, dentro questa cartella, per vedere i comandi.

APP           := MappaAvventura
WAILS_VERSION ?= latest
GOBIN_DIR     := $(shell go env GOPATH 2>/dev/null)/bin
WAILS         := $(GOBIN_DIR)/wails
BIN           := build/bin
SEED          := seed/MappaDati
DIST_WIN      := dist/windows/$(APP)
MAC_DATI      := $(HOME)/Library/Application Support/MappaAvventura
WFLAGS        := -s -skipbindings -trimpath

.DEFAULT_GOAL := aiuto
.PHONY: aiuto setup controlla app mac windows dev avvia dist-windows chiavetta installa-mac prova pulisci

aiuto:
	@echo ""
	@echo "  Mappa dell'Avventura - comandi"
	@echo "  ------------------------------"
	@echo "  make setup          la prima volta: installa Go e Wails (serve Homebrew, vedi LEGGIMI.md)"
	@echo "  make controlla      verifica che sul Mac ci sia tutto il necessario"
	@echo "  make app            crea l'app per Mac e il programma per Windows in $(BIN)/"
	@echo "  make dev            apre l'app sul Mac lavorando direttamente sui dati del progetto (seed/MappaDati):"
	@echo "                      brani, immagini e testi che carichi finiscono nella sorgente"
	@echo "  make avvia          come make dev, ma senza ricompilare: apre subito l'app gia' pronta in $(BIN)/"
	@echo "  make dist-windows   crea dist/windows/$(APP): programma per Windows + dati del progetto,"
	@echo "                      pronta da scaricare da GitHub su un PC Windows"
	@echo "  make chiavetta DEST=/Volumes/NOME"
	@echo "                      copia app, programma e dati sulla chiavetta (i dati esistenti non vengono toccati)"
	@echo "  make installa-mac   mette l'app in Applicazioni e prepara i dati sul Mac"
	@echo "  make prova          apre la mappa nel browser su http://127.0.0.1:8080 (senza finestra, per controlli)"
	@echo "  make pulisci        cancella i programmi creati (i dati restano)"
	@echo ""

setup:
	@command -v brew >/dev/null 2>&1 || { echo "Manca Homebrew: installalo con il comando del LEGGIMI.md, poi rilancia make setup"; exit 1; }
	@xcode-select -p >/dev/null 2>&1 || { echo "Installo gli strumenti di sviluppo Apple: segui la finestra, poi rilancia make setup"; xcode-select --install; exit 1; }
	brew install go
	$$(brew --prefix)/bin/go install github.com/wailsapp/wails/v2/cmd/wails@$(WAILS_VERSION)
	$$(brew --prefix)/bin/go get github.com/wailsapp/wails/v2@$(WAILS_VERSION)
	$$(brew --prefix)/bin/go mod tidy
	@echo ""
	@echo "Fatto. Ora scrivi: make app"

controlla:
	@command -v go >/dev/null 2>&1 || { echo "Go non trovato: lancia make setup"; exit 1; }
	@test -x "$(WAILS)" || { echo "Wails non trovato: lancia make setup"; exit 1; }
	go version
	"$(WAILS)" doctor

app: mac windows
	@echo ""
	@echo "Pronti:"
	@echo "  $(BIN)/$(APP).app   (Mac)"
	@echo "  $(BIN)/$(APP).exe   (Windows)"
	@echo "Per la chiavetta: make chiavetta DEST=/Volumes/NOME_CHIAVETTA"

mac:
	@test -x "$(WAILS)" || { echo "Wails non trovato: lancia make setup"; exit 1; }
	"$(WAILS)" build $(WFLAGS) -platform darwin/universal

windows:
	@test -x "$(WAILS)" || { echo "Wails non trovato: lancia make setup"; exit 1; }
	"$(WAILS)" build $(WFLAGS) -platform windows/amd64

dev:
	@test -x "$(WAILS)" || { echo "Wails non trovato: lancia make setup"; exit 1; }
	"$(WAILS)" build $(WFLAGS) -platform darwin
	@echo ""
	@echo "Modalita' sviluppo: i dati sono in $(CURDIR)/$(SEED)"
	@echo "Chiudi la finestra della mappa per tornare al Terminale."
	MAPPA_DATI="$(CURDIR)/$(SEED)" "$(BIN)/$(APP).app/Contents/MacOS/$(APP)"

avvia:
	@test -d "$(BIN)/$(APP).app" || $(MAKE) mac
	@echo "Dati: $(CURDIR)/$(SEED)  -  chiudi la finestra della mappa per tornare al Terminale."
	MAPPA_DATI="$(CURDIR)/$(SEED)" "$(BIN)/$(APP).app/Contents/MacOS/$(APP)"

dist-windows: windows
	rm -rf "$(DIST_WIN)"
	mkdir -p "$(DIST_WIN)/MappaDati"
	cp "$(BIN)/$(APP).exe" "$(DIST_WIN)/"
	cp -R "$(SEED)/." "$(DIST_WIN)/MappaDati/"
	cp tools/LEGGIMI-WINDOWS.txt "$(DIST_WIN)/LEGGIMI.txt"
	find "$(DIST_WIN)" -name .DS_Store -delete
	@echo ""
	@echo "Pronta: $(DIST_WIN)"
	@echo "Contiene il programma, i dati del progetto (seed/MappaDati) e le istruzioni."

chiavetta:
	@test -n "$(DEST)" || { echo "Indica la chiavetta, per esempio: make chiavetta DEST=/Volumes/MUSICA"; exit 1; }
	@test -d "$(DEST)" || { echo "Non trovo $(DEST): la chiavetta e' collegata? Il nome e' quello che vedi nel Finder."; exit 1; }
	@test -d "$(BIN)/$(APP).app" -a -f "$(BIN)/$(APP).exe" || { echo "Prima crea l'app con: make app"; exit 1; }
	mkdir -p "$(DEST)/$(APP)/MappaDati"
	rm -rf "$(DEST)/$(APP)/$(APP).app"
	cp -R "$(BIN)/$(APP).app" "$(DEST)/$(APP)/"
	cp "$(BIN)/$(APP).exe" "$(DEST)/$(APP)/"
	@if [ -f "$(DEST)/$(APP)/MappaDati/mappa.db" ]; then \
		echo "Dati gia' presenti sulla chiavetta: li lascio come sono."; \
	else \
		cp -R "$(SEED)/." "$(DEST)/$(APP)/MappaDati/"; \
		echo "Copiati i dati iniziali (regioni, testi, progressi di 2A e 2B, brani e immagini in MappaDati/asset)."; \
	fi
	@echo "Fatto. Espelli la chiavetta dal Finder prima di toglierla."

installa-mac:
	@test -d "$(BIN)/$(APP).app" || { echo "Prima crea l'app con: make app"; exit 1; }
	rm -rf "/Applications/$(APP).app"
	cp -R "$(BIN)/$(APP).app" /Applications/
	mkdir -p "$(MAC_DATI)"
	@if [ -f "$(MAC_DATI)/mappa.db" ]; then echo "Dati gia' presenti sul Mac: li lascio come sono."; \
	else cp -R "$(SEED)/." "$(MAC_DATI)/"; echo "Copiati i dati iniziali in $(MAC_DATI)"; fi

prova:
	@mkdir -p dati-prova
	@test -f dati-prova/mappa.db || cp -R "$(SEED)/." dati-prova/
	MAPPA_DATI="$(CURDIR)/dati-prova" go run -tags devserver .

pulisci:
	rm -rf "$(BIN)"
