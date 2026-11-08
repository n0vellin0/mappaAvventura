/* Ponte tra la pagina della Mappa e l'app sul computer.
   La pagina e' nata su claude.ai e chiede i suoi dati a window.claude.use("db" | "assets" | "downloads").
   Qui quelle stesse funzioni parlano con il programma Go, che salva tutto nel database SQLite.
   Lo stesso file funziona dentro la finestra dell'app e nel browser del telefono usato come telecomando. */
(() => {
"use strict";

const enc = encodeURIComponent;
const clone = v => v == null ? v : JSON.parse(JSON.stringify(v));

async function api(path, opts = {}){
  let res;
  try { res = await fetch(path, { cache: "no-store", ...opts }); }
  catch (e) { const err = new Error("rete"); err.code = "unavailable"; throw err; }
  if (!res.ok){
    const err = new Error("http " + res.status);
    err.code = res.status === 413 ? "too_large" : res.status === 415 ? "unsupported_type" : res.status === 400 ? "invalid_argument" : "unavailable";
    throw err;
  }
  return res;
}

/* ---------- informazioni sull'app ---------- */
let INFO = { desktop: false };
const infoReady = api("/api/info").then(r => r.json()).then(v => { INFO = v || INFO; return INFO; }).catch(() => INFO);

/* ---------- ascolto dei cambiamenti ----------
   Ogni collezione ha un numero di versione. Ogni mezzo secondo si chiede al programma quali sono cambiate
   e si rileggono solo quelle: cosi' la LIM vede subito cosa fai dal telefono, e viceversa. */
const listeners = new Set();
let pollTimer = null, polling = false;

async function poll(){
  if (polling) return; polling = true;
  try {
    const v = await (await api("/api/versions")).json();
    for (const l of listeners){
      const cur = v[l.col] || 0;
      if (cur !== l.ver){ l.ver = cur; l.fire(); }
    }
  } catch {}
  polling = false;
  clearTimeout(pollTimer); pollTimer = setTimeout(poll, 500);
}
const kick = () => { clearTimeout(pollTimer); pollTimer = setTimeout(poll, 30); };

function listen(col, fetcher, next, error){
  const l = { col, ver: undefined, seq: 0, done: 0 };
  l.fire = async () => {
    const my = ++l.seq;
    try { const snap = await fetcher(); if (my >= l.done && listeners.has(l)){ l.done = my; next(snap); } }
    catch (e) { if (error && listeners.has(l)) error(e); }
  };
  listeners.add(l);
  l.fire();
  if (!pollTimer) pollTimer = setTimeout(poll, 300);
  return () => listeners.delete(l);
}

const META = Object.freeze({ fromCache: false, hasPendingWrites: false });
function docSnap(id, v){
  const exists = !!(v && v.exists);
  const data = exists ? v.data : undefined;
  return { id, exists, metadata: META, data: () => clone(data) };
}
function colSnap(list){
  const docs = list.map(d => ({ id: d.id, exists: true, metadata: META, data: () => clone(d.data) }));
  return { docs, size: docs.length, empty: !docs.length, metadata: META, forEach: f => docs.forEach(f) };
}

function docRef(path){
  const parts = String(path).split("/");
  if (parts.length !== 2) throw new Error("percorso non valido: " + path);
  const [col, id] = parts;
  const url = "/api/doc/" + enc(col) + "/" + enc(id);
  const ref = {
    id, path,
    async get(){ return docSnap(id, await (await api(url)).json()); },
    async set(body){
      await api(url, { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body ?? {}) });
      kick();
    },
    async update(patch){
      const cur = await ref.get();
      if (!cur.exists){ const e = new Error("non trovato"); e.code = "not_found"; throw e; }
      await ref.set({ ...cur.data(), ...patch });
    },
    async delete(){ await api(url, { method: "DELETE" }); kick(); },
    onSnapshot(next, error){ return listen(col, async () => docSnap(id, await (await api(url)).json()), next, error); }
  };
  return ref;
}

const db = Object.freeze({
  doc: docRef,
  collection(col){
    return {
      id: col,
      doc: id => docRef(col + "/" + id),
      async get(){ return colSnap(await (await api("/api/col/" + enc(col))).json()); },
      onSnapshot(next, error){ return listen(col, async () => colSnap(await (await api("/api/col/" + enc(col))).json()), next, error); }
    };
  }
});

/* ---------- brani e immagini ---------- */
const assets = Object.freeze({
  async upload(blob, opts){
    const type = (opts && opts.type) || blob.type || "application/octet-stream";
    const res = await api("/api/assets", { method: "POST", headers: { "Content-Type": type }, body: blob });
    return res.json();
  },
  async delete(id){ await api("/api/assets/" + enc(id), { method: "DELETE" }); },
  async list(){ return (await api("/api/assets")).json(); }
});

/* ---------- salvataggio di file (copia di sicurezza) ---------- */
const downloads = Object.freeze({
  async save({ filename, data }){
    const blob = data instanceof Blob ? data : new Blob([data]);
    await infoReady;
    if (INFO.desktop){
      const v = await (await api("/api/desktop/save?name=" + enc(filename || "file"), { method: "POST", body: blob })).json();
      if (!v.saved){ const e = new Error("annullato"); e.code = "cancelled"; throw e; }
      return;
    }
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob); a.download = filename || "file";
    document.body.appendChild(a); a.click(); a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 10000);
  }
});

