import { el, clock } from "./ui.js";
import { statBadge } from "./md.js";

// modifiedView: tab-ul "Modified" al unui task: un rand pe fisier (si pe sub-task); clic = onOpen(file).
export function modifiedView({ onOpen }) {
  const root = el("div", { class: "modified" }, el("p", { class: "hint" }, "Loading..."));
  function row(f, ownTaskId) {
    const cut = Math.max(f.path.lastIndexOf("/"), f.path.lastIndexOf("\\")) + 1;
    return el("button", { type: "button", class: "change-file modified-row", onclick: () => onOpen(f) },
      el("span", { class: "change-path" },
        cut ? el("span", { class: "change-dir" }, f.path.slice(0, cut)) : null,
        el("span", { class: "change-name" }, f.path.slice(cut))),
      f.taskId !== ownTaskId ? el("span", { class: "modified-ref" }, f.ref) : null,
      statBadge({ added: f.added, removed: f.removed, kind: f.created ? "create" : "edit" }),
      el("span", { class: "modified-meta" }, `${f.agent || "?"} · ${clock(f.at)}`));
  }
  return {
    el: root,
    show(data, ownTaskId) {
      const files = data.files || [];
      if (!files.length) {
        root.replaceChildren(el("p", { class: "hint" }, "No file changes recorded for this task."),
          el("p", { class: "hint" }, "Changes made through shell commands are not captured as diffs."));
        return;
      }
      root.replaceChildren(...files.map((f) => row(f, ownTaskId)));
    },
    error(msg) { root.replaceChildren(el("p", { class: "hint" }, msg)); },
  };
}
