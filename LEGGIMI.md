# Mappa dell'Avventura: app per Mac e Windows

È la stessa mappa che usavi su claude.ai, trasformata in un programma che funziona senza account e senza internet.
Regioni, testi, brani, immagini e progressi di 2A e 2B sono già dentro, nella cartella `seed/MappaDati`.

- **Programma:** Wails (Go), con la pagina della mappa dentro una finestra.
- **Dati:** la cartella `MappaDati`, separata dal programma:
  - `mappa.db` (SQLite) contiene testi, regioni, classi e progressi;
  - `asset/` contiene i file normali, divisi per tipologia: `asset/brani/` (`.m4a`, `.wav`, `.mp3`) e `asset/immagini/` (`.jpg`, `.png`).
- **Compilare non tocca i dati.** `make app` rifà solo il programma: brani e immagini restano dove sono e non vanno mai reimportati.
- **Telecomando:** il telefono, collegato alla stessa Wi-Fi, oppure la Regia nel browser sullo schermo del computer.

---

## 1. Installare gli strumenti (solo la prima volta, sul Mac)

Apri **Terminale** (Applicazioni → Utility → Terminale) e incolla i comandi uno alla volta.

**Homebrew.** Ti chiede la password del Mac e di premere Invio. Installa anche gli strumenti di sviluppo Apple, e ci mette qualche minuto.

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

**Aggiungi Homebrew e Go al Terminale.** Copia le due righe così come sono:

```bash
echo 'eval "$(/opt/homebrew/bin/brew shellenv 2>/dev/null || /usr/local/bin/brew shellenv)"' >> ~/.zprofile
echo 'export PATH="$PATH:$HOME/go/bin"' >> ~/.zprofile
```

Poi **chiudi il Terminale e riaprilo**.

**Go e Wails.** Dalla cartella del progetto:

```bash
cd ~/Documents/MappaAvventura
make setup
```

Se compare una finestra che chiede di installare gli "strumenti per sviluppatori", accetta, aspetta che finisca e rilancia `make setup`.

Per controllare che sia tutto a posto: `make controlla`. Le voci sul pacchetto `npm` puoi ignorarle, perché qui non serve.

---

## 2. Creare l'app

```bash
cd ~/Documents/MappaAvventura
make app
```

Dopo qualche minuto, in `build/bin/` trovi:

- `MappaAvventura.app`, per il Mac;
- `MappaAvventura.exe`, per Windows.

---

## 2b. Modalità sviluppo: lavorare direttamente sulla sorgente

```bash
make dev
```

Crea l'app per il Mac e la apre usando come dati la cartella del progetto `seed/MappaDati`.
Se l'app è già pronta e vuoi solo aprirla, senza ricompilare, usa `make avvia`: è più veloce.
Con questi due comandi non serve installare niente in Applicazioni.
Tutto quello che fai in questa modalità finisce nella sorgente: brani e immagini caricati, testi, regioni, pianeti.
Ogni nuova chiavetta creata con `make chiavetta` partirà da lì.

- **Anche i progressi delle classi vengono salvati nella sorgente**: lanterne, zone liberate, zaino. Se in modalità sviluppo fai delle prove, alla fine riporta le classi come devono essere, per esempio con **Regia → Classi → Ricomincia**.
- Per uscire chiudi la finestra della mappa: il Terminale torna libero.
- `make chiavetta` copia i dati sulla chiavetta **solo se la chiavetta non li ha già**. Una chiavetta già in uso tiene i suoi dati e i suoi progressi.

## 3. Usarla con la chiavetta (consigliato)

Così Mac e PC della LIM lavorano sugli **stessi dati**: a casa prepari testi e brani, a scuola li trovi già pronti.

```bash
make chiavetta DEST=/Volumes/NOME_DELLA_CHIAVETTA
```

Il nome della chiavetta è quello che vedi nel Finder. Sulla chiavetta compare questa cartella:

```
MappaAvventura/
  MappaAvventura.app   ← sul Mac, doppio clic
  MappaAvventura.exe   ← sul PC della LIM, doppio clic
  MappaDati/
    mappa.db           ← testi, regioni, classi, progressi
    asset/
      brani/           ← brani (.m4a, .wav, .mp3)
      immagini/        ← ritratti, sfondi, pergamene
```

Il programma usa i dati della cartella `MappaDati` che trova accanto a sé.

**Regole d'oro**

