import { api } from "./api.js";
import { el, avatar, field, toast, kindOf, clock, topbar } from "./ui.js";
import { projectCommands } from "./commands.js";
import { libraryInstalls } from "./library-cards.js";
import { ALL, savedCategory, saveCategory, loadCategories, categoryInput } from "./category.js";

export async function projectsView(root) {
  const [projects, setups] = await Promise.all([api("/api/projects"), api("/api/setup").catch(() => [])]);
  const setupBox = el("div", { class: "setups" });
  const search = el("input", {
    type: "search", class: "search", placeholder: "Search projects", "aria-label": "Search projects",
    oninput: () => filter(),
  });
  // favoritele primele; altfel ordinea de pe server (dupa id)
  projects.sort((a, b) => Number(b.favorite) - Number(a.favorite));
  const peak = Math.max(1, ...projects.flatMap((p) => p.activity || [])); // aceeasi scara pe toate randurile
  const rows = projects.map((p) => ({ p, tr: projectRow(p, peak, (c) => pickCategory(c)) }));
  // categoriile: filtre sus si grupuri in tabel, doar cand cel putin un proiect are categorie.
  // "" = fara categorie (ultimul grup); ALL = toate
  const hasCats = projects.some((p) => p.category);
  const cats = [...new Set(projects.map((p) => p.category || ""))]
    .sort((a, b) => (a === "") - (b === "") || a.localeCompare(b, undefined, { sensitivity: "base" }));
  let current = savedCategory();
  if (current !== ALL && !cats.includes(current)) current = ALL;
  const groups = hasCats
    ? cats.map((c) => {
      const members = rows.filter((r) => (r.p.category || "") === c);
      return { c, rows: members, head: el("tr", { class: "p-group" }, el("td", { colspan: 8 },
        el("span", { class: "p-group-name" }, c || "Uncategorized"), el("span", { class: "p-group-count" }, members.length))) };
    })
    : [{ c: null, rows, head: null }];
  const tbody = el("tbody", {}, groups.map((g) => [g.head, g.rows.map((r) => r.tr)]));
  const none = el("tr", { hidden: true }, el("td", { colspan: 8, class: "hint" }, "No project matches."));
  tbody.append(none);
  const chips = hasCats ? el("div", { class: "cat-filter", role: "group", "aria-label": "Categories" }) : null;
  function drawChips() {
    if (!chips) return;
    const opts = [[ALL, "All", projects.length], ...groups.map((g) => [g.c, g.c || "Uncategorized", g.rows.length])];
    chips.replaceChildren(...opts.map(([value, label, n]) => el("button", {
      type: "button", class: "seg" + (value === current ? " active" : ""), "aria-pressed": String(value === current),
      onclick: () => pickCategory(value),
    }, label, el("span", { class: "cat-count" }, n))));
  }
  function pickCategory(value) {
    current = value;
    saveCategory(value);
    drawChips();
    filter();
  }
  function filter() {
    const q = search.value.trim().toLowerCase();
    let shown = 0;
    for (const g of groups) {
      let n = 0;
      for (const r of g.rows) {
        const on = (current === ALL || g.c === current) && (!q || r.p.name.toLowerCase().includes(q) ||
          (r.p.goal || "").toLowerCase().includes(q) || (r.p.category || "").toLowerCase().includes(q));
        r.tr.hidden = !on;
        if (on) n++;
      }
      // titlul grupului doar pe "All": cu o categorie aleasa ar repeta filtrul
      if (g.head) g.head.hidden = !n || current !== ALL;
      shown += n;
    }
    none.hidden = shown > 0;
    // pe "All" fara cautare categoria se vede din titlul grupului; altfel pe fiecare rand
    table?.classList.toggle("grouped", hasCats && current === ALL && !q);
  }
  let table = null;
  drawChips();
  root.append(
    topbar("projects",
      el("span", { class: "grow" }),
      projects.length > 5 || hasCats ? search : null,
      el("a", { class: "btn ghost", href: "#/setup", title: "Pick a folder; the AI proposes the project and the team" }, "\u2726 New project with AI"),
      el("a", { class: "btn primary", href: "#/new" }, "Create project")),
    el("div", { class: "page projects-page" },
      setupBox,
      chips,
      projects.length
        ? (table = el("table", { class: "ptable" },
          el("thead", {}, el("tr", {},
            el("th", { class: "p-star-col", "aria-label": "Favorite" }, "\u2605"),
            el("th", {}, "Name"), el("th", { class: "hide-xs" }, "Lead"), el("th", { class: "hide-sm" }, "Team"),
            el("th", {}, "Progress"), el("th", { class: "hide-md", title: "Runs started per day, today last" }, "Activity \u00b7 7d"),
            el("th", { class: "hide-md" }, "Links"), el("th", { class: "num hide-xs" }, "Total cost"))),
          tbody))
        : el("div", { class: "empty-state" },
          el("h2", {}, "No projects yet"),
          el("p", {}, "Point Foreman at a folder and let the AI propose a team, or start with just a lead."),
          el("div", { class: "actions" },
            el("a", { class: "btn primary", href: "#/setup" }, "\u2726 New project with AI"),
            el("a", { class: "btn", href: "#/new" }, "Create project")))));

  filter(); // dupa ce tabelul e in pagina (clasa "grouped")

  // analizele din wizard ruleaza pe server; cat timp una ruleaza, verificam
  // lista la cateva secunde ca sa vedem cand e gata
  let timer = null;
  let closed = false;
  function draw(list) {
    setupBox.replaceChildren(...list.map((s) => setupCard(s, refresh)));
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
      : await api("/api/setup", { method: "POST", body: { workdirs: s.workdirs, description: s.description, model: s.model, accountId: s.accountId || 0 } });
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

// projectRow: un rand din tabelul de proiecte; click oriunde pe rand deschide proiectul.
// peak: cea mai mare activitate zilnica din tabel (barele au aceeasi scara pe toate randurile;
// radical, ca o zi cu 250 de run-uri sa nu faca invizibile zilele cu 5).
// projectRow: un rand din tabelul Proiecte; onCategory(categorie) la click pe eticheta categoriei
function projectRow(p, peak, onCategory) {
  const pct = p.total ? Math.round((p.done / p.total) * 100) : 0;
  const asks = (p.openQuestions || 0) + (p.stuckTasks || 0) + (p.accessRequests || 0) + (p.agentRequests || 0) + (p.workRequests || 0) + (p.fillIns || 0) + (p.libraryProposals || 0) + (p.accountError ? 1 : 0); // intrebari, task-uri blocate, cereri de acces, cereri de agenti noi, cereri de lucru, contul Claude cazut
  const href = `#/p/${p.id}`;
  const lead = p.team.find((m) => m.isRoot) || p.team[0];
  const shown = p.team.slice(0, 3);
  const state = p.status === "Paused" ? "paused" : p.status === "Running" ? "running" : "idle";
  const star = el("button", { type: "button", class: "p-star" });
  const paintStar = () => {
    star.textContent = p.favorite ? "\u2605" : "\u2606";
    star.classList.toggle("on", !!p.favorite);
    star.setAttribute("aria-pressed", String(!!p.favorite));
    star.setAttribute("aria-label", p.favorite ? `Remove ${p.name} from favorites` : `Add ${p.name} to favorites`);
    star.title = p.favorite ? "Favorite" : "Add to favorites";
  };
  star.addEventListener("click", async () => {
    p.favorite = !p.favorite;
    paintStar();
    try {
      await api(`/api/projects/${p.id}/favorite`, { method: "PUT", body: { favorite: p.favorite } });
    } catch (err) {
      p.favorite = !p.favorite; // inapoi la starea reala
      paintStar();
      toast(err.message);
    }
  });
  paintStar();
  const activity = p.activity || [];
  return el("tr", {
    class: asks > 0 ? "needs-you-row" : null,
    onclick: (e) => { if (!e.target.closest("a, button")) location.hash = href; },
  },
    el("td", { class: "p-star-col" }, star),
    el("td", {}, el("div", { class: "p-name" },
      el("div", { class: "p-title" },
        el("a", { href }, p.name,
          asks ? el("span", { class: "badge-needs-you" }, `${asks} ${asks === 1 ? "needs" : "need"} you`) : null,
          p.autonomy ? el("span", { class: "badge-auto", title: "Agents don't ask you questions" }, "autonomous") : null,
          p.accountName ? el("span", { class: "badge-account", title: "Claude account used by this project" }, p.accountName) : null),
        p.category ? el("button", { type: "button", class: "badge-cat", title: `Show only ${p.category}`,
          onclick: () => onCategory(p.category) }, p.category) : null),
      el("span", { class: "p-sub", title: p.goal || "" },
        el("span", { class: `p-state ${state}` }, p.status), p.goal ? " \u00b7 " + p.goal : ""))),
    el("td", { class: "hide-xs" }, lead ? el("span", { class: "p-lead" }, avatar(lead.name, kindOf(lead), 30), lead.name) : el("span", { class: "hint" }, "\u2014")),
    el("td", { class: "hide-sm" }, p.team.length > 4
      ? el("span", { class: "avatars" }, el("span", { class: "stack" }, shown.map((m) => avatar(m.name, kindOf(m), 28)),
        el("span", { class: "avatar more", style: "--size:28px" }, `+${p.team.length - shown.length}`)))
      : el("span", { class: "stack" }, p.team.map((m) => avatar(m.name, kindOf(m), 28)))),
    el("td", {}, el("div", { class: "p-progress" },
      el("div", { class: "bar" }, el("div", { class: "bar-fill", style: `width:${pct}%` })),
      el("span", {}, p.total ? `${p.done}/${p.total}` : "\u2014"))),
    el("td", { class: "hide-md" }, el("span", {
      class: "p-activity", role: "img",
      "aria-label": `Runs in the last 7 days: ${activity.reduce((a, b) => a + b, 0)}`,
      title: activity.map((n, i) => `${dayLabel(6 - i)}: ${n} ${n === 1 ? "run" : "runs"}`).join("\n"),
    }, activity.map((n) => el("span", { class: "p-day" + (n ? " on" : ""), style: n ? `height:${Math.max(4, Math.round(Math.sqrt(n / peak) * 28))}px` : null })))),
    el("td", { class: "hide-md" }, el("span", { class: "p-links" + (p.links ? " has" : ""), title: p.links ? `${p.links} linked ${p.links === 1 ? "project" : "projects"}` : "No linked projects" }, `\u2194 ${p.links || 0}`)),
    el("td", { class: "num hide-xs" }, p.costUsd > 0 || p.tokens > 0
      ? el("div", { class: "p-cost" }, el("strong", {}, money(p.costUsd)), p.tokens > 0 ? el("span", {}, `${compact(p.tokens)} tokens`) : null)
      : el("span", { class: "hint" }, "\u2014")));
}

// dayLabel: "today", "yesterday" sau data scurta, pentru zilele din activitate
function dayLabel(ago) {
  if (ago === 0) return "Today";
  if (ago === 1) return "Yesterday";
  const d = new Date();
  d.setDate(d.getDate() - ago);
  return d.toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric" });
}

// money: "$3,251.78"
const money = (v) => "$" + (v || 0).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });

