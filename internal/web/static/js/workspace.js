import { api, upload } from "./api.js";
import { el, avatar, pill, toast, clock, kindOf, agentBody } from "./ui.js";
import { teamCanvas } from "./canvas.js";
import { agentEditor, editableKey } from "./agent.js";
import { liveRenderer } from "./live.js";
import { stagesEditor } from "./stages.js";

const COLUMNS = [
  { title: "In asteptare", statuses: ["queued"] },
  { title: "In lucru", statuses: ["in_progress"] },
  { title: "Review", statuses: ["review", "blocked"] },
  { title: "Gata", statuses: ["done"] },
];

const ref = (t) => `T-${t.number}`;

// Tab-urile paginii de proiect; primul e cel implicit.
const TABS = [
  { id: "task-uri", label: "Task-uri" },
  { id: "echipa", label: "Echipa" },
  { id: "fisiere", label: "Fisiere" },
];

// parseWorkspaceRoute: restul hash-ului dupa #/p/3 -> { tab, agentId }.
// Link-urile vechi #/p/3/agent/7 duc in tab-ul Echipa, cu agentul 7 selectat.
export function parseWorkspaceRoute(rest) {
  const parts = (rest || "").split("/").filter(Boolean);
  if (parts[0] === "agent") {
    return { tab: "echipa", agentId: /^\d+$/.test(parts[1] || "") ? Number(parts[1]) : null };
  }
  const tab = TABS.some((t) => t.id === parts[0]) ? parts[0] : TABS[0].id;
  const agentId = tab === "echipa" && /^\d+$/.test(parts[1] || "") ? Number(parts[1]) : null;
  return { tab, agentId };
}