- **Chiudi la mappa prima di togliere la chiavetta.** Sul Mac espellila dal Finder, su Windows con "Rimozione sicura". Un database tolto mentre è aperto si può rovinare.
- **Ogni tanto copia tutta la cartella `MappaDati`** sul Mac. È tutto il lavoro di un anno.
- **I brani nuovi si caricano dalla Regia**, come prima. Nelle cartelle `asset/brani` e `asset/immagini` i file hanno un nome in codice, che è quello con cui la mappa li ritrova: non rinominarli e non aggiungerli a mano.

**Prima apertura**

- **Sul Mac:** se chiede il permesso di accedere ai file su un volume rimovibile, rispondi **OK**.
- **Su Windows:**
  - se compare "Windows ha protetto il PC", clicca **Ulteriori informazioni** e poi **Esegui comunque**;
  - se il firewall chiede il permesso per le reti, scegli **Consenti** se vuoi usare il telefono come telecomando. Se non hai i permessi, scegli **Annulla**: la mappa funziona lo stesso, ti resta solo la Regia nel browser;
  - Windows 10 e 11 hanno già quello che serve per la finestra (WebView2). Se manca, il programma propone di scaricarlo.

### In alternativa: solo sul Mac

```bash
make installa-mac
```

Mette l'app in Applicazioni e i dati in `~/Library/Application Support/MappaAvventura/`, con dentro `mappa.db` e la cartella `asset`.
In questo modo il Mac ha dati suoi, separati da quelli della chiavetta.

---

## 4. In classe

- **Schermo intero:** tasto **F**, oppure il pulsante in basso a destra.
- **Regia nascosta:** se il PC ha uno schermo suo, premi **Win + P → Estendi**. La mappa va sulla LIM. Poi in **Regia → Classi → App sul computer** premi **Apri la Regia nel browser** e sposta quella finestra sullo schermo del PC.
- **Telefono come telecomando:** in **Regia → Classi → App sul computer** c'è un codice QR. Collega il telefono alla stessa Wi-Fi del computer e inquadra il codice con la fotocamera. Il collegamento contiene un codice segreto: senza quello, nessun altro sulla rete può aprire la mappa.
  - Se la Wi-Fi della scuola non fa comunicare i dispositivi, il telefono non si collega. In quel caso usa il secondo schermo.
- La prima volta che fai partire un brano, tocca una volta la mappa: serve per attivare l'audio, come prima.

---

## 5. Aggiornamenti

Quando Claude modifica qualcosa nel progetto, aggiorna i file in questa cartella. Tu poi lanci:

```bash
cd ~/Documents/MappaAvventura
make app
make chiavetta DEST=/Volumes/NOME_DELLA_CHIAVETTA
```

`make chiavetta` sostituisce solo i programmi: **i dati sulla chiavetta, brani compresi, non vengono toccati**.
Il numero di versione lo vedi in **Regia → Classi → App sul computer**.

---

## Se qualcosa non va

- **La mappa non parte:** nella cartella dei dati può comparire `errore-avvio.txt`, che spiega il motivo.
- **Controllare la mappa senza l'app:** con `make prova` si apre su http://127.0.0.1:8080 nel browser, con una copia dei dati nella cartella `dati-prova`.
- **Copia di sicurezza:** il pulsante **Scarica copia** nella Regia salva testi e progressi, senza i brani. Per una copia completa copia tutta la cartella `MappaDati`.

## Com'è fatto (per chi mette le mani nel codice)

| File | Cosa fa |
|---|---|
| `main.go` | Avvia la finestra Wails e il server per il telecomando |
| `internal/store` | Il database SQLite (documenti, elenco dei file, impostazioni interne) e la cartella `asset/` con brani e immagini |
| `internal/server` | Le richieste della pagina (`/api/...`, `/_blob/...`), le finestre per scegliere e salvare i file, il codice QR |
| `internal/paths` | Decide dove stanno i dati: cartella `MappaDati` accanto al programma, altrimenti la cartella dell'utente |
| `frontend/dist/index.html` | La pagina della mappa, presa da claude.ai con `tools/prepara_frontend.py` |
| `frontend/dist/shim.js` | Il ponte: la pagina chiede i dati come su claude.ai e li riceve dal database dell'app |
| `seed/MappaDati` | I dati di partenza: la mappa com'era su claude.ai l'8 novembre 2026 |
