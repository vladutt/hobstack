import { api } from "./api.js";
import { el, toast } from "./ui.js";
import { highlightInto, languageFor } from "./highlight.js";
import { diffBlock } from "./changes.js";
import { statBadge } from "./md.js";

// copyText: textul in clipboard, cu toast; fara clipboard (http fara permisiune) toast-ul arata textul
export async function copyText(text) {
  try { await navigator.clipboard.writeText(text); toast(`Copied ${text}`, "ok"); } catch { toast(text); }
}

// openFileView: modala cu fisierul de acum, liniile schimbate de task marcate pe margine,
// si comutare pe Diff. Se inchide cu Esc, cu x sau cu clic in afara.
// files: fisierele modificate de task si de sub-task-uri ({ path, taskId, ref, added, removed, created });
// cu mai mult de unul, in stanga apare lista lor (grupata pe foldere) si se trece intre ele fara sa inchizi.
export async function openFileView({ taskId, subTaskId, path, files }) {
  const first = { path, taskId: subTaskId || taskId };
  let v;
  try {
    v = await fetchFile(taskId, first);
  } catch (err) {
    toast(err.message);
    return;
  }

  const list = (files || []).filter((f) => f.path);
  const same = (a, b) => a.path === b.path && (a.taskId || taskId) === (b.taskId || taskId);
  if (!list.some((f) => same(f, first))) list.unshift(first);

  const pathBox = el("span", { class: "change-path" });
  const badgeBox = el("span", {});
  const fileBody = el("div", { class: "fv-file" });
  const diffBody = el("div", { class: "fv-diff", hidden: true });
  let mode = "file";       // ce a ales utilizatorul; un fisier sters se arata pe Diff fara sa schimbe alegerea
  let forcedDiff = false;  // fisierul curent nu mai exista: doar Diff
  const toggle = el("button", { type: "button", class: "btn small" }, "Diff");
  function applyMode() {
    const m = forcedDiff ? "diff" : mode;
    fileBody.hidden = m !== "file";
    diffBody.hidden = m !== "diff";
    toggle.textContent = m === "file" ? "Diff" : "File";
    toggle.disabled = forcedDiff;
  }
  toggle.addEventListener("click", () => { mode = mode === "file" ? "diff" : "file"; applyMode(); });
  let current = first;
  const copy = el("button", { type: "button", class: "btn small", onclick: () => copyText(current.path) }, "Copy path");

  // show: deseneaza fisierul primit de la server (File + Diff) si antetul
  function show(f, v) {
    current = f;
    const lang = languageFor(f.path);
    let added = 0, removed = 0;
    for (const run of v.runs || []) for (const c of run.files || []) { added += c.added; removed += c.removed; }
    const cut = Math.max(f.path.lastIndexOf("/"), f.path.lastIndexOf("\\")) + 1;
    pathBox.replaceChildren(cut ? el("span", { class: "change-dir" }, f.path.slice(0, cut)) : "",
      el("span", { class: "change-name" }, f.path.slice(cut)));
    badgeBox.replaceChildren(statBadge({ added, removed }));
    box.setAttribute("aria-label", f.path);

    // vederea Diff: diff-urile task-ului pe fisier, pe run-uri
    diffBody.replaceChildren();
    for (const run of v.runs || []) {
      diffBody.append(el("h4", { class: "change-run" }, `${run.agent || "?"} · ${new Date(run.at).toLocaleString()}`));
      for (const c of run.files || []) diffBody.append(diffBlock(c.path, c.hunks));
    }

    // vederea File
    const note = (t) => el("p", { class: "hint" }, t);
    fileBody.replaceChildren();
    forcedDiff = !v.exists;
    if (!v.exists) {
      fileBody.append(note("The file no longer exists."));
    } else if (v.binary) {
      fileBody.append(note("Binary file: no preview. See Diff."));
    } else if (v.tooLarge) {
      fileBody.append(note("The file is larger than 1 MB: no preview. See Diff."));
    } else {
      fileBody.append(note(v.exact
        ? "Markers show this task's changes; the file may have changed since."
        : "This task's changes can't be placed on lines (a change was too large or had no line numbers); see Diff."));
      fileBody.append(codeWithMarkers(v.content, lang, v.exact ? v.markers : null));
    }
    applyMode();
    for (const [k, btn] of rows) {
      const on = same(k, f);
      btn.classList.toggle("on", on);
      if (on) btn.setAttribute("aria-current", "true"); else btn.removeAttribute("aria-current");
    }
    fileBody.scrollTop = diffBody.scrollTop = 0;
    fileBody.scrollLeft = diffBody.scrollLeft = 0;
    fileBody.querySelector(".fv-mark")?.scrollIntoView({ block: "center" });
  }

  // open: trece pe alt fisier din lista; un raspuns vechi nu suprascrie unul nou
  let seq = 0;
  async function open(f) {
    if (same(f, current)) return;
    const my = ++seq;
    try {
      const nv = await fetchFile(taskId, f);
      if (my === seq) show(f, nv);
    } catch (err) {
      if (my === seq) toast(err.message);
    }
  }

  // lista din stanga: un grup pe folder, fisierele sub el
  const rows = [];
  let tree = null;
  if (list.length > 1) {
    const sorted = [...list].sort((a, b) => a.path.localeCompare(b.path));
    const groups = new Map();
    for (const f of sorted) {
      const cut = f.path.lastIndexOf("/") + 1;
      const dir = f.path.slice(0, cut);
      if (!groups.has(dir)) groups.set(dir, []);
      groups.get(dir).push(f);
    }
    const nodes = [];
    for (const [dir, fs] of groups) {
      if (dir) nodes.push(el("div", { class: "fv-tree-dir", title: dir }, dir));
      for (const f of fs) {
        const name = f.path.slice(f.path.lastIndexOf("/") + 1);
        const btn = el("button", { type: "button", class: "fv-tree-file" + (dir ? "" : " root"), title: f.path, onclick: () => open(f) },
          el("span", { class: "fv-tree-name" }, name),
          f.taskId && f.taskId !== taskId && f.ref ? el("span", { class: "modified-ref" }, f.ref) : null,
          f.shell ? shellTag() : null,
          f.added != null ? statBadge({ added: f.added, removed: f.removed, kind: f.created ? "create" : "edit" }) : null);
        rows.push([f, btn]);
        nodes.push(btn);
      }
    }
    tree = el("nav", { class: "fv-tree", "aria-label": "Changed files" },
      el("div", { class: "fv-tree-head" }, `Changed files · ${list.length}`), ...nodes);
  }

  const before = document.activeElement;
  const close = () => { overlay.remove(); document.removeEventListener("keydown", onKey, true); before?.focus?.(); };
  // capture: Esc inchide doar modala de fisier, iar Tab nu ajunge la capcana de focus a modalei de task.
  // In lista, sagetile sus / jos trec pe fisierul vecin.
  const onKey = (e) => {
    if (e.target?.closest?.(".ps-overlay")) return; // tastele din switcher-ul de proiecte (Cmd/Ctrl+K)
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(); }
    else if (e.key === "Tab") e.stopPropagation();
    else if ((e.key === "ArrowDown" || e.key === "ArrowUp") && tree?.contains(document.activeElement)) {
      e.preventDefault();
      const i = rows.findIndex(([, b]) => b === document.activeElement);
      const next = rows[Math.min(rows.length - 1, Math.max(0, i + (e.key === "ArrowDown" ? 1 : -1)))];
      if (next) { next[1].focus(); open(next[0]); }
    }
  };
  const box = el("div", { class: "modal fv" + (tree ? " with-tree" : ""), role: "dialog", "aria-modal": "true" },
    el("div", { class: "fv-head" }, pathBox, badgeBox,
      el("span", { class: "fv-actions" }, toggle, copy,
        el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: close }, "×"))),
    el("div", { class: "fv-main" }, tree, el("div", { class: "fv-content" }, fileBody, diffBody)));
  const overlay = el("div", { class: "modal-overlay fv-overlay", onclick: (e) => { if (e.target === overlay) close(); } }, box);
  document.addEventListener("keydown", onKey, true);
  document.body.append(overlay);
  show(first, v);
  rows.find(([f]) => same(f, first))?.[1].scrollIntoView({ block: "nearest" });
  box.tabIndex = -1;
  box.focus();
}

