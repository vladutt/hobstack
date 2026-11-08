import { el, toast } from "./ui.js";
import { renderMarkdown } from "./md.js";
import { highlightInto, languageFor } from "./highlight.js";
import { openButtons } from "./open-file.js";

// Cat text se arata in preview (restul se descarca).
const PREVIEW_BYTES = 1 << 20;

// Extensiile afisate ca text (pe langa mime-urile text/*).
const TEXT_EXT = new Set(["txt", "md", "markdown", "json", "yaml", "yml", "csv", "tsv", "log", "xml", "ini", "toml",
  "go", "php", "js", "mjs", "ts", "py", "rb", "java", "kt", "sql", "sh", "css", "scss", "env", "conf"]);
const IMAGE_MIME = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

function ext(name) {
  const i = name.lastIndexOf(".");
  return i < 0 ? "" : name.slice(i + 1).toLowerCase();
}

// kindOfFile: cum se arata fisierul in preview ("image" | "pdf" | "markdown" | "text" | "none").
// HTML nu se arata niciodata (scris de agenti, ar rula pe originea aplicatiei); SVG doar ca <img>, unde
// script-urile lui nu ruleaza.
export function kindOfFile(f) {
  const base = (f.mime || "").split(";")[0].trim();
  const e = ext(f.name);
  if (e === "svg" || base === "image/svg+xml") return "image";
  if (e === "html" || e === "htm" || base === "text/html") return "none";
  if (IMAGE_MIME.has(base)) return "image";
  if (base === "application/pdf") return "pdf";
  if (e === "md" || e === "markdown") return "markdown";
  if (base.startsWith("text/") || TEXT_EXT.has(e) || base === "application/json") return "text";
  return "none";
}

