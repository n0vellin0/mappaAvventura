#!/usr/bin/env python3
"""Trasforma la pagina della Mappa pubblicata su claude.ai nel frontend dell'app.

Uso (lo esegue Claude quando aggiorna la mappa, non serve lanciarlo a mano):
    python3 tools/prepara_frontend.py pagina-artifact.html frontend/dist/index.html

Cambia solo tre cose della pagina:
  1. i font di Google diventano font locali (fonts/fonts.css), cosi' funziona offline;
  2. carica shim.js, che sostituisce l'archivio di claude.ai con il database SQLite dell'app;
  3. aggiunge nella Regia > Classi la sezione "App sul computer".
"""
import sys

src, dst = sys.argv[1], sys.argv[2]
s = open(src, encoding="utf-8").read()

def rep(old, new):
    global s
    n = s.count(old)
    if n != 1:
        sys.exit(f"Pezzo atteso una volta, trovato {n} volte: {old[:70]!r}")
    s = s.replace(old, new)

start = s.index('<link rel="preconnect" href="https://fonts.googleapis.com">')
end = s.index('>', s.index('<link rel="stylesheet" href="https://fonts.googleapis.com')) + 1
s = s[:start] + '<link rel="stylesheet" href="fonts/fonts.css">' + s[end:]

rep('<script>\n(() => {\n"use strict";', '<script src="shim.js"></script>\n<script>\n(() => {\n"use strict";')

line_start = s.index('    h+=`<div class="sec"><div class="sec-t">Questo dispositivo</div>')
line_end = s.index("\n", line_start) + 1
s = s[:line_end] + '    if(window.__mappaDesktop) h+=window.__mappaDesktop(REMOTE);\n' + s[line_end:]

if "fonts.googleapis.com" in s:
    sys.exit("Sono rimasti riferimenti ai font di Google")
open(dst, "w", encoding="utf-8").write(s)
print("Frontend pronto:", dst)
