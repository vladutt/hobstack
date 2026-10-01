import { api } from "./api.js";
import { el, avatar, pill, field, toast, kindOf, clock } from "./ui.js";

export async function projectsView(root) {
  const [projects, setups] = await Promise.all([api("/api/projects"), api("/api/setup").catch(() => [])]);
  const grid = el("div", { class: "grid" });
  // cardurile de proiect nu se schimba; doar cele de analiza se redeseneaza
  const fixed = [
    ...projects.map(projectCard),
    el("a", { class: "card card-new", href: "#/setup" },
      el("span", { class: "plus" }, "\u2728"),
      el("strong", {}, "New project with AI"),
      el("span", { class: "hint" }, "Pick a folder; the AI proposes the project and the team")),
    el("a", { class: "card card-new", href: "#/new" },
      el("span", { class: "plus" }, "+"),
      el("strong", {}, "New project"),
      el("span", { class: "hint" }, "Start with a lead, add the team later")),
  ];
  root.append(
    el("header", { class: "page-head" },
      el("div", {},
        el("h1", {}, "Projects"),
        el("p", { class: "muted" }, "Each project has its own team. The lead plans, delegates and reviews.")),
      el("div", { class: "actions" },
        el("a", { class: "btn", href: "#/setup" }, "New project with AI"),
        el("a", { class: "btn primary", href: "#/new" }, "+ New project"))),
    grid);

  // analizele din wizard ruleaza pe server; cat timp una ruleaza, verificam
  // lista la cateva secunde ca sa vedem cand e gata
  let timer = null;
  let closed = false;
  function draw(list) {
    grid.replaceChildren(...list.map((s) => setupCard(s, refresh)), ...fixed);
    clearTimeout(timer);
    if (!closed && list.some((s) => s.status === "running")) timer = setTimeout(refresh, 3000);
  }
  async function refresh() {
    if (closed) return;
    try { draw(await api("/api/setup")); } catch (err) { toast(err.message); }
  }
  draw(setups);
  return () => { closed = true; clearTimeout(timer); };
}

const SETUP_STATUS = {
  running: "Analyzing\u2026", ready: "Ready to review", failed: "Failed", stopped: "Stopped",
};

// setupCard: o analiza "New project with AI" (sau pentru un folder existent,
// cand s.projectId e setat), pornita sau terminata.
function setupCard(s, refresh) {
  const href = `#/setup/${s.id}`;
  const act = (fn) => async (e) => {
    e.target.disabled = true;
    try { await fn(); } catch (err) { toast(err.message); }
    refresh();
  };
  const stop = act(() => api(`/api/setup/${s.id}/stop`, { method: "POST" }));
  const dismiss = act(() => api(`/api/setup/${s.id}`, { method: "DELETE" }));
  // Retry porneste o analiza noua de acelasi fel: pentru un folder existent sau proiect nou
  const retry = act(async () => {
    const next = s.projectId
      ? await api(`/api/projects/${s.projectId}/folders/${s.folderId}/setup`, { method: "POST", body: { description: s.description, model: s.model } })
      : await api("/api/setup", { method: "POST", body: { workdirs: s.workdirs, description: s.description, model: s.model } });
    await api(`/api/setup/${s.id}`, { method: "DELETE" });
    location.hash = `#/setup/${next.id}`;
  });
  const text = s.status === "failed" ? s.error
    : s.proposal?.goal || s.description || (s.workdirs || []).join(", ");
  const buttons = {
    running: [el("a", { class: "btn", href }, "Watch"), el("button", { type: "button", class: "btn", onclick: stop }, "Stop")],
    ready: [el("a", { class: "btn primary", href }, "Review proposal"), el("button", { type: "button", class: "btn", onclick: dismiss }, "Dismiss")],
  }[s.status] || [el("button", { type: "button", class: "btn primary", onclick: retry }, "Retry"),
    el("button", { type: "button", class: "btn", onclick: dismiss }, "Dismiss")];
  const agents = s.proposal?.agents?.length;
  return el("div", { class: `card setup-card setup-${s.status}` },
    el("div", { class: "card-top" },
      el("a", { class: "card-title", href }, s.title || "New project"),
      el("span", { class: `pill pill-setup-${s.status}` }, SETUP_STATUS[s.status] || s.status)),
    el("p", { class: `clamp ${s.status === "failed" ? "err" : "muted"}` }, text),
    el("span", { class: "hint" },
      (s.projectId ? "Agents with AI" : "New project with AI") + " \u00b7 started " + clock(s.createdAt) + (agents ? ` \u00b7 ${agents} agents proposed` : "")),
    el("div", { class: "card-actions" }, buttons));
}

