import { api } from "./api.js";
import { el, toast } from "./ui.js";
import { highlightInto, languageFor } from "./highlight.js";
import { diffBlock } from "./changes.js";
import { statBadge } from "./md.js";

// openFileView: modala cu fisierul de acum, liniile schimbate de task marcate pe margine,
// si comutare pe Diff. Se inchide cu Esc, cu x sau cu clic in afara.
export async function openFileView({ taskId, subTaskId, path }) {
  const q = new URLSearchParams({ path });
  if (subTaskId && subTaskId !== taskId) q.set("task", String(subTaskId));
  let v;
  try {
    v = await api(`/api/tasks/${taskId}/file?${q}`);
  } catch (err) {
    toast(err.message);
    return;
  }
  const lang = languageFor(path);
  let added = 0, removed = 0;
  for (const run of v.runs || []) for (const f of run.files || []) { added += f.added; removed += f.removed; }

  const fileBody = el("div", { class: "fv-file" });
  const diffBody = el("div", { class: "fv-diff", hidden: true });
  let mode = "file";
  const toggle = el("button", { type: "button", class: "btn small" }, "Diff");
  toggle.addEventListener("click", () => {
    mode = mode === "file" ? "diff" : "file";
    fileBody.hidden = mode !== "file";
    diffBody.hidden = mode !== "diff";
    toggle.textContent = mode === "file" ? "Diff" : "File";
  });
  const copy = el("button", { type: "button", class: "btn small", onclick: async () => {
    try { await navigator.clipboard.writeText(path); toast("Path copied"); } catch { toast(path); }
  } }, "Copy path");

  // vederea Diff: diff-urile task-ului pe fisier, pe run-uri
  for (const run of v.runs || []) {
    diffBody.append(el("h4", { class: "change-run" }, `${run.agent || "?"} · ${new Date(run.at).toLocaleString()}`));
    for (const f of run.files || []) diffBody.append(diffBlock(f.path, f.hunks));
  }

  // vederea File
  const note = (t) => el("p", { class: "hint" }, t);
  if (!v.exists) {
    fileBody.append(note("The file no longer exists."));
    toggle.click();
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

  const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1;
  const before = document.activeElement;
  const close = () => { overlay.remove(); document.removeEventListener("keydown", onKey, true); before?.focus?.(); };
  // capture: Esc inchide doar modala de fisier, iar Tab nu ajunge la capcana de focus a modalei de task
  const onKey = (e) => {
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(); }
    else if (e.key === "Tab") e.stopPropagation();
  };
  const box = el("div", { class: "modal fv", role: "dialog", "aria-modal": "true", "aria-label": path },
    el("div", { class: "fv-head" },
      el("span", { class: "change-path" }, cut ? el("span", { class: "change-dir" }, path.slice(0, cut)) : null,
        el("span", { class: "change-name" }, path.slice(cut))),
      statBadge({ added, removed }),
      el("span", { class: "fv-actions" }, toggle, copy,
        el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: close }, "×"))),
    fileBody, diffBody);
  const overlay = el("div", { class: "modal-overlay fv-overlay", onclick: (e) => { if (e.target === overlay) close(); } }, box);
  document.addEventListener("keydown", onKey, true);
  document.body.append(overlay);
  box.tabIndex = -1;
  box.focus();
  fileBody.querySelector(".fv-mark")?.scrollIntoView({ block: "center" });
}

// codeWithMarkers: codul colorat cu numere de linie si marcaje pe margine. Coloram tot textul o data
// (contextul pe mai multe linii ramane corect), apoi taiem HTML-ul pe linii.
function codeWithMarkers(text, lang, markers) {
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
