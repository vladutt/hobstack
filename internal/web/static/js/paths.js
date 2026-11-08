import { api } from "./api.js";
import { toast } from "./ui.js";
import { revealLabel } from "./open-file.js";

// Caile din mesajele de chat devin clicabile: clic = fisierul in Finder / Explorer (un folder se deschide).
// Se marcheaza intai candidatii (text care arata a cale), apoi serverul spune care exista in proiect: doar
// aceia devin link. Blocurile de cod (```), linkurile si butoanele raman neatinse.

const TOKEN = /[^\s"'`<>()[\]{},;|]+/g;
// cale cu "/" (sau "\" pe Windows), optional :linie(:coloana)
const WITH_DIR = /^(~[\\/]|[\\/]|\.{1,2}[\\/]|[A-Za-z]:\\)?([\w.@+-]+[\\/])+[\w.@+-]*[\w@+-](:\d+(:\d+)?)?$/;
// doar un nume de fisier cu extensie (Checkout.tsx, README.md:12)
const BARE_FILE = /^[\w@+-][\w.@+-]*\.[A-Za-z][A-Za-z0-9]{1,7}(:\d+(:\d+)?)?$/;
// ~/.zshrc, ./build.sh: un singur nume dupa ~/ sau ./
const PREFIXED = /^(~|\.{1,2})[\\/][\w.@+-]+(:\d+(:\d+)?)?$/;
// fisiere ascunse (.env.example, .gitignore) si foldere scrise cu / la final (src/, web/public/)
const DOTFILE = /^\.[\w-][\w.@+-]*$/;
const DIR = /^([\w.@+-]+[\\/])+$/;

export function looksLikePath(t) {
  if (t.includes("://") || t.length < 3 || t.length > 400) return false;
  return WITH_DIR.test(t) || BARE_FILE.test(t) || PREFIXED.test(t) || DOTFILE.test(t) || DIR.test(t);
}

// clean: scoate punctuatia de la final ("App.tsx." -> "App.tsx", "src/:" -> "src/") si @ din fata
function clean(t) {
  return t.replace(/^@/, "").replace(/[.,:;!?*]+$/, "");
}

// markPaths: invaleste candidatii din root in <span class="path-cand" data-path>; intoarce span-urile
export function markPaths(root) {
  const out = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode: (n) => (n.parentElement?.closest("pre, a, button, .md-pre, .path-cand, .msg-meta")
      ? NodeFilter.FILTER_REJECT : NodeFilter.FILTER_ACCEPT),
  });
  const texts = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) texts.push(n);
  for (const node of texts) {
    const text = node.textContent;
    const parts = [];
    let last = 0;
    for (const m of text.matchAll(TOKEN)) {
      const raw = m[0];
      const path = clean(raw);
      if (!looksLikePath(path)) continue;
      const start = m.index + raw.indexOf(path);
      parts.push(document.createTextNode(text.slice(last, start)));
      const span = document.createElement("span");
      span.className = "path-cand";
      span.dataset.path = path;
      span.textContent = path;
      parts.push(span);
      out.push(span);
      last = start + path.length;
    }
    if (!parts.length) continue;
    parts.push(document.createTextNode(text.slice(last)));
    node.replaceWith(...parts);
  }
  return out;
}

// pathLinker: tine minte ce a raspuns serverul (per chat) si strange cererile din mesajele adaugate deodata
export function pathLinker(projectId, agentId) {
  const known = new Map(); // cale -> { dir } | null (nu exista)
  let pending = [];
  let timer = null;

  function activate(span, info) {
    span.classList.add("path-link");
    span.tabIndex = 0;
    span.setAttribute("role", "button");
    span.title = info.dir ? "Open this folder" : revealLabel();
    const go = async (e) => {
      e.preventDefault();
      e.stopPropagation();
      try {
        await api(`/api/projects/${projectId}/reveal`, { method: "POST", body: { path: span.dataset.path, agent: agentId() } });
      } catch (err) { toast(err.message); }
    };
    span.addEventListener("click", go);
    span.addEventListener("keydown", (e) => { if (e.key === "Enter") go(e); });
  }

  async function flush() {
    timer = null;
    const spans = pending;
    pending = [];
    const ask = [...new Set(spans.map((s) => s.dataset.path).filter((p) => !known.has(p)))];
    if (ask.length) {
      try {
        const res = await api(`/api/projects/${projectId}/paths`, { method: "POST", body: { paths: ask, agent: agentId() } });
        for (const p of ask) known.set(p, res.found?.[p] || null);
      } catch { return; } // fara link-uri; textul ramane
    }
    for (const s of spans) {
      const info = known.get(s.dataset.path);
      if (info && s.isConnected) activate(s, info);
    }
  }

  return {
    // link(node): cauta caile in nodul unui mesaj
    link(node) {
      const spans = markPaths(node);
      if (!spans.length) return;
      pending.push(...spans);
      if (!timer) timer = setTimeout(flush, 30);
    },
    reset() { known.clear(); },
  };
}