function projectCard(p) {
  const pct = p.total ? Math.round((p.done / p.total) * 100) : 0;
  const asks = (p.openQuestions || 0) + (p.stuckTasks || 0) + (p.accessRequests || 0); // intrebari, task-uri blocate, cereri de acces
  return el("a", { class: "card" + (asks > 0 ? " needs-you-card" : ""), href: `#/p/${p.id}` },
    el("div", { class: "card-top" }, el("span", { class: "card-title" }, p.name),
      el("span", { class: "actions" }, pill(p.status),
        p.autonomy ? el("span", { class: "pill pill-auto", title: "Agents don't ask you questions" }, "Autonomous") : null,
        asks ? el("span", { class: "pill pill-ask" }, `${asks} ${asks === 1 ? "needs" : "need"} you`) : null)),
    el("p", { class: "muted clamp" }, p.goal || "No goal yet."),
    el("div", { class: "avatars" },
      el("span", { class: "stack" }, p.team.map((m) => avatar(m.name, kindOf(m), 28))),
      el("span", { class: "hint" }, p.team.length === 1 ? "1 agent" : `${p.team.length} agents`)),
    el("div", { class: "progress-row" },
      el("span", { class: "hint" }, p.total ? `${p.done} of ${p.total} tasks done` : "No tasks"),
      el("div", { class: "bar" },
        el("div", { class: "bar-fill" + (p.status === "Running" ? " live" : ""), style: `width:${pct}%` }))));
}

