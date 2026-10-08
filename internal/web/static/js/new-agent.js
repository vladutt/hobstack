import { api } from "./api.js";
import { el, avatar, kindOf, toast, topbar, agentBody, TOOLS } from "./ui.js";

// Pagina "Agent nou" (#/p/<id>/agents/new): numele, rolul, ce ia asupra lui si modelul in
// stanga; locul in echipa (sef, reviewer), o previzualizare a ramurii si tool-urile in dreapta.
// Sef implicit: radacina proiectului (sau ?parent=<id> din adresa).
export async function newAgentView(root, projectId, parentHint) {
  const [p, info] = await Promise.all([
    api(`/api/projects/${projectId}`),
    api("/api/info").catch(() => ({ models: [] })),
  ]);
  const agents = p.agents;
  const rootAgent = agents.find((a) => a.isRoot) || agents[0];
  const firstParent = agents.some((a) => a.id === parentHint) ? parentHint : rootAgent?.id;

  const name = el("input", { type: "text", maxlength: 40, required: true, placeholder: "e.g. Herald", "aria-label": "Name" });
  const nameAvatar = el("span", { class: "na-avatar" });
  const role = el("input", { type: "text", maxlength: 80, placeholder: "e.g. E-mail & Notifications Developer", "aria-label": "Role" });
  const takes = el("textarea", { rows: 4, maxlength: 2000, "aria-label": "Responsibilities",
    placeholder: "e.g. Transactional e-mails and in-app notifications: mail templates, notification channels, queue workers for sending. Does not touch domain logic outside Notification." });
  const model = el("select", { "aria-label": "Model" },
    el("optgroup", { label: "Always latest" }, (info.models || []).filter((m) => m.group === "latest").map((m) => el("option", { value: m.id }, m.label))),
    el("optgroup", { label: "Pinned version" }, (info.models || []).filter((m) => m.group === "pinned").map((m) => el("option", { value: m.id }, m.label))));
  model.value = "sonnet";
  const instructions = el("input", { type: "text", maxlength: 4000, placeholder: "Conventions, tone, things to always check...", "aria-label": "Extra instructions" });
  const background = el("textarea", { rows: 3, placeholder: "Who the agent is and what it knows.", "aria-label": "Background" });
  const doneDef = el("textarea", { rows: 2, placeholder: "Summary, changed files, how to verify.", "aria-label": "Definition of done" });
  const more = el("details", { class: "na-more" },
    el("summary", {}, "More: background and definition of done"),
    el("div", { class: "field" }, el("label", { class: "label" }, "Background · who it is and what it knows"), background),
    el("div", { class: "field" }, el("label", { class: "label" }, "Definition of done · what it hands to its boss"), doneDef));

  // "" = Tu (radacina), "none" = nelegat, altfel id-ul sefului
  const parent = el("select", { class: "na-person", "aria-label": "Reports to" },
    el("option", { value: "" }, "You (root)"),
    agents.map((a) => el("option", { value: a.id }, a.name)),
    el("option", { value: "none" }, "Nobody (detached)"));
  parent.value = firstParent ? String(firstParent) : "";
  const reviewer = el("select", { class: "na-person", "aria-label": "Reviewed by" },
    el("option", { value: "" }, "Nobody"), agents.map((a) => el("option", { value: a.id }, a.name)));
  const folders = p.folders || [];
  const folder = folders.length > 1 ? el("select", { "aria-label": "Folder" }, folders.map((f) => el("option", { value: f.id }, f.name))) : null;
  const syncFolder = () => { // ca seful lui, pana il alegi tu
    if (!folder || folder.dataset.touched) return;
    const boss = agents.find((a) => String(a.id) === parent.value);
    folder.value = String(boss?.folderId ?? folders[0].id);
  };
  folder?.addEventListener("change", () => { folder.dataset.touched = "1"; });
  const tools = TOOLS.map((t) => ({ ...t, box: el("input", { type: "checkbox", checked: t.id === "write" || t.id === "shell" }) }));
  const toolHints = { shell: "rm -rf, sudo, git push stay blocked", web: "search and fetch pages" };

  const preview = el("div", { class: "na-tree" });
  const note = el("p", { class: "np-note" });
  const addBtn = el("button", { type: "submit", form: "new-agent", class: "btn primary" }, "Add to team");

  // campurile atinse de utilizator nu se suprascriu cu propunerea AI
  const touched = new Set();
  for (const [key, node] of Object.entries({ name, role, takes, model, instructions, background, doneDef, parent, reviewer })) {
    node.addEventListener(node.tagName === "SELECT" ? "change" : "input", () => touched.add(key));
  }
  for (const t of tools) t.box.addEventListener("change", () => touched.add("tools"));
  const ask = el("input", { type: "text", maxlength: 1000, placeholder: "e.g. someone to own the e-mail templates", "aria-label": "What is the team missing" });
  const draftBtn = el("button", { type: "button", class: "link-btn na-draft-btn", onclick: runDraft }, "Draft");
  const draftNote = el("span", { class: "hint" });
  const draftBox = el("section", { class: "na-draft" },
    el("span", { class: "np-spark", "aria-hidden": "true" }, "\u2726"),
    el("div", { class: "na-draft-text" }, el("strong", {}, "Let AI draft this agent"),
      el("span", { class: "hint" }, "Say what's missing from the team; it proposes name, role, responsibilities and where it reports."), draftNote),
    el("div", { class: "na-draft-ask" }, ask, draftBtn));
  ask.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); runDraft(); } });
  let draftId = null, timer = null;

  async function runDraft() {
    if (draftId) { // al doilea click opreste draft-ul in curs
      api(`/api/drafts/${draftId}/stop`, { method: "POST" }).catch(() => {});
      return;
    }
    if (!ask.value.trim()) { ask.focus(); return; }
    draftBtn.disabled = true; // fara al doilea POST cat timp primul e in zbor
    try {
      const d = await api(`/api/projects/${projectId}/agents/draft`, { method: "POST", body: { ask: ask.value.trim(), model: "sonnet" } });
      draftId = d.id;
      draftBtn.textContent = "Stop";
      draftNote.textContent = "Drafting\u2026";
      poll();
    } catch (err) { toast(err.message); }
    draftBtn.disabled = false;
  }
  async function poll() {
    clearTimeout(timer);
    if (!draftId || !root.isConnected) return;
    let d;
    try { d = await api(`/api/drafts/${draftId}`); } catch (err) { finishDraft(err.message); return; }
    if (d.status === "running") { timer = setTimeout(poll, 1000); return; }
    if (d.status === "ready" && d.proposal) { fillFrom(d.proposal); finishDraft("Drafted. Check the fields, then Add to team."); return; }
    finishDraft(d.error || (d.status === "stopped" ? "Stopped." : "The draft failed."));
  }
  function finishDraft(msg) {
    draftId = null;
    draftBtn.textContent = "Draft";
    draftNote.textContent = msg;
  }
  // fillFrom: pune propunerea doar in campurile neatinse; sef/reviewer dupa nume (fara diferenta de litere/spatii)
  function fillFrom(p) {
    const set = (key, node, value) => { if (value && !touched.has(key)) node.value = value; };
    set("name", name, p.name); set("role", role, p.role); set("takes", takes, p.responsibilities);
    set("model", model, p.model); set("instructions", instructions, p.instructions);
    set("background", background, p.background); set("doneDef", doneDef, p.doneDef);
    // campurile din "More" sunt intr-un details inchis: il deschidem ca utilizatorul sa vada ce s-a completat
    if ((p.background && !touched.has("background")) || (p.doneDef && !touched.has("doneDef"))) more.open = true;
    const idOf = (n) => agents.find((a) => a.name.trim().toLowerCase() === (n || "").trim().toLowerCase())?.id;
    // seful se schimba doar daca propunerea indica un agent existent; altfel ramane cel implicit
    const bossId = idOf(p.reportsTo);
    if (!touched.has("parent") && bossId != null) parent.value = String(bossId);
    if (!touched.has("reviewer") && p.reviewedBy && idOf(p.reviewedBy)) reviewer.value = String(idOf(p.reviewedBy));
    if (!touched.has("tools") && Array.isArray(p.tools)) for (const t of tools) t.box.checked = p.tools.includes(t.id);
    syncFolder();
    draw();
  }

  const form = el("form", { id: "new-agent", class: "np na", onsubmit: save },
    el("div", { class: "np-main" }, draftBox,
      el("div", { class: "row" },
        el("div", { class: "field" }, el("label", { class: "label" }, "Name"),
          el("div", { class: "na-name" }, nameAvatar, name),
          el("span", { class: "hint" }, `${rootAgent?.name || "The lead"} assigns tasks by this name.`)),
        el("div", { class: "field" }, el("label", { class: "label" }, "Role"), role)),
      el("div", { class: "field" },
        el("label", { class: "label" }, "Takes ", el("span", { class: "np-dim" }, "· what this agent is responsible for")),
        takes,
        el("span", { class: "hint" }, `${rootAgent?.name || "The lead"} reads this to decide what to hand over. Be specific about what it does not take.`)),
      el("div", { class: "row" },
        el("div", { class: "field" }, el("label", { class: "label" }, "Model"), model,
          el("span", { class: "hint" }, "Sonnet: balanced. Opus for leads and reviewers. Haiku: fast and cheap.")),
        el("div", { class: "field" }, el("label", { class: "label" }, "Extra instructions ", el("span", { class: "np-dim" }, "· optional")), instructions)),
      folder ? el("div", { class: "field" }, el("label", { class: "label" }, "Folder"), folder,
        el("span", { class: "hint" }, "The agent works and writes here; it can read the other folders.")) : null,
      more),
    el("aside", { class: "np-side" },
      el("section", { class: "np-card" },
        el("strong", {}, "Place in the team"),
        el("div", { class: "na-place" },
          el("span", { class: "side-label" }, "Reports to"), parent,
          el("span", { class: "side-label" }, "Reviewed by"), reviewer),
        el("div", { class: "na-preview" }, el("span", { class: "na-cap" }, "Preview"), preview)),
      el("section", { class: "np-card" },
        el("strong", {}, "Tools"),
        el("div", { class: "na-tools" }, tools.map((t) => el("label", { class: "check" }, t.box,
          el("span", { class: "grow" }, t.label.replace(/ \(.*\)$/, "")),
          toolHints[t.id] ? el("span", { class: "na-tool-hint" }, toolHints[t.id]) : null))),
        el("span", { class: "hint" }, "Reading files is always allowed. Write files: only inside the agent's folder.")),
      note));

  root.append(
    topbar([["Projects", "#/"], [p.name, `#/p/${projectId}`], ["Team", `#/p/${projectId}/team`], ["New agent", null]],
      el("span", { class: "grow" }),
      el("a", { class: "btn ghost", href: `#/p/${projectId}/team` }, "Cancel"),
      addBtn),
    el("div", { class: "page" }, form));

  for (const n of [name, parent, reviewer]) n.addEventListener(n.tagName === "SELECT" ? "change" : "input", draw);
  parent.addEventListener("change", syncFolder);
  syncFolder();
  draw();
  name.focus();

  // draw: avatarul, ramura sefului cu agentul nou si nota de jos
  function draw() {
    const n = name.value.trim() || "New agent";
    nameAvatar.replaceChildren(avatar(n, "worker", 28));
    const boss = agents.find((a) => String(a.id) === parent.value);
    const rev = agents.find((a) => String(a.id) === reviewer.value);
    const detached = parent.value === "none";
    const siblings = agents.filter((a) => boss ? a.parentId === boss.id : !a.parentId && !a.detached);
    const line = (a, cls, ...extra) => el("div", { class: "na-line " + cls }, avatar(a.name, kindOf(a), 18), el("strong", {}, a.name), extra);
    const me = el("div", { class: "na-line na-me" + (boss || !detached ? " child" : "") }, avatar(n, "worker", 18), el("strong", {}, n),
      el("span", { class: "na-new" }, "new"),
      rev ? el("span", { class: "na-rev" }, `← ${rev.name} reviews`) : null);
    preview.replaceChildren(...(detached
      ? [me, el("span", { class: "hint" }, "Detached: it does not work until you link it on the canvas.")]
      : [boss ? line(boss, "na-boss") : el("div", { class: "na-line na-boss" }, el("span", { class: "avatar", style: "--size:18px;--tone:#3a414d;--ink:#dfe3e8" }, "Y"), el("strong", {}, "You")),
        siblings.length ? el("div", { class: "na-line child na-sibs" }, siblings.slice(0, 4).map((a) => a.name).join(" · ") + (siblings.length > 4 ? ` +${siblings.length - 4}` : "")) : null,
        me].filter(Boolean)));
    note.replaceChildren(el("span", { class: "dot" }), detached
      ? "Starts detached and idle."
      : `Starts idle. ${boss ? boss.name : "You"} will brief it on the first task.`);
  }

  async function save(e) {
    e.preventDefault();
    if (!name.value.trim()) { toast("The agent needs a name."); name.focus(); return; }
    addBtn.disabled = true;
    const parentId = parent.value && parent.value !== "none" ? Number(parent.value) : null;
    try {
      const a = await api(`/api/projects/${projectId}/agents`, { method: "POST", body: agentBody({
        name: name.value.trim(), role: role.value.trim(), model: model.value,
        background: background.value.trim(), instructions: instructions.value.trim(), doneDef: doneDef.value.trim(),
        tools: tools.filter((t) => t.box.checked).map((t) => t.id), responsibilities: takes.value.trim(),
        parentId, reviewerId: reviewer.value ? Number(reviewer.value) : null, detached: parent.value === "none",
        folderId: folder && Number(folder.value) !== folders[0].id ? Number(folder.value) : null,
      }) });
      location.hash = `#/p/${projectId}/team/${a.id}`;
    } catch (err) {
      toast(err.message);
      addBtn.disabled = false;
    }
  }
}
