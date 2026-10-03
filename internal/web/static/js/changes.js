import { el, clock } from "./ui.js";
import { statBadge } from "./md.js";

// changesView: tab-ul "Changes" al unui task. Starea "diff deschis" (cheie: run + cale)
// ramane intre reincarcari ale datelor.
// Intoarce { el, show(data, hasSubtasks), error(msg) }; data = raspunsul GET /api/tasks/{id}/changes.
export function changesView() {
  const root = el("div", { class: "changes" }, el("p", { class: "hint" }, "Loading..."));
  const open = new Set();

  // hunkHeader: "@@ -a,b +c,d @@"; lipseste cand numerele de linie nu se cunosc (fallback fara structuredPatch)
  function hunkHeader(h) {
    if (!h.oldStart && !h.newStart) return null;
    return el("div", { class: "diff-line diff-hunk" }, `@@ -${h.oldStart},${h.oldLines} +${h.newStart},${h.newLines} @@`);
  }

  function lineNode(text) {
    const c = text[0];
    const cls = c === "+" ? "diff-add" : c === "-" ? "diff-del" : c === "\\" ? "diff-meta" : "diff-ctx";
    return el("div", { class: `diff-line ${cls}` }, text === "" ? " " : text);
  }

  // diffNode: liniile se adauga intr-un bucla (fara spread: un diff de 150k+ linii ar depasi stiva)
  function diffNode(f) {
    const frag = document.createDocumentFragment();
    for (const h of f.hunks || []) {
      const head = hunkHeader(h);
      if (head) frag.append(head);
      for (const line of h.lines || []) frag.append(lineNode(line));
    }
    const d = el("div", { class: "diff" });
    d.append(frag);
    return d;
  }

  // pathNode: folderul estompat, numele fisierului evidentiat
  function pathNode(path) {
    const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\")) + 1;
    return el("span", { class: "change-path" },
      cut ? el("span", { class: "change-dir" }, path.slice(0, cut)) : null,
      el("span", { class: "change-name" }, path.slice(cut)));
  }

  function fileNode(run, f) {
    const key = `${run.runId}|${f.path}`;
    const trunc = f.truncated ? el("p", { class: "hint" }, "Truncated: the change was too large to keep in full.") : null;
    const body = el("div", { class: "change-body", hidden: !open.has(key) }, trunc);
    // diff-ul se construieste abia la prima deschidere (sau la redesenare cat e deschis)
    let built = false;
    function ensureDiff() {
      if (built) return;
      built = true;
      body.append(diffNode(f));
    }
    if (open.has(key)) ensureDiff();
    const btn = el("button", { type: "button", class: "change-file", "aria-expanded": String(open.has(key)) },
      pathNode(f.path),
      statBadge(f));
    btn.addEventListener("click", () => {
      if (open.has(key)) open.delete(key); else open.add(key);
      if (open.has(key)) ensureDiff();
      body.hidden = !open.has(key);
      btn.setAttribute("aria-expanded", String(open.has(key)));
    });
    return el("div", { class: "change" }, btn, body);
  }

  function runNode(run) {
    const n = run.files.length;
    const parts = [run.agent || "?", clock(run.at)];
    if (n) parts.push(`${n} ${n === 1 ? "file" : "files"}`);
    let added = 0, removed = 0;
    for (const f of run.files) { added += f.added; removed += f.removed; }
    const nodes = [el("h4", { class: "change-run" }, parts.join(" · "), n ? statBadge({ added, removed }) : null),
      ...run.files.map((f) => fileNode(run, f))];
    if (run.commands.length) {
      nodes.push(el("div", { class: "change-cmds" },
        el("h5", {}, "Commands run"),
        el("ul", {}, ...run.commands.map((c) => el("li", {},
          el("span", { class: "cmd-text" }, c.text),
          c.ok ? null : el("span", { class: "cmd-failed" }, "failed"))))));
    }
    return el("section", { class: "change-group" }, ...nodes);
  }

  return {
    el: root,
    show(data, hasSubtasks) {
      const runs = data.runs || [];
      if (!runs.length) {
        // replaceChildren nativ scrie null ca text: filtram
        root.replaceChildren(...[
          el("p", { class: "hint" }, "No file changes recorded for this task."),
          el("p", { class: "hint" }, "Changes made through shell commands are not captured as diffs."),
          hasSubtasks ? el("p", { class: "hint" }, "Changes made by subtasks are shown on each subtask.") : null,
        ].filter(Boolean));
        return;
      }
      root.replaceChildren(...runs.map(runNode)); // putine grupuri; liniile de diff nu trec prin spread
    },
    error(msg) {
      root.replaceChildren(el("p", { class: "hint" }, msg));
    },
  };
}