export async function workspaceView(root, projectId, route) {
  const state = {
    project: await api(`/api/projects/${projectId}`),
    tab: route.tab,
    panel: route.tab === "echipa" ? "details" : "chat", // panoul din dreapta: "details" sau "chat"
    selected: null,   // agentul selectat (chat si detalii)
    seen: new Set(),  // id-urile mesajelor afisate in thread
    open: new Set(),  // task-urile deschise pe board (raman deschise la redesenare)
    liveRunId: null,
    liveStream: null,
    pending: [],      // fisiere alese pentru urmatorul mesaj
    doneRunId: null,  // ultimul run terminat: activeRunId poate ramane vechi pana la reload
  };
  // butoanele "Deschide": create o singura data si mutate in header la fiecare
  // redesenare, ca selectia de editor sa nu se piarda
  const info = await api("/api/info").catch(() => ({ editors: [] }));
  const openBar = buildOpenBar(info.editors || []);
  let disposed = false; // view-ul a fost inchis: nu mai deschidem stream-uri si nu mai desenam
  // view-ul e inchis daca s-a apelat curatarea sau daca #view a fost golit intre timp
  const gone = () => disposed || !header.isConnected;
  state.selected = state.project.agents.some((a) => a.id === route.agentId) ? route.agentId : firstRootId();
  let editor = null;     // formularul din Detalii
  let editorKey = "";    // editableKey pentru datele din formular

  const hasFiles = (e) => Array.from(e.dataTransfer?.types || []).includes("Files");
  const dropResets = []; // resetari de contor + outline, apelate la orice drop/dragend
  // titlul proiectului sta in coloana din stanga, ca chat-ul sa inceapa din
  // varful paginii si sa incapa intreg pe ecran (inclusiv campul de scris)
  const header = el("div", { class: "ws-head" });
  const tabsNav = el("nav", { class: "tabs", "aria-label": "Sectiuni proiect" });
  const canvas = teamCanvas({
    onSelect: (id) => select(id),
    onEdit: (id) => select(id, "details"),
    onChanged: () => reload(),
    onCreate: (pos, link) => createAgent(pos, link),
    onDelete: (id) => deleteAgent(id),
  });
  // pe Task-uri si Fisiere alegi cu cine vorbesti din banda cu echipa
  const strip = el("div", { class: "team-strip", "aria-label": "Echipa" });
  const board = el("div", { class: "board" });
  const count = el("span", { class: "hint" });
  const boardTools = el("div", { class: "actions" }); // Coloane, + Task
  const editorSlot = el("div"); // editorul de coloane, deschis la cerere
  const boardHint = el("p", { class: "hint board-hint" }, "Fiecare proiect e diferit: fa-ti propriile coloane (etape de lucru) din butonul Coloane.");
  const filesList = el("div", { class: "files-list" });
  const projectPicker = el("input", { type: "file", multiple: true, hidden: true });
  projectPicker.addEventListener("change", () => { uploadProjectFiles(projectPicker.files); projectPicker.value = ""; });
  const filesSection = el("section", { class: "section" },
    el("div", { class: "section-head" },
      el("h2", {}, "Fisiere"),
      el("button", { type: "button", class: "btn small", onclick: () => projectPicker.click() }, "Incarca"),
      projectPicker),
    filesList);
  setupDrop(filesSection, uploadProjectFiles);
  const consoleHead = el("div", { class: "console-head" });
  const thread = el("div", { class: "thread", "aria-live": "polite" });
  const input = el("textarea", { rows: 1, "aria-label": "Mesaj" });
  const note = el("span", { class: "hint" });
  const chips = el("div", { class: "chips" });
  const chatPicker = el("input", { type: "file", multiple: true, hidden: true });
  chatPicker.addEventListener("change", () => { addPending(chatPicker.files); chatPicker.value = ""; });
  const clip = el("button", {
    type: "button", class: "clip", "aria-label": "Ataseaza fisiere", title: "Ataseaza fisiere",
    onclick: () => chatPicker.click(),
  }, "\u{1F4CE}");
  let sending = false; // trimitere in curs: blocheaza dublul submit
  const sendBtn = el("button", { type: "submit", class: "btn primary" }, "Trimite");
  const composer = el("form", { onsubmit: send }, clip, chatPicker, input, sendBtn);
  input.addEventListener("paste", (e) => {
    const files = e.clipboardData?.files;
    if (!files || !files.length) return;
    // daca e si text (ex. copiat din Word), lasam textul sa se lipeasca
    if (!e.clipboardData.getData("text/plain")) e.preventDefault();
    addPending(files);
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      composer.requestSubmit();
    }
  });

  // panoul din dreapta: pe Echipa are doua tab-uri (Detalii | Chat), altfel doar chat
  const panelTabs = el("div", { class: "panel-tabs", role: "tablist" });
  const detailsBox = el("div", { class: "details" });
  const chatBox = el("div", { class: "chat" }, thread, el("div", { class: "composer" }, chips, composer, note));
  const consoleBox = el("aside", { class: "console" }, consoleHead, panelTabs, detailsBox, chatBox);
  setupDrop(chatBox, addPending);

  const bodies = {
    "echipa": el("div", { class: "tab-body team-body" }, canvas.el),
    "task-uri": el("section", { class: "section tab-body" },
      el("div", { class: "section-head" }, el("div", { class: "actions" }, el("h2", {}, "Task-uri"), count), boardTools),
      editorSlot, board, boardHint),
    "fisiere": el("div", { class: "tab-body" }, filesSection),
  };
  const ws = el("div", { class: "ws" },
    el("div", { class: "ws-main" }, header, strip, Object.values(bodies)),
    consoleBox);
  root.append(ws);

  // ---------- desenare ----------
  function renderAll() {
    renderHeader();
    renderTabs();
    renderTeam();
    renderStrip();
    renderBoard();
    renderConsoleHead();
    renderDetails();
  }

  // renderTabs: tab-ul activ al paginii si al panoului din dreapta
  function renderTabs() {
    ws.dataset.tab = state.tab;
    tabsNav.replaceChildren(...TABS.map((t) => el("a", {
      class: "tab", href: `#/p/${projectId}/${t.id}`,
      "aria-current": t.id === state.tab ? "page" : null,
    }, t.label)));
    for (const [id, body] of Object.entries(bodies)) body.hidden = id !== state.tab;
    strip.hidden = state.tab === "echipa";
    const team = state.tab === "echipa";
    const panel = team ? state.panel : "chat";
    panelTabs.hidden = !team;
    panelTabs.replaceChildren(...[["details", "Detalii"], ["chat", "Chat"]].map(([id, label]) =>
      el("button", {
        type: "button", role: "tab", class: "panel-tab", "aria-selected": String(panel === id),
        onclick: () => { state.panel = id; renderTabs(); if (id === "chat") scrollDown(true); },
      }, label)));
    consoleBox.classList.toggle("on-details", panel === "details"); // hint-ul de thread e doar pentru chat
    detailsBox.hidden = panel !== "details";
    chatBox.hidden = panel !== "chat";
  }

  function renderStrip() {
    strip.replaceChildren(...state.project.agents.map((a) => el("button", {
      type: "button", class: "strip-agent" + (a.id === state.selected ? " selected" : ""),
      "aria-pressed": String(a.id === state.selected), title: a.role || a.name,
      onclick: () => select(a.id),
    },
      avatar(a.name, kindOf(a), 22),
      el("span", {}, a.name),
      el("span", { class: "dot " + (a.status || "idle").toLowerCase() }))));
  }

  // renderDetails: formularul agentului selectat. Nu reconstruim un formular
  // modificat de utilizator; unul nemodificat se reface daca datele s-au schimbat.
  function renderDetails(force) {
    const a = selectedAgent();
    if (!a) {
      editor?.destroy();
      editor = null;
      editorKey = "";
      detailsBox.replaceChildren(el("p", { class: "empty" }, "Alege un agent pe canvas sau fa unul nou cu dublu-click pe gol."));
      return null;
    }
    const key = a.id + ":" + editableKey(a, state.project.agents);
    if (!force && editor && editorKey.startsWith(a.id + ":") && (editor.isDirty() || key === editorKey)) return editor;
    editor?.destroy();
    editorKey = key;
    editor = agentEditor({
      projectId, project: state.project, info, agent: a,
      onSaved: () => reload(),
      onDeleted: async () => { state.selected = null; editor = null; await reload(); },
    });
    detailsBox.replaceChildren(editor.el);
    return editor;
  }

  // leaveEditor: true daca putem schimba agentul din formular (fara modificari
  // nesalvate sau utilizatorul a confirmat ca renunta la ele)
  function leaveEditor() {
    if (!editor || !editor.isDirty()) return true;
    if (!confirm("Ai modificari nesalvate la " + (selectedAgent()?.name || "agent") + ". Renunti la ele?")) return false;
    editor.destroy();
    editor = null;
    return true;
  }

  // createAgent: agent nou pus pe canvas la pos. link = {parentId, detached}:
  // in echipa unui agent, legat de utilizator sau nelegat.
  async function createAgent(pos, { parentId, detached }) {
    if (!leaveEditor()) return;
    const names = new Set(state.project.agents.map((a) => a.name.toLowerCase()));
    let name = "Agent nou";
    for (let i = 2; names.has(name.toLowerCase()); i++) name = `Agent nou ${i}`;
    const body = agentBody({
      name, role: "", model: "sonnet", background: "", instructions: "", doneDef: "",
      tools: ["write"], responsibilities: "", parentId, reviewerId: null, detached,
    });
    try {
      const a = await api(`/api/projects/${projectId}/agents`, { method: "POST", body });
      if (pos) await api(`/api/agents/${a.id}/position`, { method: "PUT", body: pos });
      state.selected = a.id;
      state.panel = "details";
      await reload();
      if (gone()) return;
      renderDetails(true)?.focusName();
      syncHash();
      await loadThread();
    } catch (err) {
      toast(err.message);
    }
  }

  // deleteAgent: butonul x de pe nod
  async function deleteAgent(id) {
    const a = state.project.agents.find((x) => x.id === id);
    if (!a || !confirm(`Stergi agentul ${a.name}?`)) return;
    try {
      await api(`/api/agents/${id}`, { method: "DELETE" });
    } catch (err) {
      toast(err.message);
      return;
    }
    if (state.selected === id) {
      editor?.destroy();
      editor = null;
      state.selected = null;
    }
    await reload();
    await loadThread();
  }

  // syncHash: pe Echipa, adresa contine agentul selectat (refresh-ul il pastreaza).
  // replaceState nu declanseaza hashchange, deci nu trece prin router.
  function syncHash() {
    if (state.tab !== "echipa" || !state.selected) return;
    const want = `#/p/${projectId}/echipa/${state.selected}`;
    if (location.hash !== want) history.replaceState(null, "", want);
  }

  // update: ruta s-a schimbat in acelasi proiect (alt tab sau alt agent)
  async function update(r) {
    if (r.tab === "echipa" && state.tab !== "echipa") state.panel = "details"; // intrarea pe Echipa arata Detaliile
    state.tab = r.tab;
    if (r.tab === "echipa" && r.agentId && r.agentId !== state.selected) {
      if (!state.project.agents.some((a) => a.id === r.agentId)) { renderTabs(); return; }
      state.panel = "details";
      await select(r.agentId);
      return;
    }
    renderTabs();
    syncHash();
    if (!chatBox.hidden) scrollDown(true);
  }

  function renderHeader() {
    const p = state.project;
    header.replaceChildren(el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" }, el("a", { href: "#/" }, "Proiecte"), " / ", p.name),
        el("div", { class: "actions" }, el("h1", {}, p.name), pill(p.status)),
        p.goal && el("p", { class: "muted clamp" }, "Obiectiv: " + p.goal),
        p.paused && el("p", { class: "hint" }, "Echipa e pe pauza. Mesajele raman in asteptare pana o pornesti.")),
      el("div", { class: "head-actions" },
        el("div", { class: "actions" },
          el("a", { class: "btn", href: `#/p/${projectId}/edit` }, "Instructiuni proiect"),
          p.paused
            ? el("button", { class: "btn primary", onclick: () => projectAction("resume") }, "Porneste echipa")
            : el("button", { class: "btn", onclick: () => projectAction("stop") }, "Opreste echipa")),
        openBar)),
      tabsNav);
  }

  function buildOpenBar(editors) {
    let saved = null;
    try { saved = localStorage.getItem("foreman-editor"); } catch {}
    const known = editors.some((e) => e.id === saved);
    const select = el("select", {
      class: "select-small", "aria-label": "Editor",
      value: known ? saved : editors[0]?.id,
      onchange: () => { try { localStorage.setItem("foreman-editor", select.value); } catch {} },
    }, editors.map((e) => el("option", { value: e.id }, e.name)));
    return el("div", { class: "actions open-bar" },
      el("span", { class: "hint" }, "Deschide:"),
      el("button", { type: "button", class: "btn small", onclick: () => openIn("folder") }, "Folder"),
      el("button", { type: "button", class: "btn small", onclick: () => openIn("browser") }, "Browser"),
      editors.length > 0 && [select,
        el("button", { type: "button", class: "btn small", onclick: () => openIn("editor", select.value) }, "Editor")]);
  }

  async function openIn(target, editor) {
    try {
      await api(`/api/projects/${projectId}/open`, { method: "POST", body: { target, editor } });
    } catch (err) {
      toast(err.message);
    }
  }

  function renderTeam() {
    canvas.render(state.project, state.selected);
  }

  // renderBoard: coloanele implicite, sau etapele proiectului + "La lider" + "Gata"
  function renderBoard() {
    const p = state.project;
    const byId = new Map(p.agents.map((a) => [a.id, a]));
    const stages = p.stages || [];
    count.textContent = `${p.done} din ${p.total} gata`;
    // replaceChildren nu ignora false (el() da): filtram noi
    boardTools.replaceChildren(...[
      stages.length > 0 && el("button", { type: "button", class: "btn small", onclick: newTask }, "+ Task"),
      el("button", { type: "button", class: "btn small", onclick: openStages }, "Coloane"),
    ].filter(Boolean));
    if (!stages.length) {
      board.className = "board";
      board.replaceChildren(...COLUMNS.map((c) => column(c.title, p.tasks.filter((t) => c.statuses.includes(t.status)), byId)));
      boardHint.hidden = false;
      return;
    }
    boardHint.hidden = true;
    board.className = "board stages-board";
    const inStage = (s) => p.tasks.filter((t) => t.stageId === s.id && t.status !== "review" && t.status !== "done");
    board.replaceChildren(
      ...stages.map((s) => column(s.name, inStage(s), byId, s)),
      column("La lider", p.tasks.filter((t) => t.status === "review"), byId),
      column("Gata", p.tasks.filter((t) => t.status === "done"), byId));
  }

  function column(title, items, byId, stage) {
    const agents = stage ? stage.agentIds.map((id) => byId.get(id)).filter(Boolean) : [];
    return el("div", { class: "col" },
      el("div", { class: "col-head" },
        el("span", {}, title, stage?.kind === "check" && el("span", { class: "tag col-kind" }, "verificare")),
        el("span", {}, items.length)),
      stage && el("div", { class: "col-agents" },
        agents.length ? agents.map((a) => avatar(a.name, kindOf(a), 18)) : el("span", { class: "badge-blocked" }, "fara agenti")),
      items.map((t) => taskCard(t, byId.get(t.assigneeId), byId)));
  }

  function openStages() {
    editorSlot.replaceChildren(stagesEditor({
      project: state.project,
      onSaved: async () => { editorSlot.replaceChildren(); await reload(); },
      onClose: () => editorSlot.replaceChildren(),
    }));
  }

  async function newTask() {
    const title = prompt("Titlul task-ului:");
    if (!title || !title.trim()) return;
    const description = prompt("Descriere (optional):") || "";
    try {
      await api(`/api/projects/${projectId}/tasks`, { method: "POST", body: { title: title.trim(), description } });
      await reload();
    } catch (err) { toast(err.message); }
  }

  // decide: utilizatorul decide un task creat de el, ajuns dupa ultima etapa
  async function decide(t, action) {
    let feedback = "";
    if (action === "send_back") {
      feedback = prompt(`Ce trebuie schimbat la ${ref(t)}?`) || "";
      if (!feedback.trim()) return;
    }
    try {
      await api(`/api/tasks/${t.id}/decide`, { method: "POST", body: { action, feedback } });
      await reload();
    } catch (err) { toast(err.message); }
  }

  // stageName: la cine asteapta un task din review (reviewer-ul asignatului, altfel creatorul, altfel seful)
  function stageName(t, who, byId) {
    if (t.byUser) return "tine";
    const target = t.reviewStage === "reviewer"
      ? byId.get(who?.reviewerId)
      : byId.get(t.creatorId) || byId.get(who?.parentId);
    return target ? target.name : null;
  }

  function taskCard(t, who, byId) {
    const blocked = t.status === "blocked";
    const stage = t.status === "review" ? stageName(t, who, byId) : null;
    const waiting = t.stageId && t.status === "queued"; // in etapa, il ia primul agent liber
    const mine = t.status === "review" && t.byUser;
    return el("details", {
      class: "task",
      open: state.open.has(t.id),
      ontoggle: (e) => (e.target.open ? state.open.add(t.id) : state.open.delete(t.id)),
    },
      el("summary", {},
        el("span", { class: "task-title" }, el("span", { class: "task-id" }, ref(t)), t.title),
        el("span", { class: "task-who" },
          waiting ? el("span", { class: "hint" }, "in asteptare") : [who && avatar(who.name, kindOf(who), 20), who ? who.name : "?"],
          blocked && el("span", { class: "badge-blocked" }, "Blocat"),
          stage && el("span", { class: "task-stage" }, `la ${stage}`))),
      el("div", { class: "task-detail" },
        t.description && el("div", {}, t.description),
        t.feedback && el("div", {}, el("strong", {}, "Feedback: "), t.feedback),
        t.result && el("div", {}, el("strong", {}, "Rezultat: "), t.result),
        !t.description && !t.feedback && !t.result && el("div", { class: "hint" }, "Fara detalii inca."),
        blocked && el("div", { class: "actions" },
          el("button", { type: "button", class: "btn small primary", onclick: () => taskAction(t, "accept") }, "Accepta"),
          el("button", { type: "button", class: "btn small", onclick: () => taskAction(t, "unblock") }, "Deblocheaza")),
        mine && el("div", { class: "actions" },
          el("button", { type: "button", class: "btn small primary", onclick: () => decide(t, "accept") }, "Accepta"),
          el("button", { type: "button", class: "btn small", onclick: () => decide(t, "send_back") }, "Trimite inapoi"))));
  }

  function firstRootId() {
    return state.project.agents.find((a) => a.isRoot)?.id ?? state.project.agents[0]?.id ?? null;
  }

  function selectedAgent() {
    return state.project.agents.find((a) => a.id === state.selected);
  }

  function renderConsoleHead() {
    const a = selectedAgent();
    if (!a) return;
    const isLead = kindOf(a) === "lead";
    const boss = state.project.agents.find((x) => x.id === a.parentId);
    consoleHead.replaceChildren(
      avatar(a.name, kindOf(a), 36),
      el("div", {},
        el("div", { class: "member-name" }, a.name, a.role && el("span", { class: "hint" }, "· " + a.role)),
        el("div", { class: "hint thread-hint" }, a.detached
          ? "Nelegat: nu lucreaza. Leaga-l pe canvas de Tu sau de un lider."
          : !boss
            ? "Thread direct. Ii dai obiective aici, iar el delegheaza echipei lui."
            : `Thread direct. ${boss.name} primeste in copie ce ii scrii.`)));
    input.placeholder = a.detached ? "Leaga agentul ca sa-i poti scrie" : isLead ? `Scrie-i lui ${a.name}...` : `Mesaj pentru ${a.name}`;
    input.disabled = sendBtn.disabled = clip.disabled = !!a.detached;
    note.textContent = state.project.paused ? "Echipa e pe pauza: mesajul asteapta pana o pornesti." : "";
  }

  // ---------- thread ----------
  // select: schimba agentul din panou; panel ("details"/"chat") alege si tab-ul panoului
  async function select(id, panel) {
    if (id !== state.selected) {
      if (!leaveEditor()) { renderTeam(); return; }
      state.pending = [];
      renderChips();
    }
    const changed = id !== state.selected;
    state.selected = id;
    if (panel) state.panel = panel;
    renderTabs();
    renderTeam();
    renderStrip();
    renderConsoleHead();
    renderDetails();
    syncHash();
    if (!changed) return;
    await loadThread();
    syncLive();
  }

  async function loadThread() {
    const id = state.selected;
    if (!id) return;
    const messages = await api(`/api/agents/${id}/messages`);
    if (gone() || id !== state.selected) return; // s-a schimbat selectia intre timp
    stopLive();
    state.seen = new Set();
    thread.replaceChildren();
    if (!messages.length) thread.append(el("div", { class: "empty" }, "Niciun mesaj inca."));
    messages.forEach(addMessage);
    scrollDown(true);
  }

  function addMessage(m) {
    if (state.seen.has(m.id)) {
      // acelasi mesaj, stare noua (ex. livrat): il redesenam pe loc
      thread.querySelector(`[data-id="${m.id}"]`)?.replaceWith(messageNode(m));
      return;
    }
    state.seen.add(m.id);
    thread.querySelector(".empty")?.remove();
    const node = messageNode(m);
    const live = thread.querySelector(".live");
    if (live) thread.insertBefore(node, live); // blocul live ramane ultimul
    else thread.append(node);
    scrollDown();
  }

  function messageNode(m) {
    if (m.kind === "event") return el("div", { class: "event", "data-id": m.id }, m.text);
    const mine = m.author === "user";
    return el("div", { class: "msg" + (mine ? " me" : ""), "data-id": m.id },
      el("span", { class: "msg-meta" }, `${mine ? "Tu" : m.author} · ${clock(m.createdAt)}`),
      m.text ? el("div", { class: "bubble" }, m.text) : null,
      m.attachments && m.attachments.length > 0 && el("div", { class: "attachments" }, m.attachments.map(attachmentNode)),
      mine && !m.delivered && el("span", { class: "pending" }, "in asteptare"));
  }

  function scrollDown(force) {
    const nearBottom = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (force || nearBottom) thread.scrollTop = thread.scrollHeight;
  }

  async function send(e) {
    e.preventDefault();
    const text = input.value.trim();
    if ((!text && !state.pending.length) || !state.selected || sending) return;
    const agentId = state.selected; // agentul pentru care trimitem, tinut minte cat dureaza cererea
    sending = true;
    sendBtn.disabled = true;
    try {
      let m;
      const path = `/api/agents/${agentId}/messages`;
      if (state.pending.length) {
        const fd = new FormData();
        fd.append("text", text);
        for (const f of state.pending) fd.append("files", f, f.name);
        m = await upload(path, fd);
      } else {
        m = await api(path, { method: "POST", body: { text } });
      }
      // daca userul a schimbat agentul intre timp, nu atingem chatul si lista celuilalt agent
      if (state.selected === agentId) {
        input.value = "";
        state.pending = [];
        renderChips();
        addMessage(m);
        scrollDown(true);
      }
    } catch (err) {
      toast(err.message);
    } finally {
      sending = false;
      sendBtn.disabled = false;
    }
  }

  // ---------- atasamente in chat ----------
  function addPending(fileList) {
    for (const f of Array.from(fileList)) {
      // imaginile lipite (Cmd+V) vin adesea cu numele generic "image.png"
      const generic = f.type.startsWith("image/") && (!f.name || /^image\.\w+$/.test(f.name));
      let file = f;
      if (generic) {
        const ext = { "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp" }[f.type] || "png";
        const d = new Date();
        const t = d.toTimeString().slice(0, 8).replaceAll(":", "") + String(d.getMilliseconds()).padStart(3, "0");
        file = new File([f], `screenshot-${t}.${ext}`, { type: f.type });
      }
      state.pending.push(file);
    }
    renderChips();
  }

  function renderChips() {
    chips.replaceChildren(...state.pending.map((f, i) =>
      el("span", { class: "chip" }, f.name,
        el("button", {
          type: "button", "aria-label": `Scoate ${f.name}`,
          onclick: () => { state.pending.splice(i, 1); renderChips(); },
        }, "\u00d7"))));
  }

  function attachmentNode(a) {
    const url = `/api/files/${a.id}`;
    if (a.mime && a.mime.startsWith("image/")) {
      return el("a", { href: url, target: "_blank", rel: "noopener" },
        el("img", { src: url, alt: a.name, loading: "lazy" }));
    }
    return el("a", { class: "file-link", href: url, target: "_blank", rel: "noopener" }, a.name);
  }

  // ---------- fisiere de proiect ----------
  function fmtSize(n) {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }

  async function loadFiles() {
    let files;
    try {
      files = await api(`/api/projects/${projectId}/files`);
    } catch (err) {
      toast(err.message);
      return;
    }
    if (gone()) return;
    if (!files.length) {
      filesList.replaceChildren(el("p", { class: "hint" }, "Niciun fisier. Trage fisiere aici sau apasa Incarca."));
      return;
    }
    filesList.replaceChildren(...files.map((f) =>
      el("div", { class: "file-row" },
        el("a", { href: `/api/files/${f.id}`, target: "_blank", rel: "noopener" }, f.name),
        el("span", { class: "hint" }, fmtSize(f.size)),
        el("button", {
          type: "button", class: "btn small", "aria-label": `Sterge ${f.name}`,
          onclick: () => removeFile(f),
        }, "x"))));
  }

  async function uploadProjectFiles(fileList) {
    const list = Array.from(fileList);
    if (!list.length) return;
    const fd = new FormData();
    for (const f of list) fd.append("files", f, f.name);
    try {
      await upload(`/api/projects/${projectId}/files`, fd);
    } catch (err) {
      toast(err.message);
    }
    await loadFiles();
  }

  async function removeFile(f) {
    if (!confirm(`Stergi fisierul ${f.name}?`)) return;
    try {
      await api(`/api/files/${f.id}`, { method: "DELETE" });
    } catch (err) {
      toast(err.message);
    }
    await loadFiles();
  }

  // drag & drop: dragover cu preventDefault, altfel browserul nu accepta drop-ul
  function setupDrop(node, onFiles) {
    let depth = 0; // dragenter/dragleave se declanseaza si pe copii
    dropResets.push(() => { depth = 0; node.classList.remove("dropping"); });
    node.addEventListener("dragenter", (e) => { if (hasFiles(e)) { depth++; node.classList.add("dropping"); } });
    node.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
    node.addEventListener("dragleave", () => { if (--depth <= 0) { depth = 0; node.classList.remove("dropping"); } });
    node.addEventListener("drop", (e) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth = 0;
      node.classList.remove("dropping");
      onFiles(e.dataTransfer.files);
    });
  }

  // garda la nivel de fereastra: un fisier scapat pe langa zone ar naviga browserul
  const guardOver = (e) => { if (hasFiles(e)) e.preventDefault(); };
  const guardDrop = (e) => {
    if (hasFiles(e)) e.preventDefault();
    dropResets.forEach((fn) => fn());
  };
  const guardEnd = () => dropResets.forEach((fn) => fn());
  window.addEventListener("dragover", guardOver);
  window.addEventListener("drop", guardDrop);
  window.addEventListener("dragend", guardEnd);

  // ---------- run-ul live al agentului selectat ----------
  function syncLive() {
    if (gone()) return;
    const a = selectedAgent();
    const runId = a?.activeRunId || null;
    if (runId === state.liveRunId) return;
    if (runId && runId === state.doneRunId) return; // run terminat, activeRunId e inca vechi
    stopLive();
    if (!runId) return;
    state.liveRunId = runId;
    const box = el("div", { class: "live" }, el("div", { class: "live-head" }, `${a.name} lucreaza acum`));
    thread.append(box);
    scrollDown();
    const render = liveRenderer(box);
    const es = new EventSource(`/api/runs/${runId}/events`);
    state.liveStream = es;
    es.onmessage = (msg) => {
      let ev;
      try { ev = JSON.parse(msg.data); } catch { return; }
      if (ev.type === "app_done") {
        state.doneRunId = runId;
        stopLive(); // raspunsul final vine ca mesaj normal in thread
        return;
      }
      render(ev);
      scrollDown();
    };
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) stopLive();
    };
  }

  function stopLive() {
    state.liveStream?.close();
    state.liveStream = null;
    state.liveRunId = null;
    thread.querySelector(".live")?.remove();
  }

  // ---------- actiuni ----------
  async function projectAction(what) {
    try {
      await api(`/api/projects/${projectId}/${what}`, { method: "POST" });
      await reload();
    } catch (err) {
      toast(err.message);
    }
  }

  async function taskAction(t, what) {
    try {
      await api(`/api/tasks/${t.id}/${what}`, { method: "POST" });
      await reload();
    } catch (err) {
      toast(err.message);
    }
  }

  // ---------- sincronizare live cu serverul ----------
  let reloadTimer;
  async function reload() {
    try {
      state.project = await api(`/api/projects/${projectId}`);
    } catch (err) {
      toast(err.message);
      return;
    }
    if (gone()) return;
    if (!selectedAgent()) { state.selected = firstRootId(); syncHash(); } // agent sters
    renderAll();
    syncLive();
  }
  const scheduleReload = () => {
    clearTimeout(reloadTimer);
    reloadTimer = setTimeout(reload, 150);
  };

  renderAll();
  loadFiles();
  await loadThread();
  syncLive();

  // stream-ul proiectului se deschide abia acum: pana aici nu exista nimic de inchis
  syncHash();
  if (gone()) {
    stopLive();
    window.removeEventListener("dragover", guardOver);
    window.removeEventListener("drop", guardDrop);
    window.removeEventListener("dragend", guardEnd);
    return () => {};
  }
  let hadError = false;
  const events = new EventSource(`/api/projects/${projectId}/events`);
  events.onmessage = (msg) => {
    let ev;
    try { ev = JSON.parse(msg.data); } catch { return; }
    if (ev.type === "message" && ev.data.agentId === state.selected) addMessage(ev.data);
    if (ev.type === "files") loadFiles();
    scheduleReload();
  };
  events.onerror = () => { hadError = true; };
  events.onopen = async () => {
    // dupa o reconectare (ex. restart server) reincarcam tot, am putut pierde evenimente
    if (!hadError) return;
    hadError = false;
    await reload();
    loadFiles();
    await loadThread();
    syncLive();
  };


  const cleanup = () => {
    disposed = true;
    editor?.destroy();
    window.removeEventListener("dragover", guardOver);
    window.removeEventListener("drop", guardDrop);
    window.removeEventListener("dragend", guardEnd);
    events.close();
    canvas.destroy();
    stopLive();
    clearTimeout(reloadTimer);
  };
  return { cleanup, update };
}
