import { api } from "./api.js";
import { el, toast } from "./ui.js";
import { diffBlock } from "./changes.js";
import { statBadge } from "./md.js";
import { highlightInto, languageFor } from "./highlight.js";

// Bucati comune pentru skill-urile aduse din afara (Add skill si pagina elementului): textul sursei,
// lista fisierelor pregatite cu previzualizare, diff-ul pe linii si fereastra de Update.
// Numele, caile si continutul vin din afara (GitHub / skills.sh / fisiere locale): doar prin el() / textContent
// (highlight.js scapa singur textul).

// shortRef: primele 7 caractere dintr-un sha (commit sau amprenta locala)
export const shortRef = (ref) => String(ref || "").slice(0, 7);

// sourceText: "anthropics/skills @ 1ed29a0" (github), "claude.ai", "Plugin: superpowers 6.4.1"
export function sourceText(src) {
  if (!src) return "";
  if (src.kind === "github") return `${src.label || src.repo}${src.ref ? " @ " + shortRef(src.ref) : ""}`;
  return src.label || src.kind;
}

// githubURL: pagina folderului pe GitHub, la commit-ul dat (sau ramura implicita); null pentru un repo care
// nu are forma owner/repo (datele vin din afara: fara link-uri catre altceva)
const REPO_RE = /^[A-Za-z0-9._-]+\/[A-Za-z0-9._-]+$/;
export function githubURL(repo, path, ref) {
  if (!REPO_RE.test(String(repo || ""))) return null;
  const base = `https://github.com/${repo}`;
  if (!path && !ref) return base;
  return `${base}/tree/${ref || "HEAD"}/${path || ""}`.replace(/\/$/, "");
}

// sourceNode: textul sursei, cu link catre GitHub cand e cazul
export function sourceNode(src) {
  const href = src?.kind === "github" ? githubURL(src.repo, src.path, src.ref) : null;
  if (!href) return el("span", {}, sourceText(src));
  return el("a", { href, target: "_blank", rel: "noopener noreferrer" }, sourceText(src));
}