// compact: 52400000 -> "52.4M", 910000 -> "910k"
function compact(n) {
  if (n >= 1e9) return (n / 1e9).toFixed(1).replace(/\.0$/, "") + "B";
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1e3) return Math.round(n / 1e3) + "k";
  return String(n);
}

export async function projectFormView(root, id) {
  const p = id
    ? await api(`/api/projects/${id}`)
    : { name: "", goal: "", context: "", folders: [], maxParallel: 0, budgetUsd: 0 };

  const name = el("input", { type: "text", value: p.name, placeholder: "e.g. HoReCa QR", required: true, maxlength: 80 });
  // categoria: una existenta (lista se deschide la focus) sau una noua
  const category = categoryInput(p.category || "", await loadCategories());
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
  // contul Claude: Default (CLI-ul logat) sau un cont din Settings
  let accounts = [];
  let accountsLoaded = false; // fara lista nu stim contul: Save nu trimite accountId (ramane neschimbat)
  try { accounts = await api("/api/accounts"); accountsLoaded = true; } catch { /* doar Default */ }
  const currentAccount = String(p.accountId || 0);
  const account = el("select", { value: currentAccount },
    el("option", { value: "0" }, "Default · the logged-in CLI"),
    accounts.map((a) => el("option", { value: String(a.id) }, a.name)));
  const accountNote = el("p", { class: "hint warn", hidden: true },
    "Agents start new Claude sessions on the new account. Earlier messages stay saved. Stop the team first if it is working.");
  account.addEventListener("change", () => { accountNote.hidden = !id || account.value === currentAccount; });
  const formId = `project-form-${id || "new"}`;
  root.append(
    topbar([["Projects", "#/"], ...(id ? [[p.name, `#/p/${id}`]] : []), [id ? "Instructions" : "New project", null]],
      el("span", { class: "grow" }),
      id && el("button", { type: "button", class: "btn ghost danger", onclick: remove }, "Delete project"),
      el("a", { class: "btn ghost", href: id ? `#/p/${id}` : "#/" }, "Cancel"),
      el("button", { type: "submit", form: formId, class: "btn primary" }, id ? "Save" : "Create project")),
    el("div", { class: "page" },
      el("form", { class: "form project-form", id: formId, onsubmit: save },
        el("div", { class: "form-main" },
          el("section", { class: "section" },
            el("h2", {}, "Brief"),
            field("Name", name),
            field("Category \u00b7 groups projects in the list", category.el),
            field("Goal \u00b7 what done looks like", goal),
            field("Shared context \u00b7 every agent reads it", context)),
          el("section", { class: "section" },
            el("div", { class: "section-head" },
              el("h2", {}, "Folders"),
              el("button", { type: "button", class: "btn small", onclick: () => folders.add() }, "+ Add folder")),
            el("p", { class: "hint" }, "A folder can be a repo or a plain folder. Each agent works and writes in its own folder and can read the others. The first folder is the default."),
            folders.el)),
        el("div", { class: "form-side" },
          el("section", { class: "section" },
            el("h2", {}, "How the team works"),
            el("div", { class: "row" },
              field("Parallel workers", parallel),
              field("Budget per run (USD)", budget)),
            el("p", { class: "hint" }, "Both are optional: leave them empty for no limit. The budget only matters if you use an API key (with a subscription you do not pay per run); when set, a run stops once it reaches it. Agents that report directly to you do not count toward the limit."),
            field("Claude account", account, accounts.length ? "Which subscription the agents use." : "Add other accounts in Settings."),
            accountNote,
            el("label", { class: "check toggle-row" }, autonomy,
              el("span", { class: "grow" }, el("strong", {}, "Full autonomy"),
                el("span", { class: "hint" }, "Agents don't ask you questions; the root lead decides what you would decide. You still get access requests."))),
            !id && el("p", { class: "hint" }, "You get a default lead. Add workers from the project page.")),
          id && linksSection(id, p.links || []),
          id && projectCommands(id),
          id && libraryInstalls(id)))));
  name.focus();

  async function save(e) {
    e.preventDefault();
    const body = {
      name: name.value, goal: goal.value, context: context.value, folders: folders.values(), category: category.input.value.trim(),
      maxParallel: Number(parallel.value) || 0, budgetUsd: Number(budget.value) || 0,
      autonomy: autonomy.checked,
      ...(accountsLoaded && account.value !== "" ? { accountId: Number(account.value) } : {}),
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

// linksSection: proiectele legate (agentii lor isi pot pune intrebari si cere munca).
// Legaturile se salveaza imediat, nu odata cu formularul.
function linksSection(projectId, initial) {
  let links = initial;
  const list = el("div", { class: "link-rows" });
  const select = el("select", {});
  const addBtn = el("button", { type: "button", class: "btn small", onclick: add }, "Link");

  async function refresh() {
    let all = [];
    try { all = await api("/api/projects"); } catch (err) { toast(err.message); }
    const linked = new Set(links.map((l) => l.projectId));
    const free = all.filter((p) => p.id !== projectId && !linked.has(p.id));
    select.replaceChildren(el("option", { value: "" }, free.length ? "Link a project..." : "No other projects"),
      ...free.map((p) => el("option", { value: p.id }, p.name)));
    select.disabled = addBtn.disabled = !free.length;
    list.replaceChildren(...(links.length ? links.map(row) : [el("p", { class: "hint" }, "No linked projects.")]));
  }

  function row(l) {
    return el("div", { class: "link-row" },
      el("a", { href: `#/p/${l.projectId}` }, l.name),
      el("button", { type: "button", class: "btn small danger", onclick: () => remove(l) }, "Remove"));
  }

  async function add() {
    if (!select.value) return;
    try {
      const l = await api(`/api/projects/${projectId}/links`, { method: "POST", body: { projectId: Number(select.value) } });
      links = [...links, l];
      await refresh();
    } catch (err) { toast(err.message); }
  }

  async function remove(l) {
    if (!confirm(`Unlink ${l.name}? Open questions and unapproved requests between the two projects are closed.`)) return;
    try {
      await api(`/api/projects/${projectId}/links/${l.projectId}`, { method: "DELETE" });
      links = links.filter((x) => x.projectId !== l.projectId);
      await refresh();
    } catch (err) { toast(err.message); }
  }

  refresh();
  return el("section", { class: "section" },
    el("h2", {}, "Linked projects", el("span", { class: "hint" }, " \u00b7 agents can ask each other")),
    el("p", { class: "hint" }, "Agents of linked projects can ask each other questions; leads can request work, which you approve in Needs you. Linking or unlinking restarts the agents' Claude sessions on their next run."),
    list,
    el("div", { class: "link-add" }, select, addBtn));
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
