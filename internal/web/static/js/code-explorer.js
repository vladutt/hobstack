import { api } from "./api.js";
import { el } from "./ui.js";
import { languageFor } from "./highlight.js";
import { codeWithMarkers, copyText } from "./file-view.js";

const isMac = /Mac|iPhone|iPad/.test(navigator.platform);
export const shortcutLabel = isMac ? "⌘P" : "Ctrl+P";
// modKey: Cmd pe macOS, Ctrl in rest (pe macOS Ctrl+P / Ctrl+C raman ale campurilor de text)
export const modKey = (e) => (isMac ? e.metaKey && !e.ctrlKey : e.ctrlKey && !e.metaKey);

// searchFiles: cautarea dupa nume, in ordinea din codefiles.SearchIdx: numele incepe cu q, numele
// contine q, calea contine q; la egalitate numele mai scurt, apoi calea. items: [{ folder, path }].
export function searchFiles(items, q, limit) {
  q = q.trim().toLowerCase();
  if (!q) return [];
  const hits = [];
  for (const it of items) {
    const lp = it.path.toLowerCase();
    const name = lp.slice(lp.lastIndexOf("/") + 1);
    const tier = name.startsWith(q) ? 0 : name.includes(q) ? 1 : lp.includes(q) ? 2 : -1;
    if (tier >= 0) hits.push({ it, tier, n: name.length, lp });
  }
  hits.sort((a, b) => a.tier - b.tier || a.n - b.n || (a.lp < b.lp ? -1 : a.lp > b.lp ? 1 : 0));
  return hits.slice(0, limit).map((h) => h.it);
}

// absPath: calea absoluta, cu separatorul folderului proiectului
function absPath(folder, rel) {
  const sep = folder.path.includes("\\") && !folder.path.includes("/") ? "\\" : "/";
  return folder.path.replace(/[\\/]+$/, "") + sep + rel.split("/").join(sep);
}

