import { api } from "./api.js";
import { el, toast } from "./ui.js";

// ago: de cand a fost sters un task, scurt ("5 min ago"; dupa o zi, data)
function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric" });
}

// deletedPanel: task-urile sterse din proiect, cu Restore (panou deasupra board-ului, ca Recurring).
// onChanged(): dupa un Restore (workspace reincarca proiectul).
export function deletedPanel({ projectId, onClose, onChanged }) {
  const list = el("ul", { class: "deleted-list" }, el("li", { class: "hint" }, "Loading..."));
  const root = el("section", { class: "section deleted-panel", "aria-label": "Deleted tasks" },
    el("div", { class: "section-head" },
      el("h2", {}, "Deleted tasks"),
      el("div", { class: "actions" },
        el("button", { type: "button", class: "btn small", onclick: onClose }, "Close"))),
    el("p", { class: "hint" }, "Deleted tasks are kept. Restore brings a task back to where it was, with its subtasks."),
    list);
  load();

  async function load() {
    try {
      const res = await api(`/api/projects/${projectId}/archived`);
      const tasks = res.tasks || [];
      if (!tasks.length) { list.replaceChildren(el("li", { class: "hint" }, "Nothing deleted.")); return; }
      list.replaceChildren(...tasks.map((t) => {
        const btn = el("button", { type: "button", class: "btn small" }, "Restore");
        btn.addEventListener("click", async () => {
          btn.disabled = true;
          try {
            await api(`/api/tasks/${t.id}/restore`, { method: "POST" });
            toast(`Restored T-${t.number}`, "ok");
            onChanged();
            load();
          } catch (err) {
            toast(err.message);
            btn.disabled = false;
          }
        });
        return el("li", { class: "deleted-row" },
          el("span", { class: "task-id" }, `T-${t.number}`),
          el("span", { class: "deleted-title" }, t.title),
          el("span", { class: "hint" }, (t.subtasks ? `${t.subtasks} ${t.subtasks === 1 ? "subtask" : "subtasks"} · ` : "") + `deleted ${ago(t.archivedAt)}`),
          btn);
      }));
    } catch (err) {
      list.replaceChildren(el("li", { class: "hint" }, `Could not load: ${err.message}`));
    }
  }

  return { el: root };
}
