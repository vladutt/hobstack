import { api } from "./api.js";
import { el, toast } from "./ui.js";

// stagesEditor: panoul in care utilizatorul isi face coloanele (etapele).
// Lucreaza pe o copie; Salveaza trimite toata lista (PUT /stages).
export function stagesEditor({ project, onSaved, onClose }) {
  const busy = new Set(project.tasks.filter((t) => t.stageId).map((t) => t.stageId)); // etape cu task-uri
  let list = project.stages.map((s) => ({ ...s, agentIds: [...s.agentIds] }));
  const rows = el("div", { class: "stage-rows" });
  const box = el("div", { class: "stages-editor section" },
    el("div", { class: "section-head" },
      el("h2", {}, "Columns (work stages)"),
      el("span", { class: "hint" }, "A task moves through the stages in order; the first free agent in a stage picks it up. After the last one, the lead decides.")),
    rows,
    el("div", { class: "actions" },
      el("button", { type: "button", class: "btn", onclick: () => { list.push({ name: "", kind: "work", agentIds: [] }); draw(); } }, "+ Stage"),
      el("span", { class: "grow" }),
      el("button", { type: "button", class: "btn", onclick: onClose }, "Cancel"),
      el("button", { type: "button", class: "btn primary", onclick: save }, "Save")));

  function draw() {
    rows.replaceChildren(...list.map((s, i) => {
      const name = el("input", { type: "text", value: s.name, maxlength: 40, placeholder: "e.g. Dev", oninput: (e) => { s.name = e.target.value; } });
      const kind = el("select", { value: s.kind, onchange: (e) => { s.kind = e.target.value; } },
        el("option", { value: "work" }, "Work"), el("option", { value: "check" }, "Check"));
      const agents = project.agents.map((a) => el("label", { class: "check" },
        el("input", { type: "checkbox", checked: s.agentIds.includes(a.id), onchange: (e) => {
          s.agentIds = e.target.checked ? [...s.agentIds, a.id] : s.agentIds.filter((x) => x !== a.id);
        } }), a.name));
      const move = (d) => { const j = i + d; if (j < 0 || j >= list.length) return; [list[i], list[j]] = [list[j], list[i]]; draw(); };
      const locked = s.id && busy.has(s.id);
      return el("div", { class: "stage-row" },
        el("div", { class: "stage-row-head" },
          el("span", { class: "stage-num" }, String(i + 1)), name, kind,
          el("button", { type: "button", class: "btn small", "aria-label": "Move up", disabled: i === 0, onclick: () => move(-1) }, "↑"),
          el("button", { type: "button", class: "btn small", "aria-label": "Move down", disabled: i === list.length - 1, onclick: () => move(1) }, "↓"),
          el("button", { type: "button", class: "btn small danger", "aria-label": "Delete stage", disabled: locked,
            title: locked ? "has tasks" : "Delete stage", onclick: () => { list.splice(i, 1); draw(); } }, "×")),
        el("div", { class: "stage-agents" }, agents.length ? agents : el("span", { class: "hint" }, "The project has no agents.")));
    }));
    if (!list.length) rows.append(el("p", { class: "hint" }, "No stages: the board uses the default columns (Queued, In progress, Review, Done)."));
  }

  async function save() {
    try {
      await api(`/api/projects/${project.id}/stages`, { method: "PUT", body: {
        stages: list.map((s) => ({ id: s.id, name: s.name.trim(), kind: s.kind, agentIds: s.agentIds })),
      } });
      toast("Columns saved.", "ok");
      onSaved();
    } catch (err) {
      toast(err.message);
    }
  }

  draw();
  return box;
}