// fetchFile: GET /api/tasks/{id}/file pentru fisierul f (al task-ului sau al unui sub-task)
function fetchFile(taskId, f) {
  const q = new URLSearchParams({ path: f.path });
  if (f.taskId && f.taskId !== taskId) q.set("task", String(f.taskId));
  return api(`/api/tasks/${taskId}/file?${q}`);
}

// codeWithMarkers: codul colorat cu numere de linie si marcaje pe margine. Coloram tot textul o data
// (contextul pe mai multe linii ramane corect), apoi taiem HTML-ul pe linii.
export function codeWithMarkers(text, lang, markers) {
  const tmp = el("code", {});
  highlightInto(tmp, text, lang);
  const lines = splitHighlighted(tmp);
  const kind = new Map();
  for (const n of markers?.added || []) kind.set(n, "added");
  for (const n of markers?.modified || []) kind.set(n, "modified");
  const deleted = new Set(markers?.deleted || []);
  const rows = el("div", { class: "fv-code" });
  const frag = document.createDocumentFragment();
  lines.forEach((html, i) => {
    const n = i + 1;
    const k = kind.get(n);
    const row = el("div", { class: "fv-line" + (k ? ` fv-mark fv-${k}` : "") + (deleted.has(n) ? " fv-del-before" : "") },
      el("span", { class: "fv-num" }, String(n)),
      el("span", { class: "fv-gutter" }));
    const code = el("span", { class: "fv-text hljs" });
    code.innerHTML = html || " "; // html vine din highlight.js (deja scapat), sau linia goala
    row.append(code);
    frag.append(row);
  });
  if (deleted.has(lines.length + 1)) frag.append(el("div", { class: "fv-line fv-mark fv-del-before fv-end" }, el("span", { class: "fv-num" }, ""), el("span", { class: "fv-gutter" })));
  rows.append(frag);
  return rows;
}