export async function projectFormView(root, id) {
  const p = id
    ? await api(`/api/projects/${id}`)
    : { name: "", goal: "", context: "", folders: [], maxParallel: 0, budgetUsd: 0 };

  const name = el("input", { type: "text", value: p.name, placeholder: "e.g. HoReCa QR", required: true, maxlength: 80 });
  const goal = el("textarea", { value: p.goal, placeholder: "An MVP and a clickable prototype for QR sign-ups." });
  const context = el("textarea", { value: p.context, rows: 7, placeholder: "Stack, conventions, what is already decided, what is off-limits." });
  // butonul "Alege..." apare doar daca serverul poate deschide dialogul nativ
  let canPick = false;
  try { canPick = (await api("/api/info")).canPickFolder; } catch { /* fara buton */ }
  // foldere: un proiect poate avea mai multe parti (ex. Client + Landing); primul e implicit
  const folders = foldersEditor(p.folders?.length ? p.folders : [{ id: 0, name: "", path: "", context: "" }],
    canPick, p.agents || [], id);
  // 0 = fara limita; in formular apare ca un camp gol
  const parallel = el("input", { type: "number", min: 1, max: 100, value: p.maxParallel || "", placeholder: "no limit" });
  const budget = el("input", { type: "number", min: 0.05, max: 100, step: 0.05, value: p.budgetUsd || "", placeholder: "no limit" });

  const autonomy = el("input", { type: "checkbox", checked: !!p.autonomy });
  root.append(el("form", { class: "form", onsubmit: save },
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" },
          el("a", { href: "#/" }, "Projects"), " / ",
          id ? el("a", { href: `#/p/${id}` }, p.name) : "New project"),
        el("h1", {}, id ? "Project instructions" : "New project")),
      el("div", { class: "actions" },
        id && el("button", { type: "button", class: "btn danger", onclick: remove }, "Delete project"),
        el("a", { class: "btn", href: id ? `#/p/${id}` : "#/" }, "Cancel"),
        el("button", { type: "submit", class: "btn primary" }, id ? "Save" : "Create project"))),
    el("div", { class: "form-cols" },
      el("section", { class: "section" },
        el("h2", {}, "Brief"),
        field("Name", name),
        field("Goal: what done looks like", goal),
        field("Shared context: every agent reads it", context)),
      el("section", { class: "section" },
        el("h2", {}, "How the team works"),
        el("div", { class: "row" },
          field("Parallel workers", parallel),
          field("Budget per run (USD)", budget)),
        el("p", { class: "hint" }, "Both are optional: leave them empty for no limit. The budget only matters if you use an API key (with a subscription you do not pay per run); when set, a run stops once it reaches it. Agents that report directly to you do not count toward the limit."),
        el("label", { class: "check" }, autonomy, el("span", {}, "Full autonomy")),
        el("p", { class: "hint" }, "Agents don't ask you questions; the root lead decides what you would decide. You still get access requests."),
        !id && el("p", { class: "hint" }, "You get a default lead. Add workers from the project page."))),
    el("section", { class: "section" },
      el("div", { class: "section-head" },
        el("h2", {}, "Folders"),
        el("button", { type: "button", class: "btn small", onclick: () => folders.add() }, "+ Add folder")),
      el("p", { class: "hint" }, "A folder can be a repo or a plain folder. Each agent works and writes in its own folder and can read the others. The first folder is the default."),
      folders.el)));
  name.focus();

  async function save(e) {
    e.preventDefault();
    const body = {
      name: name.value, goal: goal.value, context: context.value, folders: folders.values(),
      maxParallel: Number(parallel.value) || 0, budgetUsd: Number(budget.value) || 0,
      autonomy: autonomy.checked,
    };
    try {
      const saved = id
        ? await api(`/api/projects/${id}`, { method: "PUT", body })
        : await api("/api/projects", { method: "POST", body });
      location.hash = `#/p/${saved.id}`;
    } catch (err) {
      toast(err.message);
    }
  }

  async function remove() {
    if (!confirm(`Delete project ${p.name}? Tasks and conversations will be lost. The working folder is left untouched.`)) return;
    try {
      await api(`/api/projects/${id}`, { method: "DELETE" });
      location.hash = "#/";
    } catch (err) {
      toast(err.message);
    }
  }
}

