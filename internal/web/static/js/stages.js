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
      el("h2", {}, "Coloane (etape de lucru)"),
      el("span", { class: "hint" }, "Task-ul trece prin etape in ordine; il ia primul agent liber din etapa. Dupa ultima, decide liderul.")),
    rows,
    el("div", { class: "actions" },
      el("button", { type: "button", class: "btn", onclick: () => { list.push({ name: "", kind: "work", agentIds: [] }); draw(); } }, "+ Etapa"),
      el("span", { class: "grow" }),
      el("button", { type: "button", class: "btn", onclick: onClose }, "Renunta"),
      el("button", { type: "button", class: "btn primary", onclick: save }, "Salveaza")));

  function draw() {
    rows.replaceChildren(...list.map((s, i) => {
      const name = el("input", { type: "text", value: s.name, maxlength: 40, placeholder: "ex. Dev", oninput: (e) => { s.name = e.target.value; } });
      const kind = el("select", { value: s.kind, onchange: (e) => { s.kind = e.target.value; } },
        el("option", { value: "work" }, "Lucru"), el("option", { value: "check" }, "Verificare"));
      const agents = project.agents.map((a) => el("label", { class: "check" },
        el("input", { type: "checkbox", checked: s.agentIds.includes(a.id), onchange: (e) => {
          s.agentIds = e.target.checked ? [...s.agentIds, a.id] : s.agentIds.filter((x) => x !== a.id);
        } }), a.name));
      const move = (d) => { const j = i + d; if (j < 0 || j >= list.length) return; [list[i], list[j]] = [list[j], list[i]]; draw(); };
      const locked = s.id && busy.has(s.id);
      return el("div", { class: "stage-row" },
        el("div", { class: "stage-row-head" },
          el("span", { class: "stage-num" }, String(i + 1)), name, kind,
          el("button", { type: "button", class: "btn small", "aria-label": "Mai sus", disabled: i === 0, onclick: () => move(-1) }, "↑"),
          el("button", { type: "button", class: "btn small", "aria-label": "Mai jos", disabled: i === list.length - 1, onclick: () => move(1) }, "↓"),
          el("button", { type: "button", class: "btn small danger", "aria-label": "Sterge etapa", disabled: locked,
            title: locked ? "are task-uri" : "Sterge etapa", onclick: () => { list.splice(i, 1); draw(); } }, "×")),
        el("div", { class: "stage-agents" }, agents.length ? agents : el("span", { class: "hint" }, "Proiectul nu are agenti.")));
    }));
    if (!list.length) rows.append(el("p", { class: "hint" }, "Nicio etapa: board-ul foloseste coloanele implicite (In asteptare, In lucru, Review, Gata)."));
  }

  async function save() {
    try {
      await api(`/api/projects/${project.id}/stages`, { method: "PUT", body: {
        stages: list.map((s) => ({ id: s.id, name: s.name.trim(), kind: s.kind, agentIds: s.agentIds })),
      } });
      toast("Coloane salvate.", "ok");
      onSaved();
    } catch (err) {
      toast(err.message);
    }
  }

  draw();
  return box;
}
