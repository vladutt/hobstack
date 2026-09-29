import { api } from "./api.js";
import { el, field, toast, MODELS, TOOLS } from "./ui.js";

export async function agentView(root, projectId, agentId) {
  const project = await api(`/api/projects/${projectId}`);
  const existing = agentId ? project.agents.find((a) => a.id === agentId) : null;
  if (agentId && !existing) throw new Error("Agentul nu exista.");
  const a = existing || {
    name: "", role: "", position: "worker", model: "sonnet",
    background: "", instructions: "", doneDef: "", tools: ["write"],
  };
  const isLead = a.position === "lead";

  const name = el("input", { type: "text", value: a.name, placeholder: "ex. Rook", required: true, maxlength: 40 });
  const role = el("input", { type: "text", value: a.role, placeholder: "ex. Backend Developer", maxlength: 80 });
  const model = el("select", { value: a.model }, MODELS.map((m) => el("option", { value: m }, m)));
  const background = el("textarea", { value: a.background, rows: 5, placeholder: "Cine e agentul si ce stie." });
  const instructions = el("textarea", { value: a.instructions, rows: 5, placeholder: "Cum lucreaza si ce nu are voie sa faca." });
  const doneDef = el("textarea", {
    value: a.doneDef, rows: 3,
    placeholder: isLead ? "Ce contine rezumatul final pentru tine." : "Rezumat, fisiere schimbate, cum se verifica.",
  });
  const tools = TOOLS.map((t) => ({ ...t, box: el("input", { type: "checkbox", checked: a.tools.includes(t.id) }) }));
  const preview = el("pre", { class: "preview" }, "...");

  let timer;
  const refresh = () => {
    clearTimeout(timer);
    timer = setTimeout(updatePreview, 300);
  };
  for (const input of [name, role, background, instructions, doneDef]) input.addEventListener("input", refresh);

  root.append(el("form", { class: "form", onsubmit: save },
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" },
          el("a", { href: "#/" }, "Proiecte"), " / ",
          el("a", { href: `#/p/${projectId}` }, project.name), " / ",
          existing ? a.name : "Agent nou"),
        el("h1", {}, existing ? a.name : "Agent nou")),
      el("div", { class: "actions" },
        existing && !isLead && el("button", { type: "button", class: "btn danger", onclick: remove }, "Sterge"),
        el("a", { class: "btn", href: `#/p/${projectId}` }, "Renunta"),
        el("button", { type: "submit", class: "btn primary" }, existing ? "Salveaza" : "Adauga in echipa"))),
    el("div", { class: "form-cols" },
      el("div", { class: "form" },
        el("section", { class: "section" },
          el("h2", {}, "Identitate"),
          field("Nume", name, "Lead-ul asigneaza task-uri dupa acest nume."),
          field("Rol", role),
          el("div", { class: "field" },
            el("span", { class: "label" }, "Pozitie in echipa"),
            el("span", {}, isLead
              ? "Lead: planifica, imparte task-uri si verifica rezultatele."
              : "Worker: primeste task-uri de la lead si raporteaza inapoi."))),
        el("section", { class: "section" },
          el("h2", {}, "Model"),
          field("Model Claude", model, "opus: cel mai capabil. sonnet: echilibrat. haiku: rapid si ieftin.")),
        el("section", { class: "section" },
          el("h2", {}, "Tool-uri"),
          el("p", { class: "hint" }, "Citirea fisierelor e mereu permisa. git push, rm -rf, sudo si instalarile de pachete sunt blocate ca prefix de comanda; cu Comenzi shell activ, un agent le poate ocoli. Da shell doar agentilor in care ai incredere (aprobarile vin intr-o versiune viitoare)."),
          tools.map((t) => el("label", { class: "check" }, t.box, t.label)))),
      el("div", { class: "form" },
        el("section", { class: "section" },
          el("h2", {}, "Background si instructiuni"),
          field("Background: cine e si ce stie", background),
          field("Instructiuni: cum lucreaza si ce nu face niciodata", instructions),
          field(isLead ? "Rezumatul final: ce iti raporteaza" : "Definition of done: ce ii preda lead-ului", doneDef)),
        el("section", { class: "section" },
          el("div", { class: "section-head" }, el("h2", {}, "Prompt compilat"), el("span", { class: "hint" }, "Preview live")),
          preview)))));
  updatePreview();
  if (!existing) name.focus();

  async function updatePreview() {
    try {
      const res = await api("/api/prompt-preview", {
        method: "POST",
        body: {
          projectId, name: name.value, role: role.value, position: a.position,
          background: background.value, instructions: instructions.value, doneDef: doneDef.value,
        },
      });
      preview.textContent = res.prompt;
    } catch (err) {
      preview.textContent = err.message;
    }
  }

  async function save(e) {
    e.preventDefault();
    const body = {
      name: name.value, role: role.value, model: model.value,
      background: background.value, instructions: instructions.value, doneDef: doneDef.value,
      tools: tools.filter((t) => t.box.checked).map((t) => t.id),
    };
    try {
      if (existing) await api(`/api/agents/${agentId}`, { method: "PUT", body });
      else await api(`/api/projects/${projectId}/agents`, { method: "POST", body });
      location.hash = `#/p/${projectId}`;
    } catch (err) {
      toast(err.message);
    }
  }

  async function remove() {
    if (!confirm(`Stergi agentul ${a.name}?`)) return;
    try {
      await api(`/api/agents/${agentId}`, { method: "DELETE" });
      location.hash = `#/p/${projectId}`;
    } catch (err) {
      toast(err.message);
    }
  }

  return () => clearTimeout(timer);
}
