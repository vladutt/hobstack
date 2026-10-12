import { el, clock } from "./ui.js";
import { highlightLine, languageFor } from "./highlight.js";

// hunkHeader: "@@ -a,b +c,d @@"; lipseste cand numerele de linie nu se cunosc (fallback fara structuredPatch)
function hunkHeader(h) {
  if (!h.oldStart && !h.newStart) return null;
  return el("div", { class: "diff-line diff-hunk" }, `@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@`);
}

// lineNode: o linie de diff; prefixul (+, -, spatiu) ramane separat, codul de dupa el se coloreaza dupa limbaj
function lineNode(text, lang) {
  const c = text[0];
  const cls = c === "+" ? "diff-add" : c === "-" ? "diff-del" : c === "\\" ? "diff-meta" : "diff-ctx";
  if (text === "") return el("div", { class: `diff-line ${cls}` }, " ");
  if (c === "\\") return el("div", { class: `diff-line ${cls}` }, text);
  return el("div", { class: `diff-line ${cls}` }, el("span", { class: "diff-sign" }, c), highlightLine(text.slice(1), lang));
}

// diffBlock: diff-ul unui fisier (fragmentele lui), colorat dupa extensia caii. Liniile se adauga intr-o
// bucla (fara spread: un diff de 150k+ linii ar depasi stiva). Folosit si de modala de fisier.
export function diffBlock(path, hunks) {
  const lang = languageFor(path);
  const frag = document.createDocumentFragment();
  for (const h of hunks || []) {
    const head = hunkHeader(h);
    if (head) frag.append(head);
    for (const line of h.lines || []) frag.append(lineNode(line, lang));
  }
  const d = el("div", { class: "diff" });
  d.append(frag);
  return d;
}

// commandsView: tab-ul "Commands" al unui task: comenzile rulate, pe run-uri. Fisierele modificate
// sunt in rezumatul de deasupra tab-urilor si in modala de fisier (cu diff-urile).
// Intoarce { el, show(data) -> numarul de comenzi, error(msg) }; data = raspunsul GET /api/tasks/{id}/changes.
export function commandsView() {
  const root = el("div", { class: "changes" }, el("p", { class: "hint" }, "Loading..."));

  function runNode(run) {
    const n = run.commands.length;
    return el("section", { class: "change-group" },
      el("h4", { class: "change-run" }, [run.agent || "?", clock(run.at), `${n} ${n === 1 ? "command" : "commands"}`].join(" · ")),
      el("div", { class: "change-cmds" },
        el("ul", {}, ...run.commands.map((c) => el("li", {},
          el("span", { class: "cmd-text" }, c.text),
          c.ok ? null : el("span", { class: "cmd-failed" }, "failed"))))));
  }

  return {
    el: root,
    show(data) {
      const runs = (data.runs || []).filter((r) => (r.commands || []).length);
      if (!runs.length) {
        root.replaceChildren(el("p", { class: "hint" }, "No commands run on this task."));
        return 0;
      }
      root.replaceChildren(...runs.map(runNode));
      return runs.reduce((n, r) => n + r.commands.length, 0);
    },
    error(msg) {
      root.replaceChildren(el("p", { class: "hint" }, msg));
    },
  };
}