// openCodeExplorer: explorer-ul de cod al proiectului, doar citire (Cmd/Ctrl+P).
// - defaultAgentId: agentul pentru "Ask" (cel din panou sau radacina)
// - onAsk(agentId, text): deschide chat-ul agentului cu textul la inceputul campului
// - onClose(): dupa inchidere
// Intoarce { close() }.
export function openCodeExplorer({ projectId, project, defaultAgentId, onAsk, onClose }) {
  const storeKey = `hobstack-code-${projectId}`;
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem(storeKey) || "{}") || {}; } catch {}
  const openDirs = new Set(Array.isArray(saved.open) ? saved.open : []); // "<folderId>/<cale>" ("<id>/" = radacina)
  let folders = [];        // raspunsul /tree
  let all = [];            // [{ folder, path }] pentru cautare
  let current = null;      // { folder, path } deschis
  let sel = null;          // { from, to } liniile selectate
  let anchorLine = null;   // linia de la care pleaca Shift+clic
  let agentId = defaultAgentId;
  let seq = 0;             // ultima cerere de fisier
  let results = [];        // rezultatele cautarii
  let active = 0;          // rezultatul ales
  let closed = false;

  const agents = (project.agents || []).filter((a) => !a.detached);
  const agentName = (id) => agents.find((a) => a.id === id)?.name || "agent";

  function persist() {
    try { localStorage.setItem(storeKey, JSON.stringify({ open: [...openDirs], last: current })); } catch {}
  }

  // ---------- caile pentru copiere si chat ----------
  const folderOf = (id) => folders.find((f) => f.id === id);
  // copyPath: relativa in folder; cu mai multe foldere, prefixata cu numele folderului
  const copyPath = (c) => (folders.length > 1 ? `${folderOf(c.folder).name}/${c.path}` : c.path);
  // chatPath: ca @ in chat: relativa in folderul agentului, absoluta altfel
  function chatPath(c, aid) {
    const a = agents.find((x) => x.id === aid);
    const home = a?.folderId ?? folders[0]?.id;
    return c.folder === home ? c.path : absPath(folderOf(c.folder), c.path);
  }
  const lines = () => (sel ? (sel.from === sel.to ? `:${sel.from}` : `:${sel.from}-${sel.to}`) : "");

  // ---------- DOM ----------
  const search = el("input", { type: "search", class: "cx-search", placeholder: "Go to file…", "aria-label": "Go to file",
    autocomplete: "off", spellcheck: "false" });
  const resultsBox = el("ul", { class: "cx-results", role: "listbox", hidden: true });
  const tree = el("nav", { class: "cx-tree", "aria-label": "Project files" }, el("p", { class: "hint" }, "Loading..."));
  const pathBox = el("span", { class: "change-path" });
  const menuBox = el("div", { class: "cx-menu", hidden: true });
  const moreBtn = el("button", { type: "button", class: "btn small", "aria-label": "More", hidden: true,
    onclick: (e) => { e.stopPropagation(); toggleMenu([["Copy absolute path", () => copyText(absPath(folderOf(current.folder), current.path))]]); } }, "⋯");
  const copyBtn = el("button", { type: "button", class: "btn small", hidden: true, onclick: () => copyText(copyPath(current)) }, "Copy path");
  const codeBox = el("div", { class: "cx-code" }, el("p", { class: "hint cx-empty" }, `Pick a file on the left or search with ${shortcutLabel}.`));
  const bar = el("div", { class: "cx-bar", hidden: true });
  const closeBtn = el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: () => close() }, "×");
  const box = el("div", { class: "modal cx", role: "dialog", "aria-modal": "true", "aria-label": "Code explorer" },
    el("div", { class: "cx-top" }, el("div", { class: "cx-search-wrap" }, search, resultsBox), closeBtn),
    el("div", { class: "cx-main" }, tree,
      el("div", { class: "cx-view" },
        el("div", { class: "fv-head cx-head" }, pathBox, el("span", { class: "fv-actions" }, copyBtn, moreBtn), menuBox),
        codeBox, bar)));
  const overlay = el("div", { class: "modal-overlay cx-overlay", onclick: (e) => { if (e.target === overlay) close(); } }, box);
  const before = document.activeElement;

  // ---------- meniul mic (⋯ si alegerea agentului) ----------
  function toggleMenu(items, anchor) {
    if (!menuBox.hidden) { menuBox.hidden = true; return; }
    menuBox.replaceChildren(...items.map(([label, fn]) => el("button", { type: "button", class: "filter-item",
      onclick: () => { menuBox.hidden = true; fn(); } }, label)));
    menuBox.classList.toggle("cx-menu-bar", !!anchor);
    if (anchor) { bar.append(menuBox); } else { box.querySelector(".cx-head").append(menuBox); }
    menuBox.hidden = false;
  }

  // ---------- arborele ----------
  // buildNode: { dirs: Map(nume -> nod), files: [nume] } din lista de cai
  function buildNode(files) {
    const root = { dirs: new Map(), files: [] };
    for (const p of files) {
      const parts = p.split("/");
      let n = root;
      for (const d of parts.slice(0, -1)) {
        if (!n.dirs.has(d)) n.dirs.set(d, { dirs: new Map(), files: [] });
        n = n.dirs.get(d);
      }
      n.files.push(parts[parts.length - 1]);
    }
    return root;
  }

  // toggleDir: desfasoara / restrange un folder si pastreaza focusul pe butonul lui (dupa cheie)
  function toggleDir(key) {
    if (openDirs.has(key)) openDirs.delete(key); else openDirs.add(key);
    persist();
    renderTree();
    tree.querySelector(`.cx-dir[data-key="${CSS.escape(key)}"]`)?.focus();
  }
  const fileKey = (c) => `${c.folder}:${c.path}`;

  // renderDir: randurile unui folder desfasurat; subfolderele se deseneaza doar daca sunt deschise.
  // Numele sortate se tin pe nod (sortarea se face o data, nu la fiecare desenare).
  function renderDir(f, node, dir, depth, out) {
    const pad = `${10 + depth * 14}px`;
    node.dirNames ||= [...node.dirs.keys()].sort((a, b) => a.localeCompare(b));
    node.fileNames ||= [...node.files].sort((a, b) => a.localeCompare(b));
    for (const name of node.dirNames) {
      const path = dir + name + "/";
      const key = `${f.id}/${path}`;
      const open = openDirs.has(key);
      out.push(el("button", { type: "button", class: "cx-dir", style: `padding-left:${pad}`, "aria-expanded": String(open),
        "data-key": key, onclick: () => toggleDir(key) },
        el("span", { class: "cx-caret" }, open ? "▾" : "▸"), name));
      if (open) renderDir(f, node.dirs.get(name), path, depth + 1, out);
    }
    for (const name of node.fileNames) {
      const path = dir + name;
      const on = current && current.folder === f.id && current.path === path;
      out.push(el("button", { type: "button", class: "cx-file" + (on ? " on" : ""), style: `padding-left:${pad}`, title: path,
        "data-key": fileKey({ folder: f.id, path }), "aria-current": on ? "true" : null,
        onclick: () => openFile({ folder: f.id, path }, false) }, name));
    }
  }

  // markCurrent: doar evidentierea fisierului deschis (fara redesenare: focusul din arbore ramane)
  function markCurrent() {
    const k = current ? fileKey(current) : "";
    for (const b of tree.querySelectorAll(".cx-file")) {
      const on = b.dataset.key === k;
      b.classList.toggle("on", on);
      if (on) b.setAttribute("aria-current", "true"); else b.removeAttribute("aria-current");
    }
  }

  function renderTree() {
    const out = [];
    for (const f of folders) {
      const key = `${f.id}/`;
      const open = openDirs.has(key);
      out.push(el("button", { type: "button", class: "cx-dir cx-root", "aria-expanded": String(open),
        "data-key": key, onclick: () => toggleDir(key) },
        el("span", { class: "cx-caret" }, open ? "▾" : "▸"), f.name));
      if (!open) continue;
      if (f.error) { out.push(el("p", { class: "hint cx-note" }, f.error)); continue; }
      f.node ||= buildNode(f.files);
      renderDir(f, f.node, "", 1, out);
      if (f.truncated) out.push(el("p", { class: "hint cx-note" }, `Only the first ${f.files.length} files are listed; use search.`));
    }
    tree.replaceChildren(...out);
  }

  // reveal: desfasoara folderele parinte ale fisierului (redesenare doar daca s-a deschis ceva) si il aduce in zona vizibila
  function reveal(c) {
    const parts = c.path.split("/");
    const need = [`${c.folder}/`];
    for (let i = 1; i < parts.length; i++) need.push(`${c.folder}/${parts.slice(0, i).join("/")}/`);
    const missing = need.filter((k) => !openDirs.has(k));
    if (missing.length) {
      for (const k of missing) openDirs.add(k);
      persist();
      renderTree();
    } else {
      markCurrent();
    }
    tree.querySelector(".cx-file.on")?.scrollIntoView({ block: "nearest" });
  }

  async function loadTree() {
    try {
      const res = await api(`/api/projects/${projectId}/tree`);
      if (closed) return;
      folders = res.folders || [];
      all = folders.flatMap((f) => f.files.map((path) => ({ folder: f.id, path })));
      search.disabled = false;
      // ce s-a tastat cat se incarca lista se cauta acum
      if (search.value.trim()) { results = searchFiles(all, search.value, 50); active = 0; if (document.activeElement === search) renderResults(); }
      // la prima deschidere: radacina primului folder desfasurata
      if (!openDirs.size && folders[0]) openDirs.add(`${folders[0].id}/`);
      renderTree();
    } catch (err) {
      if (closed) return;
      search.disabled = true;
      tree.replaceChildren(el("p", { class: "hint cx-note" }, err.message),
        el("button", { type: "button", class: "btn small", onclick: () => { tree.replaceChildren(el("p", { class: "hint" }, "Loading...")); loadTree(); } }, "Retry"));
    }
  }

  // ---------- fisierul ----------
  async function openFile(c, doReveal = true) {
    current = { folder: c.folder, path: c.path };
    sel = null;
    anchorLine = null;
    bar.hidden = true;
    persist();
    if (doReveal) reveal(current); else markCurrent();
    const f = folderOf(c.folder);
    const cut = c.path.lastIndexOf("/") + 1;
    pathBox.replaceChildren(el("span", { class: "change-dir" }, (folders.length > 1 ? f.name + "/" : "") + c.path.slice(0, cut)),
      el("span", { class: "change-name" }, c.path.slice(cut)));
    copyBtn.hidden = moreBtn.hidden = false;
    codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "Loading..."));
    const my = ++seq;
    try {
      const q = new URLSearchParams({ folder: String(c.folder), path: c.path });
      const v = await api(`/api/projects/${projectId}/code-file?${q}`);
      if (my !== seq || closed) return;
      if (v.notRegular) codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "Not a regular file (a folder or a git submodule): no preview."));
      else if (v.binary) codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "Binary file: no preview."));
      else if (v.tooLarge) codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "The file is larger than 1 MB: no preview."));
      else codeBox.replaceChildren(codeWithMarkers(v.content, languageFor(c.path), null));
      codeBox.scrollTop = 0;
      codeBox.scrollLeft = 0;
    } catch (err) {
      if (my !== seq || closed) return;
      if (err.status === 404) {
        codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, "The file no longer exists."));
        loadTree(); // lista poate fi veche de pana la 30 s
      } else {
        codeBox.replaceChildren(el("p", { class: "hint cx-empty" }, err.message));
      }
    }
  }

  // ---------- selectia de linii ----------
  function paintSelection() {
    const rows = codeBox.querySelectorAll(".fv-line");
    rows.forEach((row, i) => row.classList.toggle("cx-sel", !!sel && i + 1 >= sel.from && i + 1 <= sel.to));
    if (!sel) { bar.hidden = true; return; }
    const askBtn = el("button", { type: "button", class: "btn small primary", onclick: () => ask(agentId) }, `Ask ${agentName(agentId)}`);
    const pick = agents.length > 1 ? el("button", { type: "button", class: "btn small primary cx-pick", "aria-label": "Ask another agent",
      onclick: (e) => { e.stopPropagation(); toggleMenu(agents.filter((a) => a.id !== agentId).map((a) => [`Ask ${a.name}`, () => { agentId = a.id; ask(a.id); }]), true); } }, "▾") : null;
    bar.replaceChildren(el("button", { type: "button", class: "btn small", onclick: () => copyText(copyPath(current) + lines()) }, "Copy reference"),
      el("span", { class: "cx-ask" }, askBtn, pick));
    bar.hidden = false;
  }

  codeBox.addEventListener("click", (e) => {
    const num = e.target.closest(".fv-num");
    if (!num || !current) return;
    const n = Number(num.textContent);
    if (!n) return;
    if (e.shiftKey && anchorLine) sel = { from: Math.min(anchorLine, n), to: Math.max(anchorLine, n) };
    else if (sel && sel.from === n && sel.to === n) { sel = null; anchorLine = null; }
    else { sel = { from: n, to: n }; anchorLine = n; }
    window.getSelection()?.removeAllRanges(); // Shift+clic nu lasa text selectat
    paintSelection();
  });

  function ask(aid) {
    const text = `@${chatPath(current, aid)}${lines()} `;
    close();
    onAsk(aid, text);
  }

  // ---------- cautarea ----------
  function renderResults() {
    if (!results.length) {
      resultsBox.hidden = !search.value.trim();
      resultsBox.replaceChildren(el("li", { class: "mention-empty" }, "No matching file"));
      return;
    }
    resultsBox.replaceChildren(...results.map((r, i) => {
      const cut = r.path.lastIndexOf("/") + 1;
      const f = folderOf(r.folder);
      return el("li", { class: "mention-item" + (i === active ? " active" : ""), role: "option", "aria-selected": String(i === active),
        onmousedown: (e) => { e.preventDefault(); choose(i); } },
        el("span", { class: "mention-name" }, r.path.slice(cut)),
        el("span", { class: "mention-dir" }, (folders.length > 1 ? f.name + "/" : "") + r.path.slice(0, cut)));
    }));
    resultsBox.hidden = false;
    resultsBox.children[active]?.scrollIntoView({ block: "nearest" });
  }
  function choose(i) {
    const r = results[i];
    if (!r) return;
    search.value = "";
    results = [];
    resultsBox.hidden = true;
    openFile(r);
  }
  search.addEventListener("input", () => { results = searchFiles(all, search.value, 50); active = 0; renderResults(); });
  search.addEventListener("keydown", (e) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      if (!results.length) return;
      e.preventDefault();
      active = (active + (e.key === "ArrowDown" ? 1 : results.length - 1)) % results.length;
      renderResults();
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(active);
    }
  });
  search.addEventListener("blur", () => setTimeout(() => { resultsBox.hidden = true; }, 150));
  search.addEventListener("focus", () => { if (search.value.trim()) renderResults(); });

  // in arbore: sageata dreapta desfasoara un folder, stanga il restrange (Enter merge ca un clic pe buton)
  tree.addEventListener("keydown", (e) => {
    const b = e.target.closest(".cx-dir");
    if (!b) return;
    const open = b.getAttribute("aria-expanded") === "true";
    if ((e.key === "ArrowRight" && !open) || (e.key === "ArrowLeft" && open)) {
      e.preventDefault();
      toggleDir(b.dataset.key); // redeseneaza si pune focusul inapoi pe acelasi folder
    }
  });

  // ---------- tastatura ----------
  // pe window, in capture: ruleaza inaintea ascultatorilor de pe document (modala de fisier, modala de task);
  // tastele tratate aici se opresc cu stopImmediatePropagation
  function onKey(e) {
    const mod = modKey(e);
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopImmediatePropagation();
      if (!menuBox.hidden) menuBox.hidden = true;
      else if (document.activeElement === search && search.value) { search.value = ""; results = []; resultsBox.hidden = true; }
      else if (sel) { sel = null; anchorLine = null; paintSelection(); }
      else close();
    } else if (e.key === "Tab") {
      e.stopImmediatePropagation();
    } else if (mod && e.shiftKey && e.key.toLowerCase() === "c" && current) {
      e.preventDefault();
      e.stopImmediatePropagation();
      copyText(copyPath(current));
    } else if (mod && !e.shiftKey && e.key.toLowerCase() === "c" && current
      && !(document.activeElement === search && search.selectionStart !== search.selectionEnd)
      && !(window.getSelection()?.toString())) {
      e.preventDefault();
      e.stopImmediatePropagation();
      copyText(copyPath(current) + lines());
    }
  }
  const onDocClick = (e) => { if (!menuBox.hidden && !menuBox.contains(e.target)) menuBox.hidden = true; };

  function close() {
    if (closed) return;
    closed = true;
    overlay.remove();
    window.removeEventListener("keydown", onKey, true);
    document.removeEventListener("click", onDocClick);
    before?.focus?.();
    onClose?.();
  }

  window.addEventListener("keydown", onKey, true);
  document.addEventListener("click", onDocClick);
  document.body.append(overlay);
  search.focus(); // activ de la inceput: tastele nu ajung in campul de dedesubt cat se incarca lista
  loadTree().then(() => {
    // ultimul fisier deschis, daca mai e in lista
    const last = saved.last;
    if (!closed && last && all.some((x) => x.folder === last.folder && x.path === last.path)) openFile(last);
    if (!closed) search.focus();
  });
  return { close };
}