// sizeText: 512 B / 1.2 KB / 3.4 MB
export function sizeText(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1).replace(/\.0$/, "")} KB`;
  return `${(n / 1024 / 1024).toFixed(1).replace(/\.0$/, "")} MB`;
}

const plural = (n, one, many) => `${n} ${n === 1 ? one : many}`;
export { plural };

// dropToken: pregatirea nu mai trebuie (fereastra inchisa); erorile nu conteaza, serverul o sterge oricum in 30 min
export function dropToken(token) {
  if (token) api(`/api/library/skills/prepare/${encodeURIComponent(token)}`, { method: "DELETE" }).catch(() => {});
}

// preparedFiles: lista fisierelor pregatite (marime, "script", "sensitive") + previzualizarea fisierului ales.
// Previzualizarea vine din GET /api/library/skills/prepare/{token}/file?path=, colorata dupa extensie.
export function preparedFiles(token, files) {
  const preview = el("pre", { class: "md-pre skill-preview", hidden: true });
  const previewHead = el("div", { class: "skill-preview-head", hidden: true });
  let current = null;
  let seq = 0;
  const rows = files.map((f) => {
    const btn = el("button", { type: "button", class: "skill-file", "aria-pressed": "false" },
      el("span", { class: "skill-file-path mono" }, f.path),
      f.script ? el("span", { class: "mcp-badge req" }, "script") : null,
      f.sensitive ? el("span", { class: "mcp-badge warn" }, "sensitive") : null,
      el("span", { class: "skill-file-size muted" }, sizeText(f.size)));
    btn.addEventListener("click", () => show(f, btn));
    return btn;
  });
  async function show(f, btn) {
    if (current === btn) { // al doilea clic inchide previzualizarea
      current = null;
      btn.setAttribute("aria-pressed", "false");
      preview.hidden = previewHead.hidden = true;
      return;
    }
    current?.setAttribute("aria-pressed", "false");
    current = btn;
    btn.setAttribute("aria-pressed", "true");
    const my = ++seq;
    previewHead.hidden = preview.hidden = false;
    previewHead.replaceChildren(el("span", { class: "mono" }, f.path));
    preview.classList.remove("hljs");
    preview.textContent = "Loading…";
    try {
      const res = await fetch(`/api/library/skills/prepare/${encodeURIComponent(token)}/file?path=${encodeURIComponent(f.path)}`);
      const text = await res.text();
      if (my !== seq) return;
      if (!res.ok) {
        let msg = text;
        try { msg = JSON.parse(text).error || text; } catch { /* text simplu */ }
        preview.textContent = res.status === 413 ? "This file is too large to preview." : (msg || "Cannot show this file.");
        return;
      }
      if (text.slice(0, 8192).includes("\0")) { preview.textContent = "Binary file: no preview."; return; }
      highlightInto(preview, text, languageFor(f.path));
    } catch (err) {
      if (my === seq) preview.textContent = err.message;
    }
  }
  return el("div", { class: "skill-files-box" }, el("div", { class: "skill-files" }, rows), previewHead, preview);
}

// ---------- diff pe linii ----------

const CONTEXT = 3; // linii de context in jurul unei schimbari, ca la git
const MAX_CELLS = 4_000_000; // peste atat (dupa ce scoatem inceputul si sfarsitul comune) nu mai cautam LCS

const linesOf = (t) => {
  if (!t) return [];
  const l = String(t).split("\n");
  if (l[l.length - 1] === "") l.pop();
  return l;
};

// lineOps: operatiile [" "|"-"|"+", linie] dintre a si b (LCS pe linii; mijlocul prea mare = sters tot + adaugat tot)
function lineOps(a, b) {
  let pre = 0;
  while (pre < a.length && pre < b.length && a[pre] === b[pre]) pre++;
  let suf = 0;
  while (suf < a.length - pre && suf < b.length - pre && a[a.length - 1 - suf] === b[b.length - 1 - suf]) suf++;
  const A = a.slice(pre, a.length - suf);
  const B = b.slice(pre, b.length - suf);
  const ops = a.slice(0, pre).map((x) => [" ", x]);
  const n = A.length, m = B.length;
  if (n * m > MAX_CELLS) {
    for (const x of A) ops.push(["-", x]);
    for (const x of B) ops.push(["+", x]);
  } else {
    // lcs[i*(m+1)+j] = lungimea LCS pentru A[i:] si B[j:]
    const w = m + 1;
    const lcs = new Uint32Array((n + 1) * w);
    for (let i = n - 1; i >= 0; i--) {
      for (let j = m - 1; j >= 0; j--) {
        lcs[i * w + j] = A[i] === B[j] ? lcs[(i + 1) * w + j + 1] + 1 : Math.max(lcs[(i + 1) * w + j], lcs[i * w + j + 1]);
      }
    }
    let i = 0, j = 0;
    while (i < n && j < m) {
      if (A[i] === B[j]) { ops.push([" ", A[i]]); i++; j++; }
      else if (lcs[(i + 1) * w + j] >= lcs[i * w + j + 1]) ops.push(["-", A[i++]]);
      else ops.push(["+", B[j++]]);
    }
    while (i < n) ops.push(["-", A[i++]]);
    while (j < m) ops.push(["+", B[j++]]);
  }
  for (const x of a.slice(a.length - suf)) ops.push([" ", x]);
  return ops;
}

// textHunks: fragmentele in forma lui diffBlock ({oldStart, oldLines, newStart, newLines, lines: ["+x", "-y", " z"]})
export function textHunks(before, after) {
  const ops = lineOps(linesOf(before), linesOf(after));
  const hunks = [];
  let cur = null;
  let lastChange = -Infinity;
  let oldNo = 0, newNo = 0; // liniile de dinainte de pozitia curenta
  // pozitiile (oldNo, newNo) dinaintea fiecarei operatii, ca un fragment sa poata incepe cu context
  const at = ops.map(([op]) => {
    const pos = [oldNo, newNo];
    if (op !== "+") oldNo++;
    if (op !== "-") newNo++;
    return pos;
  });
  ops.forEach(([op], k) => {
    if (op === " ") return;
    if (cur && k - lastChange <= CONTEXT * 2) {
      for (let x = lastChange + 1; x < k; x++) cur.lines.push(" " + ops[x][1]);
    } else {
      if (cur) closeHunk(cur, ops, lastChange);
      const from = Math.max(0, k - CONTEXT);
      cur = { oldStart: at[from][0] + 1, newStart: at[from][1] + 1, lines: [] };
      for (let x = from; x < k; x++) cur.lines.push(" " + ops[x][1]);
      hunks.push(cur);
    }
    cur.lines.push(op + ops[k][1]);
    lastChange = k;
  });
  if (cur) closeHunk(cur, ops, lastChange);
  return hunks;
}

function closeHunk(h, ops, last) {
  for (let x = last + 1; x < Math.min(ops.length, last + 1 + CONTEXT); x++) h.lines.push(" " + ops[x][1]);
  h.oldLines = h.lines.filter((l) => l[0] !== "+").length;
  h.newLines = h.lines.filter((l) => l[0] !== "-").length;
  // un fisier gol de o parte: git scrie -0,0 (nu -1,0)
  if (!h.oldLines) h.oldStart = Math.max(0, h.oldStart - 1);
  if (!h.newLines) h.newStart = Math.max(0, h.newStart - 1);
}

const MAX_DIFF_LINES = 3000; // peste atat pe fisier aratam doar inceputul

// capHunks: primele MAX_DIFF_LINES linii din fragmente; more = cate linii au ramas pe dinafara
function capHunks(hunks) {
  let left = MAX_DIFF_LINES, more = 0;
  const out = [];
  for (const h of hunks) {
    if (left <= 0) { more += h.lines.length; continue; }
    if (h.lines.length <= left) { out.push(h); left -= h.lines.length; continue; }
    out.push({ ...h, lines: h.lines.slice(0, left) });
    more += h.lines.length - left;
    left = 0;
  }
  return { hunks: out, more };
}

const CHANGE = { added: "added", removed: "removed", modified: "modified", mode: "mode changed" };

// diffNode: diff-ul unui fisier, taiat la MAX_DIFF_LINES linii, cu nota pentru rest
function diffNode(path, hunks) {
  const { hunks: shown, more } = capHunks(hunks);
  return [diffBlock(path, shown), more ? el("p", { class: "hint skill-diff-cut" }, `Diff truncated: ${more} more ${more === 1 ? "line" : "lines"}.`) : null];
}

// diffList: lista schimbarilor pe fisiere (primele 3 deschise), fiecare cu diff-ul ei
export function diffList(diff) {
  if (!diff?.length) {
    return el("p", { class: "hint" }, "No file changes. Only the source reference moved.");
  }
  return el("div", { class: "skill-diff" }, diff.map((d, i) => {
    let added = 0, removed = 0;
    let hunks = null;
    if (!d.binary && !d.tooLarge && d.change !== "mode") {
      hunks = textHunks(d.before || "", d.after || "");
      for (const h of hunks) for (const l of h.lines) { if (l[0] === "+") added++; else if (l[0] === "-") removed++; }
    }
    const note = d.change === "mode" ? "The executable bit changed; the content is the same."
      : d.binary ? "Binary file: no diff." : d.tooLarge ? "Too large to show the diff." : null;
    const body = el("div", { class: "change-body", hidden: i >= 3 },
      note ? el("p", { class: "hint" }, note) : null,
      hunks ? (hunks.length ? diffNode(d.path, hunks)
        : el("p", { class: "hint" }, d.before || d.after ? "Only the newline at the end of the file changed." : "Empty file.")) : null);
    const btn = el("button", { type: "button", class: "change-file", "aria-expanded": String(i < 3) },
      el("span", { class: "change-path" }, d.path),
      el("span", { class: `skill-change ${d.change}` }, CHANGE[d.change] || d.change),
      hunks ? statBadge({ added, removed }) : null);
    btn.addEventListener("click", () => {
      body.hidden = !body.hidden;
      btn.setAttribute("aria-expanded", String(!body.hidden));
    });
    return el("div", { class: "change" }, btn, body);
  }));
}

// ---------- fereastra de Update (pagina elementului) ----------

// updateDialog: pregateste versiunea noua de la sursa, arata diferentele si o instaleaza la confirmare.
// onDone(item) primeste elementul cu versiunea noua.
export async function updateDialog(item, button, onDone) {
  const label = button.textContent;
  button.disabled = true;
  button.classList.add("busy");
  button.textContent = "Preparing…";
  let prep;
  try {
    prep = await api(`/api/library/items/${item.id}/source/prepare`, { method: "POST" });
  } catch (err) {
    toast(err.message);
    return;
  } finally {
    button.disabled = false;
    button.classList.remove("busy");
    button.textContent = label;
  }
  let busy = false;
  let left = false; // pagina parasita: la finalul unei cereri in curs fereastra se inchide singura
  let token = prep.token; // "" dupa ce s-a folosit sau a expirat
  const err = el("p", { class: "mcp-dlg-error", role: "alert", hidden: true });
  const onKey = (e) => { if (e.key === "Escape") close(); };
  const close = () => {
    if (busy) return;
    overlay.remove();
    document.removeEventListener("keydown", onKey);
    dropToken(token);
    token = "";
  };
  const diff = prep.diff || [];
  const files = prep.files || [];
  const counts = ["added", "modified", "removed", "mode"].map((c) => [c, diff.filter((d) => d.change === c).length]).filter(([, n]) => n);
  // scripturile noi, schimbate sau devenite scripturi (bitul executabil apare ca "mode"): orice fisier din diff,
  // in afara de cele sterse, care in versiunea noua e script. Un fisier neschimbat nu apare in diff.
  const scriptNow = new Set(files.filter((f) => f.script).map((f) => f.path));
  const scripts = diff.filter((d) => d.change !== "removed" && scriptNow.has(d.path));
  const goLabel = `Update to ${shortRef(prep.source?.ref)}`;
  const body = el("div", { class: "lib-form-body" });
  const foot = el("div", { class: "lib-form-foot" });
  const btn = (text, cls, onclick) => el("button", { type: "button", class: "btn " + cls, onclick }, text);
  const diffNode = diffList(diff);

  function form() {
    body.replaceChildren(
      el("h2", {}, `Update ${item.name}`),
      el("p", { class: "skill-src-line" }, "From ", sourceNode(item.source), " to ",
        el("span", { class: "mono" }, shortRef(prep.source?.ref))),
      err,
      el("p", { class: "hint" }, counts.length
        ? counts.map(([c, n]) => `${n} ${CHANGE[c]}`).join(" · ") + ` · ${plural(files.length, "file", "files")} in the new version`
        : "Nothing changed in the files."),
      diffNode,
      el("p", { class: "hint" }, "Projects that installed it can move to the new version in Instructions › Library, like with any new Library version."));
    const go = btn(goLabel, "primary", () => { if (scripts.length) confirmStep(); else send(go); });
    go.disabled = !token;
    foot.replaceChildren(btn("Cancel", "ghost", close), el("span", { class: "grow" }), go);
  }

  // confirmStep: ca la install, scripturile pe care agentii le pot rula cer confirmarea
  function confirmStep() {
    body.replaceChildren(
      el("h2", {}, `Update ${item.name}`),
      el("p", { class: "mcp-confirm" }, `This update adds or changes ${plural(scripts.length, "script", "scripts")} that agents can run. Update?`),
      el("ul", { class: "skill-script-list" }, scripts.map((d) => el("li", { class: "mono" }, d.path))));
    const go = btn(goLabel, "primary", () => send(go));
    foot.replaceChildren(btn("Back", "ghost", () => form()), el("span", { class: "grow" }), go);
    go.focus();
  }

  async function send(go) {
    busy = true;
    err.hidden = true;
    foot.querySelectorAll("button").forEach((b) => { b.disabled = true; });
    go.classList.add("busy");
    go.textContent = "Updating…";
    try {
      const it = await api(`/api/library/items/${item.id}/source/update`, { method: "POST", body: { token } });
      token = "";
      busy = false;
      toast(`${it.name} updated to v${it.version?.version || ""}`, "ok");
      close();
      if (!left) onDone?.(it);
    } catch (e) {
      busy = false;
      if (left) { toast(e.message); close(); return; } // close() sterge si pregatirea
      // pregatirea nu mai e buna: doar Cancel. O stergem acum (409 = serverul inca o tine)
      if (e.status === 410 || e.status === 409) { dropToken(token); token = ""; }
      err.textContent = e.message;
      err.hidden = false;
      form();
    }
  }

  const box = el("div", { class: "modal lib-form mcp-dialog skill-dialog", role: "dialog", "aria-modal": "true", "aria-label": `Update ${item.name}` }, body, foot);
  const overlay = el("div", { class: "modal-overlay", onclick: (e) => { if (e.target === overlay) close(); } }, box);
  form();
  document.addEventListener("keydown", onKey);
  document.body.append(overlay);
  foot.querySelector(".btn.primary")?.focus();
  // la iesirea din pagina: fereastra se inchide acum, sau la finalul cererii in curs
  return () => { left = true; close(); };
}
