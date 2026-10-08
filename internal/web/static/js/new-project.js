import { api, upload } from "./api.js";
import { el, toast, topbar } from "./ui.js";

// Pagina "Proiect nou" (#/new): numele, scopul, folderul, fisierele de pornire si
// setarile echipei in stanga; in dreapta echipa (analiza AI sau doar liderul) si
// proiectele legate. Fisierele si legaturile se trimit dupa ce proiectul exista.
export async function newProjectView(root) {
  const [info, others] = await Promise.all([
    api("/api/info").catch(() => ({ models: [], canPickFolder: false })),
    api("/api/projects").catch(() => []),
  ]);
  const files = [];   // fisierele alese, urcate dupa creare
  const links = [];   // proiectele legate alese ({ id, name, goal })
  let busy = false;

  const name = el("input", { type: "text", class: "np-name", maxlength: 80, required: true, placeholder: "e.g. Meridian Social", "aria-label": "Name" });
  const goal = el("textarea", { rows: 3, maxlength: 2000, placeholder: "e.g. A Laravel package that posts car listings to Facebook and Instagram: one Page per dealer, post on a button, mark “SOLD” when a listing is taken down.", "aria-label": "Goal" });
  const repo = el("input", { type: "text", class: "np-path", placeholder: "code/my-project (empty = a new folder in ~/Foreman)", "aria-label": "Repository" });
  const browse = info.canPickFolder ? el("button", { type: "button", class: "btn", onclick: () => pick(repo, browse).then(syncTilde) }, "Browse...") : null;
  // "~/" din fata campului dispare cand calea e absoluta (aleasa cu Browse sau scrisa)
  const syncTilde = () => repo.parentElement?.classList.toggle("abs", /^[/~]/.test(repo.value.trim()));
  repo.addEventListener("input", syncTilde);
  const extra = el("div", { class: "np-extra" }); // foldere in plus (cai), rar folosite
  const extraInputs = [];
  const context = el("textarea", { rows: 3, maxlength: 20000, placeholder: "Stack, conventions, what is already decided, what is off-limits.", "aria-label": "Shared context" });
  const loadCtx = el("button", { type: "button", class: "link-btn np-load", onclick: loadClaudeMd }, "Load from CLAUDE.md");
  const parallel = el("input", { type: "number", min: 1, max: 100, placeholder: "no limit", "aria-label": "Parallel agents" });
  const budget = el("input", { type: "number", min: 0.05, max: 100, step: 0.05, placeholder: "no limit", "aria-label": "Budget per run (USD)" });
  const autonomy = el("input", { type: "checkbox", role: "switch", class: "switch-input" });
  const autonomyLabel = el("span", {}, "Off");
  autonomy.addEventListener("change", () => { autonomyLabel.textContent = autonomy.checked ? "On" : "Off"; });
  const startNow = el("input", { type: "checkbox", checked: true });
  const note = el("p", { class: "np-note" });
  startNow.addEventListener("change", syncNote);
  goal.addEventListener("input", syncNote);

  // ---------- fisiere ----------
  const fileList = el("div", { class: "np-files" });
  const picker = el("input", { type: "file", multiple: true, hidden: true, onchange: () => { addFiles(picker.files); picker.value = ""; } });
  const drop = el("div", { class: "np-drop", tabindex: "0", role: "button", "aria-label": "Add files",
    onclick: () => picker.click(),
    onkeydown: (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); picker.click(); } } },
  el("span", { class: "np-drop-icon", "aria-hidden": "true" }, "⇧"),
  el("span", {}, "Drop files here or ", el("strong", {}, "browse")),
  el("span", { class: "grow" }),
  el("span", { class: "hint" }, "Agents read them before the first task · you can add more later in Files"),
  picker);
  let depth = 0;
  const hasFiles = (e) => Array.from(e.dataTransfer?.types || []).includes("Files");
  drop.addEventListener("dragenter", (e) => { if (hasFiles(e)) { depth++; drop.classList.add("dropping"); } });
  drop.addEventListener("dragleave", () => { if (--depth <= 0) { depth = 0; drop.classList.remove("dropping"); } });
  drop.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
  drop.addEventListener("drop", (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    depth = 0;
    drop.classList.remove("dropping");
    addFiles(e.dataTransfer.files);
  });
  // un fisier scapat pe langa zona nu are voie sa navigheze browserul
  const guard = (e) => { if (hasFiles(e)) e.preventDefault(); };
  window.addEventListener("dragover", guard);
  window.addEventListener("drop", guard);

  function addFiles(list) {
    for (const f of Array.from(list)) {
      if (!files.some((x) => x.name === f.name && x.size === f.size)) files.push(f);
    }
    drawFiles();
  }
  function drawFiles() {
    fileList.replaceChildren(...files.map((f, i) => {
      const ext = (f.name.includes(".") ? f.name.split(".").pop() : "file").slice(0, 4).toUpperCase();
      return el("div", { class: "np-file" },
        el("span", { class: `np-ext ext-${ext.toLowerCase()}` }, ext),
        el("span", { class: "np-file-main" }, el("span", { class: "np-file-name", title: f.name }, f.name), el("span", { class: "hint" }, size(f.size))),
        el("button", { type: "button", class: "np-x", "aria-label": `Remove ${f.name}`, onclick: () => { files.splice(i, 1); drawFiles(); } }, "×"));
    }));
    fileList.hidden = !files.length;
    syncNote();
  }

  // ---------- proiecte legate ----------
  const linkList = el("div", { class: "np-links" });
  const linkSelect = el("select", { class: "np-link-select", "aria-label": "Link another project", onchange: () => {
    const p = others.find((x) => String(x.id) === linkSelect.value);
    if (p) links.push(p);
    drawLinks();
  } });
  function drawLinks() {
    linkList.replaceChildren(...links.map((p, i) => el("div", { class: "np-link" },
      el("span", { class: "dot", style: "background:var(--teal)" }),
      el("span", { class: "np-link-main" }, el("strong", {}, p.name), p.goal ? el("span", { class: "hint" }, p.goal) : null),
      el("button", { type: "button", class: "np-x", "aria-label": `Unlink ${p.name}`, onclick: () => { links.splice(i, 1); drawLinks(); } }, "×"))));
    const free = others.filter((p) => !links.includes(p));
    linkSelect.replaceChildren(el("option", { value: "" }, free.length ? "↔ Link another project" : "No other projects"),
      ...free.map((p) => el("option", { value: p.id }, p.name)));
    linkSelect.value = "";
    linkSelect.disabled = !free.length;
  }

  // ---------- echipa: analiza AI ----------
  const model = el("select", { class: "select-small np-model", "aria-label": "Model for the analysis" },
    (info.models || []).filter((m) => m.group === "latest").map((m) => el("option", { value: m.id }, m.label.replace(/ \(latest\)$/, ""))));
  if ((info.models || []).some((m) => m.id === "sonnet")) model.value = "sonnet";
  const analyzeBtn = el("button", { type: "button", class: "btn primary", onclick: analyze }, "Analyze & propose team");
  const createBtn = el("button", { type: "submit", form: "new-project", class: "btn primary" }, "Create project");
  const leadOnly = el("button", { type: "submit", form: "new-project", class: "link-btn np-lead-only" }, "Start with just the lead");

  const form = el("form", { id: "new-project", class: "np", onsubmit: create },
    el("div", { class: "np-main" },
      el("div", { class: "field" }, el("label", { class: "label" }, "Name"), name),
      el("div", { class: "field" }, el("label", { class: "label" }, "Goal ", el("span", { class: "np-dim" }, "· what done looks like")), goal),
      el("div", { class: "field" },
        el("label", { class: "label" }, "Repository"),
        el("div", { class: "input-row" }, el("div", { class: "np-path-wrap" }, el("span", { class: "np-tilde", "aria-hidden": "true" }, "~/"), repo), browse),
        extra,
        el("span", { class: "hint" }, "Empty = a new folder is created. If it has code, the AI reads it before proposing the team. Nothing is modified. ",
          el("button", { type: "button", class: "link-btn", onclick: addFolder }, "+ Add another folder"))),
      el("div", { class: "field" },
        el("span", { class: "label" }, "Files · briefs, specs, designs ", el("span", { class: "np-dim" }, "· the team starts from these")),
        fileList, drop),
      el("label", { class: "check np-start" }, startNow,
        el("span", {}, el("strong", {}, "Start working right after creation"),
          el("span", { class: "np-dim" }, " · the lead turns the goal and files into the first tasks"))),
      el("div", { class: "field" },
        el("div", { class: "field-head" }, el("label", { class: "label" }, "Shared context · every agent reads it ", el("span", { class: "np-dim" }, "· optional")), loadCtx),
        context),
      el("div", { class: "field" },
        el("span", { class: "label" }, "How the team works"),
        el("div", { class: "np-tiles" },
          el("label", { class: "np-tile" }, el("span", { class: "np-tile-label" }, "Parallel agents"), parallel),
          el("label", { class: "np-tile" }, el("span", { class: "np-tile-label" }, "Budget per run (USD)"), budget),
          el("label", { class: "np-tile" }, el("span", { class: "np-tile-label" }, "Full autonomy"),
            el("span", { class: "switch" }, autonomy, el("span", { class: "switch-track", "aria-hidden": "true" }), autonomyLabel))),
        el("span", { class: "hint" }, "Autonomy off: agents ask you before deciding. On: the lead decides what you would decide; you still get access requests. Empty limits = no limit; the budget only matters with an API key."))),
    el("aside", { class: "np-side" },
      el("section", { class: "np-team" },
        el("div", { class: "np-team-head" }, el("span", { class: "np-spark", "aria-hidden": "true" }, "✦"), el("strong", {}, "Team"), el("span", { class: "np-dim" }, "· nothing yet")),
        el("p", {}, "Analyze the goal and the repository and get a proposed team: a lead, the specialists it needs, who reviews whom. You edit it before anything is created."),
        el("div", { class: "np-team-actions" }, analyzeBtn, model, el("span", { class: "grow" }), leadOnly),
        el("p", { class: "hint" }, "Reads the repository only; nothing is modified. Takes about a minute. Files and linked projects are added after you create the project from the proposal.")),
      el("section", { class: "np-card" },
        el("div", {}, el("strong", {}, "Linked projects"), el("span", { class: "np-dim" }, " · agents can ask each other")),
        linkList, linkSelect),
      note));

  root.append(
    topbar([["Projects", "#/"], ["New project", null]],
      el("span", { class: "grow" }),
      el("a", { class: "btn ghost", href: "#/" }, "Cancel"),
      createBtn),
    el("div", { class: "page" }, form));
  drawFiles();
  drawLinks();
  syncNote();
  name.focus();

  // ---------- actiuni ----------
  function syncNote() {
    const n = files.length;
    note.replaceChildren(el("span", { class: "dot" + (startNow.checked ? " working" : "") }),
      startNow.checked
        ? `Starts right away: the lead reads the goal${n ? ` and the ${n === 1 ? "file" : n + " files"}` : ""} and opens the first tasks.`
        : "The lead waits for your first message or task.");
  }

  function addFolder() {
    if (extraInputs.length >= 5) { toast("At most 6 folders."); return; }
    const input = el("input", { type: "text", placeholder: "e.g. ~/code/meridian-landing", "aria-label": "Another folder" });
    const btn = info.canPickFolder ? el("button", { type: "button", class: "btn", onclick: () => pick(input, btn) }, "Browse...") : null;
    const row = el("div", { class: "input-row" }, input, btn,
      el("button", { type: "button", class: "btn", "aria-label": "Remove folder", onclick: () => { extraInputs.splice(extraInputs.indexOf(input), 1); row.remove(); } }, "×"));
    extraInputs.push(input);
    extra.append(row);
    input.focus();
  }

  // repoPath: "~/" din fata campului e doar decor; o cale absoluta ramane cum e
  const repoPath = () => {
    const v = repo.value.trim();
    if (!v || v.startsWith("/") || v.startsWith("~")) return v;
    return "~/" + v;
  };
  const folderPaths = () => [repoPath(), ...extraInputs.map((i) => i.value.trim()).filter(Boolean)];

  async function loadClaudeMd() {
    const p = repoPath();
    if (!p) { toast("Choose the repository first."); repo.focus(); return; }
    try {
      const res = await api(`/api/folder-context?path=${encodeURIComponent(p)}`);
      if (!res.found) { toast("There is no CLAUDE.md in this folder."); return; }
      if (context.value.trim() && context.value !== res.content && !confirm("Replace the shared context with the folder's CLAUDE.md?")) return;
      context.value = res.content;
      if (res.truncated) toast("Loaded from CLAUDE.md (cut to 20000 characters).", "ok");
    } catch (err) { toast(err.message); }
  }

  // analyze: porneste analiza AI (wizard-ul) pe folder + scop si deschide pagina ei
  async function analyze() {
    const workdirs = folderPaths().filter(Boolean);
    const description = [name.value.trim(), goal.value.trim()].filter(Boolean).join(": ");
    if (!workdirs.length && !description) { toast("Write the goal or choose the repository first."); goal.focus(); return; }
    if (busy) return;
    busy = true;
    analyzeBtn.disabled = true;
    try {
      const s = await api("/api/setup", { method: "POST", body: { workdirs, description, model: model.value } });
      location.hash = `#/setup/${s.id}`;
    } catch (err) {
      toast(err.message);
      analyzeBtn.disabled = false;
      busy = false;
    }
  }

  // create: proiectul cu liderul implicit, apoi fisierele, legaturile si (optional) pornirea
  async function create(e) {
    e.preventDefault();
    if (busy) return;
    if (!name.value.trim()) { toast("The project needs a name."); name.focus(); return; }
    busy = true;
    createBtn.disabled = leadOnly.disabled = true;
    let p;
    try {
      p = await api("/api/projects", { method: "POST", body: {
        name: name.value.trim(), goal: goal.value.trim(), context: context.value.trim(),
        folders: folderPaths().map((path) => ({ id: 0, name: "", path, context: "" })),
        maxParallel: Number(parallel.value) || 0, budgetUsd: Number(budget.value) || 0, autonomy: autonomy.checked,
      } });
    } catch (err) {
      toast(err.message);
      busy = false;
      createBtn.disabled = leadOnly.disabled = false;
      return;
    }
    // proiectul exista: de aici erorile nu mai opresc nimic, doar se raporteaza
    const problems = [];
    if (files.length) {
      const fd = new FormData();
      for (const f of files) fd.append("files", f, f.name);
      try { await upload(`/api/projects/${p.id}/files`, fd); } catch (err) { problems.push("files: " + err.message); }
    }
    for (const l of links) {
      try { await api(`/api/projects/${p.id}/links`, { method: "POST", body: { projectId: l.id } }); }
      catch (err) { problems.push(`link to ${l.name}: ${err.message}`); }
    }
    if (startNow.checked) {
      try {
        const full = await api(`/api/projects/${p.id}`);
        const lead = full.agents.find((a) => a.isRoot) || full.agents[0];
        await api(`/api/projects/${p.id}/resume`, { method: "POST" });
        if (lead) {
          await api(`/api/agents/${lead.id}/messages`, { method: "POST", body: {
            text: "The project was just created. Read the goal, the shared context" + (files.length ? " and the project files" : "") +
              ", look at the repository, then plan the work and create the first tasks.",
          } });
        }
      } catch (err) { problems.push("start: " + err.message); }
    }
    if (problems.length) toast("Project created, but: " + problems.join("; "));
    location.hash = `#/p/${p.id}/tasks`;
  }

  return () => {
    window.removeEventListener("dragover", guard);
    window.removeEventListener("drop", guard);
  };
}

// pick deschide dialogul nativ si pune calea aleasa in input
async function pick(input, btn) {
  btn.disabled = true;
  try {
    const r = await api("/api/pick-folder", { method: "POST" });
    if (r?.path) input.value = r.path; // 204 = a renuntat
  } catch (err) {
    toast(err.message);
  } finally {
    btn.disabled = false;
  }
}

function size(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
