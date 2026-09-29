import { api } from "./api.js";
import { el, field, toast, agentBody, TOOLS } from "./ui.js";

// agentEditor: formularul agentului, afisat in panoul din dreapta al paginii
// "Echipa". Nu schimba pagina: dupa salvare sau stergere cheama onSaved/onDeleted.
// Intoarce { el, isDirty(), destroy() }.
export function agentEditor({ projectId, project, info, agent: a, onSaved, onDeleted }) {
  const others = project.agents.filter((x) => x.id !== a.id);
  const agentOptions = (first) => [
    el("option", { value: "" }, first),
    others.map((x) => el("option", { value: x.id }, x.name)),
  ];
  const responsibilities = el("textarea", {
    value: a.responsibilities || "", rows: 2, maxlength: 2000, placeholder: "ex. Endpoint-uri API si migrari",
  });
  const parentSelect = el("select", { value: a.parentId ? String(a.parentId) : "" }, agentOptions("Tu"));
  const reviewerSelect = el("select", { value: a.reviewerId ? String(a.reviewerId) : "" }, agentOptions("nimeni"));
  const idOf = (sel) => (sel.value ? Number(sel.value) : null);

  const name = el("input", { type: "text", value: a.name, placeholder: "ex. Rook", required: true, maxlength: 40 });
  const role = el("input", { type: "text", value: a.role, placeholder: "ex. Backend Developer", maxlength: 80 });
  const model = el("select", { value: a.model }, modelOptions(info.models || [], a.model));
  const background = el("textarea", { value: a.background, rows: 4, placeholder: "Cine e agentul si ce stie." });
  const instructions = el("textarea", { value: a.instructions, rows: 4, placeholder: "Cum lucreaza si ce nu are voie sa faca." });
  const doneDef = el("textarea", { value: a.doneDef, rows: 3 });
  const doneField = field("", doneDef);
  const doneLabel = doneField.querySelector("label");
  // radacina raporteaza utilizatorului, ceilalti predau sefului
  const syncRoot = () => {
    const isRoot = !parentSelect.value;
    doneLabel.textContent = isRoot ? "Rezumatul final: ce iti raporteaza" : "Definition of done: ce ii preda sefului";
    doneDef.placeholder = isRoot ? "Ce contine rezumatul final pentru tine." : "Rezumat, fisiere schimbate, cum se verifica.";
  };
  syncRoot();
  const tools = TOOLS.map((t) => ({ ...t, box: el("input", { type: "checkbox", checked: a.tools.includes(t.id) }) }));

  // preview-ul prompt-ului se incarca doar cand e deschis
  const preview = el("pre", { class: "preview" }, "...");
  const previewBox = el("details", { class: "preview-box", ontoggle: () => previewBox.open && updatePreview() },
    el("summary", {}, "Prompt compilat"), preview);
  let timer;
  const refresh = () => {
    clearTimeout(timer);
    if (previewBox.open) timer = setTimeout(updatePreview, 300);
  };

  // dirty: utilizatorul a schimbat ceva de la ultima incarcare
  let dirty = false;
  const saveBtn = el("button", { type: "submit", class: "btn primary" }, "Salveaza");
  const form = el("form", { class: "agent-editor", onsubmit: save },
    field("Nume", name, "Liderul asigneaza task-uri dupa acest nume."),
    field("Rol", role),
    field("Model", model, "\"Cel mai nou\" trece automat pe versiunile noi. Fable: cel mai capabil; Opus: foarte capabil; Sonnet: echilibrat; Haiku: rapid si ieftin."),
    field("Ce ia (responsabilitati)", responsibilities),
    el("div", { class: "row" }, field("Raporteaza lui", parentSelect), field("Verificat de", reviewerSelect)),
    el("div", { class: "field" },
      el("span", { class: "label" }, "Tool-uri"),
      tools.map((t) => el("label", { class: "check" }, t.box, t.label)),
      el("span", { class: "hint" }, "Citirea fisierelor e mereu permisa. git push, rm -rf, sudo si instalarile de pachete sunt blocate ca prefix de comanda; cu Comenzi shell activ, un agent le poate ocoli.")),
    field("Background: cine e si ce stie", background),
    field("Instructiuni: cum lucreaza si ce nu face niciodata", instructions),
    doneField,
    previewBox,
    el("div", { class: "editor-actions" },
      el("button", { type: "button", class: "btn danger", onclick: remove }, "Sterge"),
      el("span", { class: "grow" }),
      saveBtn));
  form.addEventListener("input", () => { dirty = true; refresh(); });
  form.addEventListener("change", () => { dirty = true; syncRoot(); refresh(); });

  async function updatePreview() {
    try {
      const res = await api("/api/prompt-preview", {
        method: "POST",
        body: {
          projectId, name: name.value, role: role.value, agentId: a.id,
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
    saveBtn.disabled = true;
    try {
      await api(`/api/agents/${a.id}`, { method: "PUT", body });
      dirty = false;
      toast("Salvat.", "ok");
      await onSaved();
    } catch (err) {
      toast(err.message);
    } finally {
      saveBtn.disabled = false;
    }
  }

  async function remove() {
    if (!confirm(`Stergi agentul ${a.name}?`)) return;
    try {
      await api(`/api/agents/${a.id}`, { method: "DELETE" });
      dirty = false;
      await onDeleted();
    } catch (err) {
      toast(err.message);
    }
  }

  return {
    el: form,
    isDirty: () => dirty,
    focusName() { name.focus(); name.select(); },
    destroy() { clearTimeout(timer); },
  };
}

// editableKey: semnatura campurilor editabile; daca se schimba (ex. o legatura
// trasa pe canvas), formularul nemodificat se reconstruieste cu datele noi.
export function editableKey(a, agents) {
  return JSON.stringify([agentBody(a), agents.map((x) => [x.id, x.name])]);
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
