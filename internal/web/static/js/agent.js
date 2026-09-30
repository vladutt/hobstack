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
    value: a.responsibilities || "", rows: 2, maxlength: 2000, placeholder: "e.g. API endpoints and migrations",
  });
  // "" = Tu (radacina), "none" = nelegat, altfel id-ul sefului
  const parentSelect = el("select", { value: a.parentId ? String(a.parentId) : a.detached ? "none" : "" },
    el("option", { value: "none" }, "nobody (detached)"), agentOptions("You"));
  const reviewerSelect = el("select", { value: a.reviewerId ? String(a.reviewerId) : "" }, agentOptions("nobody"));
  // folderul in care lucreaza agentul; apare doar cand proiectul are mai multe
  const folders = project.folders || [];
  const folderSelect = folders.length > 1 && el("select", { value: String(a.folderId ?? folders[0].id) },
    folders.map((f) => el("option", { value: f.id }, f.name)));
  // primul folder se salveaza ca null (= implicit), ca in store
  const folderValue = () => {
    if (!folderSelect) return a.folderId ?? null;
    const id = Number(folderSelect.value);
    return id === folders[0].id ? null : id;
  };
  const idOf = (sel) => (sel.value && sel.value !== "none" ? Number(sel.value) : null);

  const name = el("input", { type: "text", value: a.name, placeholder: "e.g. Rook", required: true, maxlength: 40 });
  const role = el("input", { type: "text", value: a.role, placeholder: "e.g. Backend Developer", maxlength: 80 });
  const model = el("select", { value: a.model }, modelOptions(info.models || [], a.model));
  const background = el("textarea", { value: a.background, rows: 4, placeholder: "Who the agent is and what it knows." });
  const instructions = el("textarea", { value: a.instructions, rows: 4, placeholder: "How it works and what it must never do." });
  const doneDef = el("textarea", { value: a.doneDef, rows: 3 });
  const doneField = field("", doneDef);
  const doneLabel = doneField.querySelector("label");
  // radacina raporteaza utilizatorului, ceilalti predau sefului
  const syncRoot = () => {
    const isRoot = !parentSelect.value;
    doneLabel.textContent = isRoot ? "Final summary: what it reports to you" : "Definition of done: what it hands to its boss";
    doneDef.placeholder = isRoot ? "What the final summary for you contains." : "Summary, changed files, how to verify.";
  };
  syncRoot();
  const tools = TOOLS.map((t) => ({ ...t, box: el("input", { type: "checkbox", checked: a.tools.includes(t.id) }) }));

  // preview-ul prompt-ului se incarca doar cand e deschis
  const preview = el("pre", { class: "preview" }, "...");
  const previewBox = el("details", { class: "preview-box", ontoggle: () => previewBox.open && updatePreview() },
    el("summary", {}, "Compiled prompt"), preview);
  let timer;
  const refresh = () => {
    clearTimeout(timer);
    if (previewBox.open) timer = setTimeout(updatePreview, 300);
  };

  // dirty: utilizatorul a schimbat ceva de la ultima incarcare
  let dirty = false;
  const saveBtn = el("button", { type: "submit", class: "btn primary" }, "Save");
  const form = el("form", { class: "agent-editor", onsubmit: save },
    field("Name", name, "The lead assigns tasks by this name."),
    field("Role", role),
    field("Model", model, "\"Latest\" moves to new versions automatically. Fable: most capable; Opus: very capable; Sonnet: balanced; Haiku: fast and cheap."),
    field("Takes (responsibilities)", responsibilities),
    el("div", { class: "row" }, field("Reports to", parentSelect), field("Reviewed by", reviewerSelect)),
    folderSelect && field("Folder", folderSelect, "The agent works and writes here; it can read the other folders."),
    el("div", { class: "field" },
      el("span", { class: "label" }, "Tools"),
      tools.map((t) => el("label", { class: "check" }, t.box, t.label)),
      el("span", { class: "hint" }, "Write files: only inside the agent's folder. Reading files is always allowed. git push, rm -rf, sudo and package installs are blocked as command prefixes; with Shell commands enabled, an agent can work around them.")),
    field("Background: who it is and what it knows", background),
    field("Instructions: how it works and what it never does", instructions),
    doneField,
    previewBox,
    el("div", { class: "editor-actions" },
      el("button", { type: "button", class: "btn danger", onclick: remove }, "Delete"),
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
          parentId: idOf(parentSelect), folderId: folderValue(),
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
      detached: parentSelect.value === "none",
      folderId: folderValue(),
    });
    saveBtn.disabled = true;
    try {
      await api(`/api/agents/${a.id}`, { method: "PUT", body });
      dirty = false;
      toast("Saved.", "ok");
      await onSaved();
    } catch (err) {
      toast(err.message);
    } finally {
      saveBtn.disabled = false;
    }
  }

  async function remove() {
    if (!confirm(`Delete agent ${a.name}?`)) return;
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
    !known && current && el("option", { value: current }, current + " (no longer listed)"),
    group("Always latest", "latest"),
    group("Pinned version", "pinned"),
  ];
}
