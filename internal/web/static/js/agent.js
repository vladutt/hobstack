import { api } from "./api.js";
import { el, field, toast, agentBody, TOOLS } from "./ui.js";

export async function agentView(root, projectId, agentId) {
  const [project, info] = await Promise.all([api(`/api/projects/${projectId}`), api("/api/info")]);
  const existing = agentId ? project.agents.find((a) => a.id === agentId) : null;
  if (agentId && !existing) throw new Error("Agentul nu exista.");
  const a = existing || {
    name: "", role: "", model: "sonnet", responsibilities: "",
    background: "", instructions: "", doneDef: "", tools: ["write"],
    parentId: project.agents.find((x) => x.isRoot)?.id ?? null, reviewerId: null,
  };
  const others = project.agents.filter((x) => x.id !== agentId);
  const agentOptions = (first, current) => [
    el("option", { value: "" }, first),
    others.map((x) => el("option", { value: x.id }, x.name)),
  ];
  const responsibilities = el("textarea", {
    value: a.responsibilities || "", rows: 3, maxlength: 2000, placeholder: "ex. Endpoint-uri API si migrari",
  });
  const parentSelect = el("select", { value: a.parentId ? String(a.parentId) : "" }, agentOptions("Tu"));
  const reviewerSelect = el("select", { value: a.reviewerId ? String(a.reviewerId) : "" }, agentOptions("nimeni"));
  const idOf = (sel) => (sel.value ? Number(sel.value) : null);
  let isRoot = !parentSelect.value;

  const name = el("input", { type: "text", value: a.name, placeholder: "ex. Rook", required: true, maxlength: 40 });
  const role = el("input", { type: "text", value: a.role, placeholder: "ex. Backend Developer", maxlength: 80 });
  const model = el("select", { value: a.model }, modelOptions(info.models || [], a.model));
  const background = el("textarea", { value: a.background, rows: 5, placeholder: "Cine e agentul si ce stie." });
  const instructions = el("textarea", { value: a.instructions, rows: 5, placeholder: "Cum lucreaza si ce nu are voie sa faca." });
  const doneDef = el("textarea", {
    value: a.doneDef, rows: 3,
    placeholder: "Rezumat, fisiere schimbate, cum se verifica.",
  });
  const doneField = field("", doneDef);
  const doneLabel = doneField.querySelector("label");
  // radacina raporteaza utilizatorului, ceilalti predau sefului
  const syncRoot = () => {
    isRoot = !parentSelect.value;
    doneLabel.textContent = isRoot ? "Rezumatul final: ce iti raporteaza" : "Definition of done: ce ii preda sefului";
    doneDef.placeholder = isRoot ? "Ce contine rezumatul final pentru tine." : "Rezumat, fisiere schimbate, cum se verifica.";
  };
  syncRoot();
  parentSelect.addEventListener("change", () => { syncRoot(); updatePreview(); });
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
        existing && el("button", { type: "button", class: "btn danger", onclick: remove }, "Sterge"),
        el("a", { class: "btn", href: `#/p/${projectId}` }, "Renunta"),
        el("button", { type: "submit", class: "btn primary" }, existing ? "Salveaza" : "Adauga in echipa"))),
    el("div", { class: "form-cols" },
      el("div", { class: "form" },
        el("section", { class: "section" },
          el("h2", {}, "Identitate"),
          field("Nume", name, "Liderul asigneaza task-uri dupa acest nume."),
          field("Rol", role),
          field("Ce ia (responsabilitati)", responsibilities),
          field("Raporteaza lui", parentSelect),
          field("Verificat de", reviewerSelect)),
        el("section", { class: "section" },
          el("h2", {}, "Model"),
          field("Model Claude", model, "\"Cel mai nou\" trece automat pe versiunile noi. O versiune fixa ramane neschimbata. Fable: cel mai capabil; Opus: foarte capabil; Sonnet: echilibrat; Haiku: rapid si ieftin.")),
        el("section", { class: "section" },
          el("h2", {}, "Tool-uri"),
          el("p", { class: "hint" }, "Citirea fisierelor e mereu permisa. git push, rm -rf, sudo si instalarile de pachete sunt blocate ca prefix de comanda; cu Comenzi shell activ, un agent le poate ocoli. Da shell doar agentilor in care ai incredere (aprobarile vin intr-o versiune viitoare)."),
          tools.map((t) => el("label", { class: "check" }, t.box, t.label)))),
      el("div", { class: "form" },
        el("section", { class: "section" },
          el("h2", {}, "Background si instructiuni"),
          field("Background: cine e si ce stie", background),
          field("Instructiuni: cum lucreaza si ce nu face niciodata", instructions),
          doneField),
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
          projectId, name: name.value, role: role.value, agentId: agentId || 0,
          background: background.value, instructions: instructions.value, doneDef: doneDef.value,
          parentId: idOf(parentSelect),
        },
      });
      preview.textContent = res.prompt;
    } catch (err) {
      preview.textContent = err.message;
    }
  }

  async function save(e) {
    e.preventDefault();
    const body = agentBody(a, {
      name: name.value, role: role.value, model: model.value,
      background: background.value, instructions: instructions.value, doneDef: doneDef.value,
      tools: tools.filter((t) => t.box.checked).map((t) => t.id),
      responsibilities: responsibilities.value,
      parentId: idOf(parentSelect), reviewerId: idOf(reviewerSelect),
    });
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

// modelOptions construieste optiunile grupate: alias-uri (mereu cel mai nou)
// si versiuni fixe. Un model salvat care nu mai e in lista ramane selectabil.
function modelOptions(list, current) {
  const group = (label, id) => el("optgroup", { label },
    list.filter((m) => m.group === id).map((m) => el("option", { value: m.id }, m.label)));
  const known = list.some((m) => m.id === current);
  return [
    !known && current && el("option", { value: current }, current + " (nu mai e in lista)"),
    group("Mereu cel mai nou", "latest"),
    group("Versiune fixa", "pinned"),
  ];
}