function size(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1 << 20)).toFixed(1)} MB`;
}

function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

const url = (id, download) => `/api/task-files/${id}${download ? "?download=1" : ""}`;

const hasFiles = (e) => Array.from(e.dataTransfer?.types || []).includes("Files");

// taskFilesView: tab-ul "Files" al unui task. Click pe un rand deschide preview-ul in locul listei (cu Back).
// onUpload(files) -> Promise: urca fisierele utilizatorului (lipsa = fara Upload); canUpload() -> false pe un task sters.
// Intoarce { el, show(data), error(msg) }; data = raspunsul GET /api/tasks/{id}/files.
export function taskFilesView({ onUpload = null, canUpload = () => true, limit = 0 } = {}) {
  const root = el("div", { class: "tfiles" });
  let last = null;       // ultimul raspuns aratat
  let previewId = null;  // fisierul deschis (ramane deschis la refresh)
  const open = new Set(); // cai cu "earlier versions" desfasurat
  let uploading = false;  // un upload in curs (butonul e dezactivat)
  let showAll = false;    // cu limit: toate grupurile, nu doar primele

  // Upload: input ascuns + drag & drop pe tot tab-ul
  const picker = el("input", { type: "file", multiple: true, hidden: true, class: "tfiles-picker",
    onchange: () => { const list = Array.from(picker.files); picker.value = ""; send(list); } });
  const uploadBtn = el("button", { type: "button", class: "btn small", onclick: () => picker.click() }, "Upload");
  const status = el("span", { class: "hint tfiles-status", "aria-live": "polite" });
  const bar = el("div", { class: "tfiles-bar" }, uploadBtn,
    el("span", { class: "hint" }, "or drop or paste files here. The agent reads them on this task."), status, picker);

  let depth = 0; // dragenter/dragleave se declanseaza si pe copii
  const dropOn = () => onUpload && canUpload();
  root.addEventListener("dragenter", (e) => { if (hasFiles(e) && dropOn()) { depth++; root.classList.add("dropping"); } });
  root.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
  root.addEventListener("dragleave", () => { if (--depth <= 0) { depth = 0; root.classList.remove("dropping"); } });
  root.addEventListener("drop", (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault(); // fara el browserul ar deschide fisierul in locul aplicatiei
    depth = 0;
    root.classList.remove("dropping");
    if (dropOn()) send(Array.from(e.dataTransfer.files));
  });

  async function send(list) {
    if (!list.length || uploading || !dropOn()) return;
    uploading = true;
    uploadBtn.disabled = true;
    status.textContent = `Uploading ${list.length} ${list.length === 1 ? "file" : "files"}...`;
    try {
      await onUpload(list);
      status.textContent = "";
    } catch (err) {
      status.textContent = "";
      toast(err.message);
    } finally {
      uploading = false;
      uploadBtn.disabled = false;
    }
  }

  function show(data) {
    last = data;
    if (previewId != null) {
      if (findFile(previewId)) return; // preview-ul ramane; lista se redeseneaza la Back
      previewId = null;
    }
    renderList();
  }

  function error(msg) {
    root.replaceChildren(el("p", { class: "hint" }, `Could not load the files: ${msg}`));
  }

  function findFile(id) {
    const groups = [...(last.user || []), ...last.files, ...last.subtasks.flatMap((s) => s.files)];
    for (const g of groups) {
      if (g.id === id) return g;
      const v = g.versions.find((x) => x.id === id);
      if (v) return { ...g, ...v, versions: [] };
    }
    return null;
  }

  function renderList() {
    const nodes = [];
    if (onUpload && canUpload()) nodes.push(bar);
    const user = last.user || [];
    if (user.length) {
      nodes.push(el("h4", { class: "tfiles-sub-head" }, "From you"));
      nodes.push(el("ul", { class: "tfiles-list" }, user.map(row)));
    }
    if (last.files.length) {
      if (user.length) nodes.push(el("h4", { class: "tfiles-sub-head" }, "From agents"));
      const cut = limit > 0 && !showAll && last.files.length > limit;
      nodes.push(el("ul", { class: "tfiles-list" }, (cut ? last.files.slice(0, limit) : last.files).map(row)));
      if (cut) nodes.push(el("button", { type: "button", class: "link-btn tfiles-more", onclick: () => { showAll = true; renderList(); } },
        `Show all ${last.files.length}`));
    } else if (!user.length && !last.subtasks.length) {
      nodes.push(el("p", { class: "hint" }, "No files on this task yet."));
    }
    if (last.subtasks.length) {
      nodes.push(el("h4", { class: "tfiles-sub-head" }, "From subtasks"));
      for (const s of last.subtasks) {
        nodes.push(el("div", { class: "tfiles-sub" },
          el("div", { class: "tfiles-sub-title" }, el("span", { class: "task-id" }, s.ref), " ", s.title),
          el("ul", { class: "tfiles-list" }, s.files.map(row))));
      }
    }
    root.replaceChildren(...nodes);
  }

  function toggle(path) {
    if (open.has(path)) open.delete(path);
    else open.add(path);
    renderList();
  }

  function row(g) {
    const dir = g.path.includes("/") ? g.path.slice(0, g.path.lastIndexOf("/") + 1) : "";
    const n = g.versions.length;
    return el("li", { class: "tfiles-row" },
      el("button", { type: "button", class: "tfiles-main", title: g.path, onclick: () => preview(g.id) },
        el("span", { class: "tfiles-name" }, g.name),
        dir ? el("span", { class: "tfiles-dir" }, dir) : null),
      el("span", { class: "tfiles-meta" }, `${size(g.size)} · ${g.agentName} · ${ago(g.createdAt)}${g.ref ? " · " + g.ref : ""}`),
      n ? el("button", { type: "button", class: "link-btn tfiles-meta", "aria-expanded": String(open.has(g.path)), onclick: () => toggle(g.path) },
        `${n} earlier ${n === 1 ? "version" : "versions"}`) : null,
      el("span", { class: "tfiles-actions" }, ...openButtons(`/api/task-files/${g.id}`, g.name),
        el("a", { class: "btn ghost small", href: url(g.id, true), download: g.name }, "Download")),
      open.has(g.path) ? el("ul", { class: "tfiles-versions" }, g.versions.map((v) => el("li", {},
        el("button", { type: "button", class: "link-btn", onclick: () => preview(v.id) }, ago(v.createdAt)),
        el("span", { class: "tfiles-meta" }, ` ${size(v.size)} · ${v.agentName}${v.ref ? " · " + v.ref : ""} · `),
        el("a", { class: "link-btn", href: url(v.id, true), download: g.name }, "Download")))) : null);
  }

  async function preview(id) {
    const f = findFile(id);
    if (!f) return;
    previewId = id;
    const back = el("button", { type: "button", class: "btn ghost small", onclick: () => { previewId = null; renderList(); } }, "← Back");
    const head = el("div", { class: "tfiles-preview-head" }, back,
      el("span", { class: "tfiles-name" }, f.name), el("span", { class: "tfiles-dir" }, f.path),
      el("span", { class: "grow" }),
      ...openButtons(`/api/task-files/${id}`, f.name),
      el("a", { class: "btn ghost small", href: url(id, true), download: f.name }, "Download"));
    const body = el("div", { class: "tfiles-preview-body" }, el("p", { class: "hint" }, "Loading..."));
    root.replaceChildren(head, body);
    const kind = kindOfFile(f);
    if (kind === "image") { body.replaceChildren(el("img", { src: url(id), alt: f.name })); return; }
    if (kind === "pdf") { body.replaceChildren(el("iframe", { src: url(id), title: f.name })); return; }
    if (kind === "none") { body.replaceChildren(el("p", { class: "hint" }, "No preview for this file type.")); return; }
    if (!f.size) { body.replaceChildren(el("p", { class: "hint" }, "The file is empty.")); return; }
    try {
      // doar primul MB: un log de 30 MB nu se incarca in modala
      const res = await fetch(url(id), { headers: { Range: `bytes=0-${PREVIEW_BYTES - 1}` } });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const text = await res.text();
      if (previewId !== id) return;
      const nodes = [kind === "markdown" ? renderMarkdown(text) : (() => { const pre = el("pre", { class: "tfiles-text" }); highlightInto(pre, text, languageFor(f.name)); return pre; })()];
      if (f.size > PREVIEW_BYTES) nodes.push(el("p", { class: "hint" }, "Showing the first 1 MB. Download to see the rest."));
      body.replaceChildren(...nodes); // replaceChildren scrie "null" pentru un copil null
    } catch (err) {
      if (previewId === id) body.replaceChildren(el("p", { class: "hint" }, `Could not load the file: ${err.message}`));
    }
  }

  return { el: root, show, error };
}
