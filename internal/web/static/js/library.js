import { api } from "./api.js";
import { el, avatar, field, toast, topbar } from "./ui.js";

// Paginile Library: #/library/tools, #/library/skills, #/library/mcp si pagina unui element,
// #/library/item/<id>. Tot textul vine din server (scris de agenti): se pune doar prin el() / textContent.

const KINDS = {
  tool: { path: "tools", title: "Tools", one: "tool", add: "+ New tool", search: "Search tools…" },
  skill: { path: "skills", title: "Skills", one: "skill", add: "+ New skill", search: "Search skills…" },
  mcp: { path: "mcp", title: "MCP", one: "server", add: "+ Add server", search: "Search servers…" },
};
const KIND_BY_PATH = { tools: "tool", skills: "skill", mcp: "mcp" };
const NEEDS = {
  network: "Network", write: "Write files", browser: "Browser", shell: "Shell commands",
};

// ago: "2 h ago", "yesterday", "Sep 28"
function ago(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  const mins = Math.max(0, (Date.now() - d.getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${Math.floor(mins)} min ago`;
  if (mins < 60 * 24) return `${Math.floor(mins / 60)} h ago`;
  if (mins < 60 * 48) return "yesterday";
  return d.toLocaleDateString("en-GB", { month: "short", day: "numeric" });
}

const needsText = (it) => [
  ...(it.needs || []).map((n) => "needs " + (NEEDS[n] || n).toLowerCase()),
  ...(it.secrets || []).map((s) => "secret " + s),
].join(", ");

// topNav: titlurile mari Tools / Skills / MCP (linkuri)
function titles(active) {
  return el("div", { class: "lib-titles" },
    Object.entries(KINDS).map(([k, c]) =>
      el("a", { href: `#/library/${c.path}`, class: "lib-title" + (k === active ? " active" : ""), "aria-current": k === active ? "page" : null }, c.title)));
}

// busy: pune butonul pe "ocupat" cat dureaza actiunea; erorile apar ca toast (422 = mesajul de reparat)
export async function run(btn, label, fn, onError) {
  const text = btn.textContent;
  btn.disabled = true;
  btn.classList.add("busy");
  btn.textContent = label;
  try {
    await fn();
    return true;
  } catch (err) {
    if (onError) onError(err); else toast(err.message);
    return false;
  } finally {
    btn.disabled = false;
    btn.classList.remove("busy");
    btn.textContent = text;
  }
}

// commandOf: linia de comanda a unui server MCP local ("" pentru restul). Comanda e aleasa de agent
// si rulata o data pe masina utilizatorului la Publish / Keep, deci apare pe orice card.
export const commandOf = (it) => (it.kind === "mcp" && it.transport === "local" ? [it.command, ...(it.args || [])].join(" ") : "");

export function cmdBox(it) {
  const cmd = commandOf(it);
  return cmd ? el("div", { class: "lib-cmd" }, el("span", { class: "hint" }, "Runs on your machine: "), el("code", { class: "mono" }, cmd)) : null;
}

// testedAction: Publish / Keep. La un server local cere confirmarea cu comanda exacta; daca testul pica,
// arata eroarea si "Publish without test" (tot cu confirmare), care trimite skipTest.
async function testedAction(it, btn, { verb, busy, path, body, success, onDone }) {
  const cmd = commandOf(it);
  if (cmd && !confirm(`${verb}ing runs this command once on your machine to test it: ${cmd}. Continue?`)) return;
  const host = btn.parentElement;
  const clear = () => host?.parentElement?.querySelectorAll(":scope > .lib-fail").forEach((n) => n.remove());
  const attempt = async (button, skipTest) => {
    clear();
    const payload = skipTest ? { ...body, skipTest: true } : body;
    const ok = await run(button, cmd && !skipTest ? "Testing…" : busy, () => api(path, { method: "POST", body: payload }), (err) => {
      if (!cmd) { toast(err.message); return; }
      const skip = el("button", { type: "button", class: "btn small ghost" }, `${verb} without test`);
      skip.addEventListener("click", () => {
        if (confirm(`${verb} without testing? Agents will run this command unverified: ${cmd}. Continue?`)) attempt(skip, true);
      });
      host?.insertAdjacentElement("afterend", el("div", { class: "lib-fail" },
        el("span", { class: "lib-fail-msg" }, err.message), skip));
    });
    if (ok) { clear(); toast(success, "ok"); onDone?.(); }
  };
  await attempt(btn, false);
}

export function publishItem(it, btn, onDone) {
  return testedAction(it, btn, { verb: "Publish", busy: "Publishing…", path: `/api/library/${it.id}/publish`, body: {},
    success: `${it.name} is in the library`, onDone });
}

export function keepItem(it, btn, onDone) {
  return testedAction(it, btn, { verb: "Keep", busy: "Keeping…", path: `/api/library/${it.id}/decline`, body: { keep: true },
    success: `${it.name} kept in the project`, onDone });
}

function publishButton(it, onDone, cls = "btn primary") {
  const btn = el("button", { class: cls, type: "button" }, "Publish");
  btn.addEventListener("click", () => publishItem(it, btn, onDone));
  return btn;
}

function declineButtons(it, projects, onDone) {
  const decline = el("button", { class: "btn ghost", type: "button" }, "Decline");
  decline.addEventListener("click", async () => {
    if (await run(decline, "Declining…", () => api(`/api/library/${it.id}/decline`, { method: "POST", body: { keep: false } }))) onDone();
  });
  const out = [decline];
  if (it.sourceProjectId) {
    const name = projects.get(it.sourceProjectId) || "the project";
    const keep = el("button", { class: "btn ghost keep", type: "button", title: "Not shared: it stays installed only in this project" }, `Keep in ${name}`);
    keep.addEventListener("click", () => keepItem(it, keep, onDone));
    out.push(keep);
  }
  return out;
}

const reviewLink = (it) => el("a", { class: "btn ghost", href: `#/library/item/${it.id}?tab=code` }, "Review code");

export function chips(it) {
  return [
    ...(it.needs || []).map((n) => el("span", { class: "chip" }, NEEDS[n] || n)),
    ...(it.secrets || []).map((s) => el("span", { class: "chip secret" }, s)),
  ];
}

// ---------- pagina de lista ----------

export async function libraryView(root, path) {
  const kind = KIND_BY_PATH[path] || "tool";
  const cfg = KINDS[kind];
  let items, projectList;
  try {
    [items, projectList] = await Promise.all([
      api(`/api/library?kind=${kind}`),
      api("/api/projects").catch(() => []),
    ]);
  } catch (err) {
    return notFound(root, `Could not load the library: ${err.message}`, cfg);
  }
  const projects = new Map(projectList.map((p) => [p.id, p.name]));
  let filter = "all";
  let query = "";
  let selected = null; // skills: elementul din panoul din dreapta
  const body = el("div", { class: "page lib-page" });
  const filters = kind === "mcp" ? [["all", "All"], ["agents", "By agents"], ["external", "External"]]
    : [["all", "All"], ["mine", "Mine"], ["recent", "Recently added"]];

  const search = el("input", {
    type: "search", class: "search", placeholder: cfg.search, "aria-label": cfg.search,
    oninput: () => { query = search.value.trim().toLowerCase(); draw(); },
  });
  const nameKind = kind === "mcp" ? "server" : kind;
  root.append(
    topbar("library", el("span", { class: "grow" }), search,
      el("button", { class: "btn primary", type: "button", onclick: () => newForm(kind, reload) }, cfg.add)),
    body);

  let closed = false;
  async function reload() {
    if (closed) return;
    try {
      const fresh = await api(`/api/library?kind=${kind}`);
      items.splice(0, items.length, ...fresh);
      draw();
    } catch (err) { toast(err.message); }
  }

  const mine = (it) => it.madeByAgentId === 0;
  const matches = (it) => !query || it.name.toLowerCase().includes(query) || (it.description || "").toLowerCase().includes(query)
    || (it.madeBy || "").toLowerCase().includes(query);
  function pass(it) {
    if (!matches(it)) return false;
    if (filter === "mine") return mine(it);
    if (filter === "agents") return !mine(it);
    if (filter === "external") return mine(it);
    return true;
  }
  function draw() {
    const proposed = items.filter((it) => it.status === "proposed").sort((a, b) => b.createdAt.localeCompare(a.createdAt));
    let published = items.filter((it) => it.status === "published");
    const total = published.length;
    if (filter === "recent") published.sort((a, b) => (b.publishedAt || "").localeCompare(a.publishedAt || ""));
    published = published.filter(pass);
    if (filter === "recent") published = published.slice(0, 10);
    const props = proposed.filter(pass);
    const sub = {
      tool: `Made by agents in one project, shared with every project. ${total} ${total === 1 ? "tool" : "tools"}`,
      skill: `Instructions and checklists agents load when a task needs them. ${total} ${total === 1 ? "skill" : "skills"}`,
      mcp: `Servers that give agents live access to systems. ${total} ${total === 1 ? "server" : "servers"}`,
    }[kind] + (proposed.length ? (kind === "tool" ? ` · ${proposed.length} waiting for you` : ` · ${proposed.length} proposed`) : "");

    const head = el("div", { class: "lib-head" },
      el("div", { class: "lib-head-main" }, titles(kind), el("p", { class: "lib-sub" }, sub)),
      el("div", { class: "lib-filters", role: "group", "aria-label": "Filter" },
        filters.map(([id, label]) => el("button", {
          type: "button", class: "lib-filter", "aria-pressed": id === filter ? "true" : "false",
          onclick: () => { filter = id; draw(); },
        }, label))));

    if (kind === "skill") {
      const all = [...props, ...published];
      if (!selected || !all.some((x) => x.id === selected)) selected = all[0]?.id ?? null;
      const cur = all.find((x) => x.id === selected);
      body.replaceChildren(head,
        el("div", { class: "lib-skills" },
          el("div", { class: "lib-grid" }, all.length ? all.map((it) => skillCard(it)) : emptyNote(items.length)),
          cur ? skillPanel(cur) : null));
      return;
    }
    body.replaceChildren(head,
      props.length ? el("section", { class: "lib-proposed" },
        el("h2", { class: "lib-need" }, el("span", { class: "dot-amber" }), "Proposed · needs you"),
        el("div", { class: kind === "mcp" ? "lib-cards one" : "lib-cards" }, props.map((it) => kind === "mcp" ? mcpProposal(it) : toolProposal(it)))) : null,
      published.length ? table(published) : (props.length ? null : emptyNote(items.length)));
  }

  function emptyNote(any) {
    return el("div", { class: "empty-state" },
      el("h2", {}, any ? `No ${nameKind} matches` : `No ${cfg.title === "MCP" ? "servers" : cfg.title.toLowerCase()} yet`),
      el("p", {}, kind === "mcp" ? "Add a remote or local MCP server, or let an agent propose one."
        : `Add one yourself, or let an agent propose one from its work.`));
  }

  const done = () => reload();

  function toolProposal(it) {
    return el("article", { class: "np-card lib-prop" },
      el("div", { class: "lib-prop-top" },
        el("a", { class: "lib-name", href: `#/library/item/${it.id}` }, it.name),
        el("span", { class: "label-tag" }, "script"),
        el("span", { class: "grow" }),
        el("span", { class: "hint" }, ago(it.createdAt))),
      el("p", { class: "lib-desc" }, it.description),
      el("div", { class: "lib-prop-foot" },
        el("span", { class: "lib-by" }, avatar(it.madeBy, "worker", 26), el("span", {}, it.madeBy),
          needsText(it) ? el("span", { class: "hint" }, "· " + needsText(it)) : null),
        el("span", { class: "grow" }),
        el("div", { class: "lib-actions" }, reviewLink(it), declineButtons(it, projects, done), publishButton(it, done, "btn primary small"))));
  }

  function mcpProposal(it) {
    const local = it.transport === "local";
    return el("article", { class: "np-card lib-prop mcp-prop" },
      el("div", { class: "lib-prop-col" },
        el("div", { class: "lib-prop-top" },
          el("a", { class: "lib-name", href: `#/library/item/${it.id}` }, it.name),
          el("span", { class: "label-tag by-agent" }, "by agent"),
          el("span", { class: "grow" }),
          el("span", { class: "lib-flag" }, "Proposed")),
        el("p", { class: "lib-desc" }, it.description),
        el("p", { class: "lib-tested untested" }, el("span", { class: "dot-grey" }),
          local ? "Tested when you publish" : `Not tested \u00b7 remote ${it.url}`),
        cmdBox(it),
        el("div", { class: "lib-actions" }, publishButton(it, done), reviewLink(it),
          el("span", { class: "grow" }), declineButtons(it, projects, done))),
      el("div", { class: "lib-prop-col" },
        el("h3", { class: "lib-h" }, "Exposes"),
        (it.toolNames || []).length
          ? el("pre", { class: "md-pre lib-exposes" }, it.toolNames.join("\n"))
          : el("p", { class: "hint" }, local ? "The tool list is known after publishing." : "Known when an agent connects.")),
      el("div", { class: "lib-prop-col" },
        el("h3", { class: "lib-h" }, "Each project provides"),
        el("div", { class: "chips" }, (it.secrets || []).length || (it.needs || []).length ? chips(it) : el("span", { class: "hint" }, "Nothing"))));
  }

  function table(rows) {
    const go = (it) => { location.hash = `#/library/item/${it.id}`; };
    const row = (it) => {
      const tr = el("tr", { tabindex: "0", onclick: () => go(it), onkeydown: (e) => { if (e.key === "Enter") go(it); } });
      const name = el("td", {}, el("span", { class: "lib-name-cell" },
        el("a", { class: "lib-name small", href: `#/library/item/${it.id}`, onclick: (e) => e.stopPropagation() }, it.name),
        kind === "mcp" ? el("span", { class: "label-tag" + (mine(it) ? "" : " by-agent") }, mine(it) ? "external" : "by agent")
          : el("span", { class: "label-tag" }, "script")));
      const desc = el("td", { class: "lib-cell-desc" }, it.description);
      if (kind === "mcp") {
        tr.append(name, desc, el("td", {}, it.transport === "remote" ? "Remote" : "Local"),
          el("td", { class: "muted" }, (it.toolNames || []).length || "–"), el("td", {}, it.madeBy));
      } else {
        tr.append(name, desc, el("td", {}, it.madeBy), el("td", { class: "num mono muted" }, it.version ? "v" + it.version.version : ""));
      }
      return tr;
    };
    return el("table", { class: "ptable lib-table" },
      el("thead", {}, el("tr", {}, kind === "mcp"
        ? [el("th", {}, "Server"), el("th", {}, "Description"), el("th", {}, "Runs"), el("th", {}, "Tools"), el("th", {}, "Made by")]
        : [el("th", {}, "Tool"), el("th", {}, "What it does"), el("th", {}, "Made by"), el("th", { class: "num" }, "Version")])),
      el("tbody", {}, rows.map(row)));
  }

  function skillCard(it) {
    const meta = it.version ? (it.version.files > 1 ? `SKILL.md + ${it.version.files - 1} ${it.version.files === 2 ? "file" : "files"}` : `SKILL.md · ${it.version.lines} lines`) : "SKILL.md";
    return el("button", {
      type: "button", class: "lib-skill" + (it.id === selected ? " selected" : "") + (it.status === "proposed" ? " proposed" : ""),
      onclick: () => { selected = it.id; draw(); },
    },
      el("span", { class: "lib-skill-top" }, el("span", { class: "lib-icon" }, "✦"), el("span", { class: "lib-skill-name" }, it.name),
        el("span", { class: "lib-ver" + (it.status === "proposed" ? " flag" : "") }, it.status === "proposed" ? "Proposed" : it.version ? "v" + it.version.version : "")),
      el("span", { class: "lib-skill-desc" }, it.description),
      el("span", { class: "lib-skill-foot" }, el("span", { class: "lib-by" }, avatar(it.madeBy, "worker", 24), el("span", {}, it.madeBy)),
        el("span", { class: "mono muted" }, meta)));
  }

  function skillPanel(it) {
    const pre = el("pre", { class: "md-pre lib-preview" }, "Loading…");
    fetch(`/api/library/${it.id}/files/SKILL.md`).then((r) => r.ok ? r.text() : "").then((t) => {
      const lines = t.replace(/\n$/, "").split("\n");
      pre.textContent = t ? lines.slice(0, 12).join("\n") + (lines.length > 12 ? `\n… ${lines.length - 12} more lines` : "") : "No preview.";
    }).catch(() => { pre.textContent = "No preview."; });
    const proposed = it.status === "proposed";
    return el("aside", { class: "lib-panel" },
      el("div", { class: "lib-panel-head" }, el("span", { class: "lib-icon" }, "✦"),
        el("a", { class: "lib-panel-name", href: `#/library/item/${it.id}` }, it.name),
        el("span", { class: "lib-ver" + (proposed ? " flag" : "") }, proposed ? "Proposed" : it.version ? "v" + it.version.version : "")),
      pre,
      el("div", { class: "chips" }, chips(it).length ? chips(it) : el("span", { class: "chip" }, "No tools needed")),
      el("div", { class: "lib-actions panel" },
        proposed ? publishButton(it, done) : el("a", { class: "btn primary", href: `#/library/item/${it.id}` }, "Open"),
        reviewLink(it),
        el("span", { class: "grow" }),
        proposed ? declineButtons(it, projects, done) : null));
  }

  draw();
  return () => { closed = true; };
}

// notFound: bara de sus + stare goala cu drum inapoi la lista
function notFound(root, message, cfg) {
  root.append(topbar("library"),
    el("div", { class: "page" }, el("div", { class: "empty-state" },
      el("h2", {}, message),
      el("a", { class: "btn", href: `#/library/${cfg.path}` }, `Back to ${cfg.title}`))));
}

// ---------- pagina unui element ----------

export async function libraryItemView(root, id, tab) {
  let d;
  try {
    d = await api(`/api/library/${id}`);
  } catch (err) {
    return notFound(root, "Item not found", KINDS.tool);
  }
  const cfg = KINDS[d.kind];
  let current = ["usage", "code", "versions"].includes(tab) ? tab : "usage";
  const kindWord = { tool: "Script", skill: "Skill", mcp: "MCP server" }[d.kind];
  const proposed = d.status === "proposed";
  const projectNames = new Map();
  if (d.sourceProjectId) {
    const ps = await api("/api/projects").catch(() => []);
    ps.forEach((p) => projectNames.set(p.id, p.name));
  }

  const tabs = [["usage", "Usage"], ["code", "Code"], ["versions", "Versions"]];
  const panel = el("div", { class: "lib-tabpanel" });
  const tabBar = el("div", { class: "modal-tabs", role: "tablist" });
  function drawTabs() {
    tabBar.replaceChildren(...tabs.map(([k, label]) => el("button", {
      type: "button", class: "tab", role: "tab", "aria-selected": k === current ? "true" : "false",
      onclick: () => { current = k; drawTabs(); },
    }, label, k === "versions" ? el("span", { class: "count" }, String(d.versions.length)) : null)));
    panel.replaceChildren(current === "usage" ? usage() : current === "code" ? code() : versions());
  }

  function usage() {
    const box = el("div", { class: "lib-usage" });
    if (d.kind === "tool") {
      // aceeasi forma ca in buildTool: <build>/bin/<name>/<entry>, sau .../run cand tool-ul are secrete
      const cmd = (d.secrets || []).length ? "run" : d.entry;
      box.append(el("h3", { class: "lib-h" }, "How agents call it"),
        el("pre", { class: "md-pre lib-call" }, `.../bin/${d.name}/${cmd} <arguments>`),
        el("p", { class: "hint" }, "Agents get the full path in their turn message; tools with secrets run through a wrapper that adds them. Install it in a project under Instructions \u203a Library."));
    } else if (d.kind === "skill") {
      box.append(el("h3", { class: "lib-h" }, "How agents use it"),
        el("p", { class: "lib-text" }, "Agents in projects that installed this skill load SKILL.md when a task matches its description."));
    } else {
      box.append(el("h3", { class: "lib-h" }, "Connection"),
        el("pre", { class: "md-pre lib-call" }, d.transport === "remote" ? d.url : commandOf(d)),
        d.transport === "remote" ? null : el("p", { class: "hint" }, "This command runs on your machine, once to test it when you publish and then whenever an agent in a project that installed it starts."),
        (d.toolNames || []).length ? el("h3", { class: "lib-h" }, "Exposes") : null,
        (d.toolNames || []).length ? el("pre", { class: "md-pre lib-call" }, d.toolNames.join("\n")) : null);
    }
    box.append(el("h3", { class: "lib-h spaced" }, "Recent use"),
      d.recentUses.length
        ? el("ul", { class: "lib-uses" }, d.recentUses.map((u) => el("li", {},
          el("span", { class: "mono muted" }, `T-${u.number}`), el("span", { class: "lib-use-title" }, u.title), el("span", { class: "muted" }, u.agent))))
        : el("p", { class: "hint" }, "Not used yet."));
    if (d.installedIn.length) {
      box.append(el("h3", { class: "lib-h spaced" }, "Installed in"),
        el("ul", { class: "lib-uses" }, d.installedIn.map((i) => el("li", {},
          el("a", { href: `#/p/${i.projectId}` }, i.project || `Project ${i.projectId}`),
          el("span", { class: "muted" }, "v" + i.version), el("span", { class: "muted" }, i.enabled ? "on" : "off")))));
    }
    return box;
  }

  function code() {
    const box = el("div", { class: "lib-code" });
    if (!d.files.length) return el("div", {}, cmdBox(d), el("p", { class: "hint" }, d.kind === "mcp" && d.transport === "remote" ? "A remote server has no files." : "No files."));
    const pre = el("pre", { class: "md-pre lib-file" }, "Loading…");
    const names = el("div", { class: "lib-files" });
    async function open(name) {
      names.replaceChildren(...d.files.map((f) => el("button", {
        type: "button", class: "lib-file-btn" + (f.name === name ? " on" : ""), onclick: () => open(f.name),
      }, f.name, el("span", { class: "hint" }, ` ${f.size} B`))));
      pre.textContent = "Loading…";
      try {
        const res = await fetch(`/api/library/${d.id}/files/${name.split("/").map(encodeURIComponent).join("/")}`);
        pre.textContent = res.ok ? await res.text() : ((await res.json().catch(() => ({}))).error || "Cannot show this file.");
      } catch (err) { pre.textContent = err.message; }
    }
    box.append(cmdBox(d), el("p", { class: "hint" }, d.filesFrom === "proposal" ? "This is the proposed code. Nothing runs until you publish." : "Code of the latest version."), names, pre);
    open(d.files.find((f) => f.name === d.entry)?.name || d.files.find((f) => f.name === "SKILL.md")?.name || d.files[0].name);
    return box;
  }

  function versions() {
    if (!d.versions.length) return el("p", { class: "hint" }, "Nothing published yet.");
    return el("ul", { class: "lib-uses versions" }, d.versions.map((v) => el("li", {},
      el("span", { class: "mono" }, "v" + v.version),
      el("span", { class: "lib-use-title" }, v.note || "—"),
      el("span", { class: "muted" }, `${v.files} ${v.files === 1 ? "file" : "files"} · ${v.lines} lines · ${new Date(v.createdAt).toLocaleDateString("en-GB", { month: "short", day: "numeric" })}`))));
  }

  const row = (label, ...val) => [el("span", { class: "side-label" }, label), el("div", { class: "side-value" }, val)];
  const unpublish = el("button", { class: "btn ghost danger", type: "button" }, "Unpublish");
  unpublish.addEventListener("click", async () => {
    if (!confirm(`Unpublish ${d.name}? Projects that installed it keep using their version.`)) return;
    if (await run(unpublish, "Unpublishing…", () => api(`/api/library/${d.id}/unpublish`, { method: "POST" }))) {
      location.hash = `#/library/${cfg.path}`;
    }
  });
  const toList = () => { location.hash = `#/library/${cfg.path}`; };
  const side = el("aside", { class: "modal-side lib-side" },
    el("div", { class: "side-rows" },
      row("Type", kindWord),
      row("Made by", avatar(d.madeBy, "worker", 26), d.madeBy),
      row("Status", { published: "Published", proposed: "Proposed · needs you", declined: "Kept in a project", unpublished: "Unpublished" }[d.status] || d.status),
      d.publishedAt ? row("Published", new Date(d.publishedAt).toLocaleDateString("en-GB", { month: "short", day: "numeric" })) : null,
      d.version ? row("Version", el("span", { class: "mono" }, "v" + d.version.version)) : null,
      d.installs ? row("Installed in", `${d.installs} ${d.installs === 1 ? "project" : "projects"}`) : null),
    (d.needs || []).length || (d.secrets || []).length
      ? el("div", { class: "lib-needs" }, el("span", { class: "side-label" }, "Needs"), el("div", { class: "chips" }, chips(d)))
      : null,
    el("div", { class: "side-foot lib-side-foot" },
      proposed ? [publishButton(d, toList), ...declineButtons(d, projectNames, toList)] : null,
      d.status === "published" ? unpublish : null));

  root.append(
    topbar("library"),
    el("div", { class: "lib-item" },
      el("div", { class: "lib-item-main" },
        el("nav", { class: "lib-crumbs", "aria-label": "Breadcrumb" },
          el("a", { href: `#/library/${cfg.path}` }, cfg.title), el("span", { class: "crumb-sep", "aria-hidden": "true" }, "/"),
          el("span", { class: "mono current" }, d.name)),
        el("h1", { class: "lib-item-title" }, d.name),
        el("p", { class: "lib-item-desc" }, d.description),
        tabBar, panel),
      side));
  drawTabs();
}

// ---------- formular "New tool / skill / server" ----------

function newForm(kind, onCreated) {
  const cfg = KINDS[kind];
  const name = el("input", { type: "text", placeholder: kind === "skill" ? "romanian-ui-copy" : "pdf-offer", autocomplete: "off" });
  const desc = el("input", { type: "text", placeholder: "One line: what it does and when to use it" });
  const content = el("textarea", { class: "mono-area", rows: "10", placeholder: kind === "skill" ? "---\nname: ...\nwhen: ...\n---\nInstructions" : "#!/bin/sh\n..." });
  const entry = el("input", { type: "text", value: "run.sh", autocomplete: "off" });
  const needs = el("div", { class: "lib-checks" }, Object.entries(NEEDS).map(([k, label]) =>
    el("label", { class: "lib-check" }, el("input", { type: "checkbox", value: k }), label)));
  const secrets = el("input", { type: "text", placeholder: "META_TOKEN, DATABASE_URL (names only; values are set per project)", autocomplete: "off" });
  const mode = el("select", {}, el("option", { value: "remote" }, "Remote (URL)"), el("option", { value: "local" }, "Local (command)"));
  const url = el("input", { type: "text", placeholder: "https://example.com/mcp", autocomplete: "off" });
  const command = el("input", { type: "text", placeholder: "npx", autocomplete: "off" });
  const args = el("textarea", { rows: "3", placeholder: "One argument per line" });
  const remoteBox = el("div", { class: "field-stack" }, field("URL", url));
  const localBox = el("div", { class: "field-stack", hidden: true }, field("Command", command), field("Arguments", args));
  mode.addEventListener("change", () => { remoteBox.hidden = mode.value !== "remote"; localBox.hidden = mode.value !== "local"; });
  const list = (s) => s.split(",").map((x) => x.trim()).filter(Boolean);

  const create = el("button", { class: "btn primary", type: "button" }, "Create and publish");
  const close = () => overlay.remove();
  create.addEventListener("click", async () => {
    const body = { kind, name: name.value.trim(), description: desc.value.trim() };
    if (kind === "skill") body.content = content.value;
    if (kind === "tool") {
      body.content = content.value;
      body.entry = entry.value.trim();
      body.needs = [...needs.querySelectorAll("input:checked")].map((i) => i.value);
    }
    if (kind !== "skill") body.secrets = list(secrets.value);
    if (kind === "mcp") {
      if (mode.value === "remote") body.url = url.value.trim();
      else { body.command = command.value.trim(); body.args = args.value.split("\n").map((x) => x.trim()).filter(Boolean); }
    }
    if (await run(create, "Creating…", () => api("/api/library", { method: "POST", body }))) {
      toast(`${body.name} created`, "ok");
      close();
      onCreated();
    }
  });
  const overlay = el("div", { class: "modal-overlay", onclick: (e) => { if (e.target === overlay) close(); } },
    el("div", { class: "modal lib-form", role: "dialog", "aria-modal": "true", "aria-label": cfg.add.replace("+ ", "") },
      el("div", { class: "lib-form-body" },
        el("h2", {}, cfg.add.replace("+ ", "")),
        field("Name", name, "Lowercase letters, digits and dashes."),
        field("Description", desc),
        kind === "mcp" ? field("Runs", mode) : null,
        kind === "mcp" ? [remoteBox, localBox] : null,
        kind === "tool" ? field("Entry file", entry, "The file agents run.") : null,
        kind === "tool" ? el("div", { class: "field" }, el("span", { class: "label" }, "Needs"), needs) : null,
        kind !== "mcp" ? field(kind === "skill" ? "SKILL.md" : "Code", content) : null,
        kind !== "skill" ? field("Secrets", secrets, kind === "mcp" ? "A remote server can only have AUTH_TOKEN (sent as a Bearer token). A local one gets each secret as an environment variable." : null) : null),
      el("div", { class: "lib-form-foot" },
        el("button", { class: "btn ghost", type: "button", onclick: close }, "Cancel"), el("span", { class: "grow" }), create)));
  overlay.addEventListener("keydown", (e) => { if (e.key === "Escape") close(); });
  document.body.append(overlay);
  name.focus();
}