window.claude = Object.freeze({
  use(name){
    return Promise.resolve(name === "db" ? db : name === "assets" ? assets : name === "downloads" ? downloads : null);
  }
});

/* ---------- solo nella finestra dell'app ---------- */
const isDesktop = () => INFO.desktop === true;

// Scelta dei file con la finestra del sistema (piu' affidabile dentro l'app, soprattutto sul Mac).
const nativeClick = HTMLInputElement.prototype.click;
HTMLInputElement.prototype.click = function(){
  if (this.type === "file" && isDesktop()){ pickFile(this); return; }
  return nativeClick.call(this);
};
async function pickFile(input){
  try {
    const v = await (await api("/api/desktop/pick?accept=" + enc(input.accept || ""), { method: "POST" })).json();
    if (!v || !v.token) return;
    const blob = await (await api("/api/desktop/picked/" + enc(v.token))).blob();
    const file = new File([blob], v.name, { type: v.type || blob.type });
    const dt = new DataTransfer(); dt.items.add(file);
    input.files = dt.files;
    input.dispatchEvent(new Event("change", { bubbles: true }));
  } catch (e) { console.error("Scelta del file non riuscita", e); }
}

// Schermo intero: dentro l'app e' la finestra a diventare intera.
let fullOn = false;
const rt = () => window.runtime;
const nativeReq = Element.prototype.requestFullscreen, nativeExit = document.exitFullscreen;
const nativeFE = Object.getOwnPropertyDescriptor(Document.prototype, "fullscreenElement");
Element.prototype.requestFullscreen = function(...a){
  if (isDesktop() && rt()){ fullOn = true; rt().WindowFullscreen(); document.dispatchEvent(new Event("fullscreenchange")); return Promise.resolve(); }
  return nativeReq ? nativeReq.apply(this, a) : Promise.reject(new Error("non disponibile"));
};
document.exitFullscreen = function(){
  if (isDesktop() && rt()){ fullOn = false; rt().WindowUnfullscreen(); document.dispatchEvent(new Event("fullscreenchange")); return Promise.resolve(); }
  return nativeExit ? nativeExit.call(document) : Promise.resolve();
};
Object.defineProperty(document, "fullscreenElement", {
  configurable: true,
  get(){ return (isDesktop() && rt()) ? (fullOn ? document.documentElement : null) : (nativeFE ? nativeFE.get.call(document) : null); }
});

// Sezione "App sul computer" nella Regia > Classi (la pagina chiama questa funzione).
const escH = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
window.__mappaDesktop = function(remote){
  if (!isDesktop() || remote) return "";
  const urls = INFO.remoteURLs || [];
  const phone = urls.length
    ? `<p class="note">Telefono come telecomando: collegalo alla stessa Wi-Fi del computer e inquadra il codice con la fotocamera.</p>${urls.map((u, i) => `<div style="display:flex;gap:12px;align-items:center"><img src="/api/desktop/qr/${i}?t=${Date.now() >> 16}" alt="Codice QR del telecomando" width="132" height="132" style="background:#fff;border-radius:8px;padding:6px;flex:none"><p class="note" style="user-select:all;-webkit-user-select:all;overflow-wrap:anywhere;color:var(--parchment)">${escH(u)}</p></div>`).join("")}`
    : `<p class="note">Il computer non e' collegato a una rete: il telefono come telecomando non e' disponibile.</p>`;
  return `<div class="sec"><div class="sec-t">App sul computer <span class="status">versione ${escH(INFO.version || "")}</span></div>
    <p class="note">Con lo schermo esteso (Win + P → Estendi) metti la mappa sulla LIM e apri la Regia sullo schermo del computer.</p>
    <div class="flexrow"><button class="btn" data-desk="regia">Apri la Regia nel browser</button><button class="btn ghost" data-desk="data">Apri la cartella dei dati</button></div>
    ${phone}
    <p class="note">Dati salvati in: <span style="overflow-wrap:anywhere">${escH(INFO.dataDir || "")}</span>${INFO.portable ? " (chiavetta)" : ""}</p></div>`;
};
document.addEventListener("click", async e => {
  const b = e.target.closest && e.target.closest("[data-desk]");
  if (!b) return;
  e.preventDefault(); e.stopPropagation();
  const what = b.dataset.desk === "regia" ? "open-regia" : "open-data";
  try { await api("/api/desktop/" + what, { method: "POST" }); } catch (err) { console.error(err); }
}, true);

})();