// foldersEditor: randurile Name / Path / Context din formularul proiectului.
// agents: pentru avertismentul de la stergerea unui folder care are agenti.
// projectId: la un proiect salvat, fiecare folder salvat are "Set up agents with AI".
function foldersEditor(initial, canPick, agents, projectId) {
  const box = el("div", { class: "folder-rows" });
  const rows = [];

  function add(f = { id: 0, name: "", path: "", context: "" }) {
    const name = el("input", { type: "text", value: f.name, maxlength: 40, placeholder: "e.g. Client" });
    const path = el("input", { type: "text", value: f.path,
      placeholder: rows.length ? "e.g. ~/code/probe-landing" : "empty = ~/Foreman/<project name>" });
    const context = el("textarea", { value: f.context, rows: 3, maxlength: 20000,
      placeholder: "Stack, build/test commands, conventions for this folder." });
    const pickBtn = canPick && el("button", { type: "button", class: "btn", onclick: () => pickInto(path, pickBtn) }, "Browse...");
    const r = { id: f.id || 0, name, path, context };
    // contextul se poate copia din CLAUDE.md-ul folderului: automat cand alegi
    // calea si campul e gol, sau oricand din buton (textul ramane editabil)
    const ctxHint = el("span", { class: "hint" });
    const loadBtn = el("button", { type: "button", class: "btn small", onclick: () => loadClaudeMd(true) }, "Load CLAUDE.md");
    async function loadClaudeMd(manual) {
      const p = path.value.trim();
      if (!p) {
        if (manual) toast("Choose the folder path first.");
        return;
      }
      if (!manual && context.value.trim()) return;
      let res;
      try {
        res = await api(`/api/folder-context?path=${encodeURIComponent(p)}`);
      } catch (err) {
        if (manual) toast(err.message);
        return;
      }
      if (!res.found) {
        ctxHint.textContent = manual ? "There is no CLAUDE.md in this folder." : "";
        return;
      }
      const current = context.value.trim();
      if (!manual && current) return; // utilizatorul a scris intre timp
      if (manual && current && current !== res.content && !confirm("Replace the current context with the folder's CLAUDE.md?")) return;
      context.value = res.content;
      ctxHint.textContent = res.truncated ? "Loaded from CLAUDE.md (cut to 20000 characters)." : "Loaded from CLAUDE.md.";
    }
    path.addEventListener("change", () => loadClaudeMd(false));
    const ctxField = field("Context for this folder", context);
    ctxField.insertBefore(el("div", { class: "field-head" }, ctxField.querySelector("label"), loadBtn), context);
    ctxField.append(ctxHint);
    r.removeBtn = el("button", { type: "button", class: "btn small danger", onclick: () => remove(r) }, "Remove folder");
    // doar pentru folderele deja salvate: wizard-ul lucreaza pe folderul din baza de date
    const aiBtn = f.id && projectId ? el("a", { class: "btn small", href: `#/setup/folder/${projectId}/${f.id}`,
      title: "An AI reads this folder and proposes agents for it. Save your changes first." }, "Set up agents with AI") : null;
    r.el = el("div", { class: "folder-row" },
      el("div", { class: "row" }, field("Name", name), field("Path", el("div", { class: "input-row" }, path, pickBtn))),
      ctxField,
      el("div", { class: "actions" }, aiBtn, el("span", { class: "grow" }), r.removeBtn));
    rows.push(r);
    draw();
    return name;
  }

  function remove(r) {
    if (rows.length === 1) return;
    // agentii fara folderId sunt in primul folder: la stergerea lui trec pe urmatorul
    const first = rows.indexOf(r) === 0;
    const n = r.id ? agents.filter((a) => a.folderId === r.id || (first && a.folderId == null)).length : 0;
    const label = r.name.value.trim() || r.path.value.trim() || "this folder";
    if (n && !confirm(`${n === 1 ? "1 agent works" : n + " agents work"} in ${label}. They will move to the first folder. Remove it?`)) return;
    rows.splice(rows.indexOf(r), 1);
    draw();
  }

  function draw() {
    box.replaceChildren(...rows.map((r) => r.el));
    for (const r of rows) r.removeBtn.hidden = rows.length === 1;
  }

  initial.forEach((f) => add(f));
  return {
    el: box,
    // doar randurile adaugate din buton iau focusul (cele initiale nu)
    add: () => add().focus(),
    values: () => rows.map((r) => ({ id: r.id, name: r.name.value.trim(), path: r.path.value.trim(), context: r.context.value })),
  };
}

// pickInto deschide dialogul nativ si pune calea aleasa in input.
async function pickInto(input, btn) {
  btn.disabled = true;
  btn.textContent = "Opening...";
  try {
    const r = await api("/api/pick-folder", { method: "POST" });
    if (r && r.path) {
      input.value = r.path; // 204 = a renuntat
      input.dispatchEvent(new Event("change")); // ca la scrierea de mana (ex. incarca CLAUDE.md)
    }
  } catch (err) {
    toast(err.message);
  } finally {
    btn.disabled = false;
    btn.textContent = "Browse...";
  }
}