// splitHighlighted: imparte HTML-ul colorat pe linii, inchizand si redeschizand <span>-urile
// deschise la capatul fiecarei linii (un comentariu pe mai multe linii ramane colorat).
function splitHighlighted(code) {
  const out = [];
  let cur = "";
  const open = [];
  const walk = (node) => {
    for (const ch of node.childNodes) {
      if (ch.nodeType === Node.TEXT_NODE) {
        const parts = ch.textContent.split("\n");
        parts.forEach((p, i) => {
          if (i > 0) {
            out.push(cur + "</span>".repeat(open.length));
            cur = open.map((c) => `<span class="${c}">`).join("");
          }
          cur += p.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
        });
      } else if (ch.nodeType === Node.ELEMENT_NODE) {
        const cls = ch.getAttribute("class") || "";
        open.push(cls);
        cur += `<span class="${cls}">`;
        walk(ch);
        open.pop();
        cur += "</span>";
      }
    }
  };
  walk(code);
  out.push(cur);
  if (out.length > 1 && out[out.length - 1] === "") out.pop(); // ultimul "\n" din fisier
  return out;
}

// shellTag: eticheta "shell" pe un fisier schimbat prin shell (gasit din git, nu din Edit / Write)
export function shellTag() {
  return el("span", { class: "shell-tag", title: "Changed through the shell, not with Edit or Write: found by comparing git before and after the run. Another agent working in the same folder at the same time could also have made it." }, "shell");
}
