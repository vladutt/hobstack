import { api, upload } from "./api.js";
import { el, avatar, pill, toast, clock, kindOf, agentBody, folderOf, field } from "./ui.js";
import { teamCanvas } from "./canvas.js";
import { agentEditor, editableKey } from "./agent.js";
import { liveRenderer } from "./live.js";
import { stagesEditor } from "./stages.js";
import { recurringPanel } from "./recurring.js";
import { questionBox } from "./questions.js";
import { stuckBox, refreshStuckAges } from "./stuck.js";
import { accessBox } from "./access.js";
import { taskModal } from "./task-modal.js";

const COLUMNS = [
  { title: "Queued", statuses: ["queued"] },
  { title: "In progress", statuses: ["in_progress"] },
  { title: "Review", statuses: ["review", "blocked"] },
  { title: "Done", statuses: ["done"] },
];

const ref = (t) => `T-${t.number}`;

// Tab-urile paginii de proiect; primul e cel implicit.
const TABS = [
  { id: "tasks", label: "Tasks" },
  { id: "team", label: "Team" },
  { id: "files", label: "Files" },
];

// parseWorkspaceRoute: restul hash-ului dupa #/p/3 -> { tab, agentId, taskId }.
// Link-urile vechi #/p/3/agent/7 duc in tab-ul Echipa, cu agentul 7 selectat.
// #/p/3/tasks/12 deschide modala task-ului 12 peste board.
export function parseWorkspaceRoute(rest) {
  const parts = (rest || "").split("/").filter(Boolean);
  // adresele vechi (in romana) duc in tab-urile noi
  const legacy = { "task-uri": "tasks", "echipa": "team", "fisiere": "files" };
  if (parts[0] in legacy) parts[0] = legacy[parts[0]];
  if (parts[0] === "agent") {
    return { tab: "team", agentId: /^\d+$/.test(parts[1] || "") ? Number(parts[1]) : null, taskId: null };
  }
  const tab = TABS.some((t) => t.id === parts[0]) ? parts[0] : TABS[0].id;
  const num = /^\d+$/.test(parts[1] || "") ? Number(parts[1]) : null;
  const agentId = tab === "team" ? num : null;
  const taskId = tab === "tasks" ? num : null;
  return { tab, agentId, taskId };
}

export async function workspaceView(root, projectId, route) {
  const state = {
    project: await api(`/api/projects/${projectId}`),
    tab: route.tab,
    panel: route.tab === "team" ? "details" : "chat", // panoul din dreapta: "details" sau "chat"
    selected: null,   // agentul selectat (chat si detalii)
    seen: new Set(),  // id-urile mesajelor afisate in thread
    folderFilter: null, // id-ul folderului filtrat pe board; null = toate
    liveRunId: null,
    liveStream: null,
    pending: [],      // fisiere alese pentru urmatorul mesaj
    doneRunId: null,  // ultimul run terminat: activeRunId poate ramane vechi pana la reload
  };
  // butoanele "Deschide": create o singura data si mutate in header la fiecare
  // redesenare, ca selectia de editor sa nu se piarda
  const info = await api("/api/info").catch(() => ({ editors: [] }));
  const openFolder = el("select", { class: "select-small", "aria-label": "Folder to open" });
  const openBar = buildOpenBar(info.editors || []);
  let disposed = false; // view-ul a fost inchis: nu mai deschidem stream-uri si nu mai desenam
  // view-ul e inchis daca s-a apelat curatarea sau daca #view a fost golit intre timp
  const gone = () => disposed || !header.isConnected;
  state.selected = state.project.agents.some((a) => a.id === route.agentId) ? route.agentId : firstRootId();
  let editor = null;     // formularul din Detalii
  let editorKey = "";    // editableKey pentru datele din formular
  let modal = null;      // modala task-ului deschis (adresa #/p/<id>/tasks/<taskId>)

  const hasFiles = (e) => Array.from(e.dataTransfer?.types || []).includes("Files");
  const dropResets = []; // resetari de contor + outline, apelate la orice drop/dragend
  // titlul proiectului sta in coloana din stanga, ca chat-ul sa inceapa din
  // varful paginii si sa incapa intreg pe ecran (inclusiv campul de scris)
  const header = el("div", { class: "ws-head" });
  const tabsNav = el("nav", { class: "tabs", "aria-label": "Project sections" });
  // intrebarile deschise ale agentilor ("Needs you"): containerul se creeaza o
  // singura data si nu se scoate din pagina, ca raspunsul tastat sa nu se piarda
  // Doua sub-containere create o data: intrebarile si task-urile blocate se
  // redeseneaza separat, ca o schimbare in lista de blocate sa nu stearga
  // raspunsul tastat la o intrebare.
  const needsTitle = el("h2", {});
  const questionsBox = el("div", { class: "needs-questions" });
  const stuckList = el("div", { class: "needs-stuck" });
  const accessList = el("div", { class: "needs-access" });
  const needsBox = el("section", { class: "needs-you", "aria-live": "polite", hidden: true },
    needsTitle, questionsBox, stuckList, accessList);
  // varstele task-urilor blocate ("N min") se actualizeaza la fiecare minut,
  // fara sa redesenam casetele
  const agesTimer = setInterval(() => refreshStuckAges(needsBox), 60000);
  let keepPanel = false; // urmatorul update() pastreaza panoul ales (vezi openChat)
  let questionsKey = null; // id-urile intrebarilor desenate ultima data in banda
  let stuckKey = null;     // id-urile task-urilor blocate desenate ultima data
  let accessKey = null;    // id:count ale cererilor de acces desenate ultima data
  let pageHead = null;  // capul paginii desenat ultima data (inlocuit la fiecare renderHeader)
  const canvas = teamCanvas({
    onSelect: (id) => select(id),
    onEdit: (id) => select(id, "details"),
    onChanged: () => reload(),
    onCreate: (pos, link) => createAgent(pos, link),
    onDelete: (id) => deleteAgent(id),
  });
  // pe Task-uri si Fisiere alegi cu cine vorbesti din banda cu echipa
  const strip = el("div", { class: "team-strip", "aria-label": "Team" });
  const board = el("div", { class: "board" });
  const count = el("span", { class: "hint" });
  const boardTools = el("div", { class: "actions" }); // + Task, Recurring, Coloane
  const editorSlot = el("div"); // editorul de coloane, deschis la cerere
  const folderChips = el("div", { class: "folder-chips", role: "group", "aria-label": "Filter by folder" });
  let boardPanel = null; // "stages" | "task" | "recurring" | null
  let recurring = null; // panoul Recurring cand e deschis ({ el, update })
  const boardHint = el("p", { class: "hint board-hint" }, "Every project is different: set up your own columns (work stages) with the Columns button.");
  const filesList = el("div", { class: "files-list" });
  const projectPicker = el("input", { type: "file", multiple: true, hidden: true });
  projectPicker.addEventListener("change", () => { uploadProjectFiles(projectPicker.files); projectPicker.value = ""; });
  const filesSection = el("section", { class: "section" },
    el("div", { class: "section-head" },
      el("h2", {}, "Files"),
      el("button", { type: "button", class: "btn small", onclick: () => projectPicker.click() }, "Upload"),
      projectPicker),
    filesList);
  setupDrop(filesSection, uploadProjectFiles);
  const consoleHead = el("div", { class: "console-head" });
  const thread = el("div", { class: "thread", "aria-live": "polite" });
  const input = el("textarea", { rows: 1, "aria-label": "Message" });
  const note = el("span", { class: "hint" });
  const chips = el("div", { class: "chips" });
  const chatPicker = el("input", { type: "file", multiple: true, hidden: true });
  chatPicker.addEventListener("change", () => { addPending(chatPicker.files); chatPicker.value = ""; });
  const clip = el("button", {
    type: "button", class: "clip", "aria-label": "Attach files", title: "Attach files",
    onclick: () => chatPicker.click(),
  }, "\u{1F4CE}");
  let sending = false; // trimitere in curs: blocheaza dublul submit
  const sendBtn = el("button", { type: "submit", class: "btn primary" }, "Send");
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
  // intrebarea deschisa a agentului selectat, deasupra campului de scris
  const chatQuestion = el("div", { class: "chat-question", hidden: true });
  let chatQuestionKey = null; // id-ul intrebarii desenate ultima data in chat
  const chatBox = el("div", { class: "chat" }, thread, chatQuestion, el("div", { class: "composer" }, chips, composer, note));
  const consoleBox = el("aside", { class: "console" }, consoleHead, panelTabs, detailsBox, chatBox);
  setupDrop(chatBox, addPending);

  const bodies = {
    "team": el("div", { class: "tab-body team-body" }, canvas.el),
    "tasks": el("section", { class: "section tab-body" },
      el("div", { class: "section-head" }, el("div", { class: "actions" }, el("h2", {}, "Tasks"), count), boardTools),
      editorSlot, folderChips, board, boardHint),
    "files": el("div", { class: "tab-body" }, filesSection),
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
    renderChatQuestion();
    renderNeeds();
    renderDetails();
  }

  // ---------- intrebari catre utilizator ----------
  // openQuestionOf: prima intrebare deschisa a agentului (sunt in ordinea sosirii)
  function openQuestionOf(agentId) {
    return (state.project.questions || []).find((q) => q.agentId === agentId) || null;
  }

  // questionNode: caseta unei intrebari, cu numele agentului si task-ul (T-<nr>) daca are
  function questionNode(q) {
    const p = state.project;
    const agent = p.agents.find((a) => a.id === q.agentId);
    const task = q.taskId ? p.tasks.find((t) => t.id === q.taskId) : null;
    return questionBox(q, {
      agentName: agent ? agent.name : "Agent",
      avatar: agent ? avatar(agent.name, kindOf(agent), 20) : null,
      taskRef: task ? ref(task) : null,
      onDone: reload,
    });
  }

  // stuckNode: caseta unui task blocat (nimeni nu-l misca)
  function stuckNode(s) {
    return stuckBox(s, {
      agent: state.project.agents.find((a) => a.id === s.agentId),
      projectId,
      onDone: reload,
      onOpen: openChat,
    });
  }

  // accessNode: caseta unei cereri de acces (un tool refuzat unui agent)
  function accessNode(r) {
    return accessBox(r, {
      agent: state.project.agents.find((a) => a.id === r.agentId),
      projectId,
      onDone: reload,
      onOpen: openChat,
    });
  }

  // openChat: link-ul "Open <agent>" duce pe agent cu panoul Chat. update()
  // ar deschide Detaliile, deci tinem minte alegerea pana trece prin update.
  // Daca adresa e deja cea din link nu vine hashchange: doar aratam Chat-ul.
  // Un click cu Cmd/Ctrl/Shift (tab sau fereastra noua) nu schimba pagina
  // curenta, deci nu tinem minte nimic.
  function openChat(href, e) {
    if (e && (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0)) return;
    state.panel = "chat";
    if (location.hash === href) { renderTabs(); scrollDown(true); return; }
    keepPanel = true;
  }

  // renderNeeds: banda "Needs you". Titlul si vizibilitatea se actualizeaza
  // mereu; intrebarile si task-urile blocate se redeseneaza fiecare doar cand
  // se schimba setul lor: reload-urile dese (SSE) ar sterge raspunsul pe care il scrii.
  function renderNeeds() {
    const questions = state.project.questions || [];
    const stuck = state.project.stuck || [];
    const access = state.project.access || [];
    const total = questions.length + stuck.length + access.length;
    needsTitle.textContent = `Needs you (${total})`;
    needsBox.hidden = !total;
    const qKey = questions.map((q) => q.id).join(",");
    if (qKey !== questionsKey) {
      questionsKey = qKey;
      questionsBox.replaceChildren(...questions.map(questionNode)); // replaceChildren nativ nu aplatizeaza array-uri
    }
    const sKey = stuck.map((s) => s.taskId).join(",");
    if (sKey !== stuckKey) {
      stuckKey = sKey;
      stuckList.replaceChildren(...stuck.map(stuckNode));
    }
    const aKey = access.map((r) => `${r.id}:${r.count}`).join(",");
    if (aKey !== accessKey) {
      accessKey = aKey;
      accessList.replaceChildren(...access.map(accessNode));
    }
  }

  // renderChatQuestion: intrebarea agentului selectat in chat; aceeasi regula:
  // redesenam doar cand se schimba intrebarea
  function renderChatQuestion() {
    const q = state.selected ? openQuestionOf(state.selected) : null;
    const key = q ? String(q.id) : "";
    if (key === chatQuestionKey) return;
    chatQuestionKey = key;
    chatQuestion.hidden = !q;
    chatQuestion.replaceChildren(...(q ? [questionNode(q)] : []));
  }

  // renderTabs: tab-ul activ al paginii si al panoului din dreapta
  function renderTabs() {
    ws.dataset.tab = state.tab;
    tabsNav.replaceChildren(...TABS.map((t) => el("a", {
      class: "tab", href: `#/p/${projectId}/${t.id}`,
      "aria-current": t.id === state.tab ? "page" : null,
    }, t.label)));
    for (const [id, body] of Object.entries(bodies)) body.hidden = id !== state.tab;
    strip.hidden = state.tab === "team";
    const team = state.tab === "team";
    const panel = team ? state.panel : "chat";
    panelTabs.hidden = !team;
    panelTabs.replaceChildren(...[["details", "Details"], ["chat", "Chat"]].map(([id, label]) =>
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
      openQuestionOf(a.id) ? el("span", { class: "ask-mark", title: "Waiting for your answer" }, "?") : null,
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
      detailsBox.replaceChildren(el("p", { class: "empty" }, "Pick an agent on the canvas, or double-click empty space to create one."));
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
    if (!confirm("You have unsaved changes to " + (selectedAgent()?.name || "agent") + ". Discard them?")) return false;
    editor.destroy();
    editor = null;
    return true;
  }

  // createAgent: agent nou pus pe canvas la pos. link = {parentId, detached}:
  // in echipa unui agent, legat de utilizator sau nelegat.
  async function createAgent(pos, { parentId, detached }) {
    if (!leaveEditor()) return;
    const names = new Set(state.project.agents.map((a) => a.name.toLowerCase()));
    let name = "New agent";
    for (let i = 2; names.has(name.toLowerCase()); i++) name = `New agent ${i}`;
    const body = agentBody({
      name, role: "", model: "sonnet", background: "", instructions: "", doneDef: "",
      tools: ["write"], responsibilities: "", parentId, reviewerId: null, detached,
      folderId: state.project.agents.find((x) => x.id === parentId)?.folderId ?? null, // ca seful lui
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
    if (!a || !confirm(`Delete agent ${a.name}?`)) return;
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
    if (state.tab !== "team" || !state.selected) return;
    const want = `#/p/${projectId}/team/${state.selected}`;
    if (location.hash !== want) history.replaceState(null, "", want);
  }

  // update: ruta s-a schimbat in acelasi proiect (alt tab sau alt agent)
  async function update(r) {
    const keep = keepPanel;
    keepPanel = false;
    syncModal(r.taskId);
    if (r.tab === "team" && state.tab !== "team" && !keep) state.panel = "details"; // intrarea pe Echipa arata Detaliile
    state.tab = r.tab;
    if (r.tab === "team" && r.agentId && r.agentId !== state.selected) {
      if (!state.project.agents.some((a) => a.id === r.agentId)) { renderTabs(); return; }
      if (!keep) state.panel = "details";
      await select(r.agentId);
      return;
    }
    renderTabs();
    syncHash();
    if (!chatBox.hidden) scrollDown(true);
  }

  function renderHeader() {
    const p = state.project;
    const folders = p.folders || [];
    const keep = openFolder.value;
    openFolder.replaceChildren(...folders.map((f) => el("option", { value: f.id }, f.name)));
    if (folders.some((f) => String(f.id) === keep)) openFolder.value = keep;
    openFolder.hidden = folders.length < 2;
    const head = el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" }, el("a", { href: "#/" }, "Projects"), " / ", p.name),
        el("div", { class: "actions" }, el("h1", {}, p.name), pill(p.status),
          p.autonomy ? el("span", { class: "pill pill-auto", title: "Agents don't ask you questions" }, "Autonomous") : null),
        p.goal && el("p", { class: "muted clamp" }, "Goal: " + p.goal),
        p.paused && el("p", { class: "hint" }, "The team is paused. Messages stay queued until you start it.")),
      el("div", { class: "head-actions" },
        el("div", { class: "actions" },
          el("a", { class: "btn", href: `#/p/${projectId}/edit` }, "Project instructions"),
          p.paused
            ? el("button", { class: "btn primary", onclick: () => projectAction("resume") }, "Start team")
            : el("button", { class: "btn", onclick: () => projectAction("stop") }, "Stop team")),
        openBar));
    // inlocuim doar capul paginii: banda "Needs you" ramane in pagina (altfel
    // campul in care scrii ar pierde focusul la fiecare reload)
    if (pageHead) pageHead.replaceWith(head);
    else header.replaceChildren(head, needsBox, tabsNav);
    pageHead = head;
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
      el("span", { class: "hint" }, "Open in:"),
      openFolder,
      el("button", { type: "button", class: "btn small", onclick: () => openIn("folder") }, "Folder"),
      el("button", { type: "button", class: "btn small", onclick: () => openIn("browser") }, "Browser"),
      editors.length > 0 && [select,
        el("button", { type: "button", class: "btn small", onclick: () => openIn("editor", select.value) }, "Editor")]);
  }

  async function openIn(target, editor) {
    try {
      await api(`/api/projects/${projectId}/open`, { method: "POST", body: { target, editor, folderId: openFolder.hidden ? null : Number(openFolder.value) } });
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
    const folders = p.folders || [];
    if (!folders.some((f) => f.id === state.folderFilter)) state.folderFilter = null;
    folderChips.hidden = folders.length < 2;
    const chip = (id, label) => el("button", {
      type: "button", class: "btn small" + (state.folderFilter === id ? " active" : ""),
      "aria-pressed": String(state.folderFilter === id),
      onclick: () => { state.folderFilter = id; renderBoard(); },
    }, label);
    folderChips.replaceChildren(chip(null, "All"), ...folders.map((f) => chip(f.id, f.name)));
    // task-urile fara agent apar mereu; celelalte dupa folderul agentului
    const tasks = p.tasks.filter((t) => !state.folderFilter || !t.assigneeId ||
      folderOf(p, byId.get(t.assigneeId))?.id === state.folderFilter);
    count.textContent = `${p.done} of ${p.total} done`;
    // replaceChildren nu ignora false (el() da): filtram noi
    // butoanele deschid si inchid panoul lor (editorul de coloane sau formularul de task)
    const toggle = (id, label, onclick) => el("button", {
      type: "button", class: "btn small" + (boardPanel === id ? " active" : ""),
      "aria-pressed": String(boardPanel === id), onclick,
    }, label);
    boardTools.replaceChildren(toggle("task", "+ Task", toggleTaskForm), toggle("recurring", "Recurring", toggleRecurring), toggle("stages", "Columns", toggleStages));
    if (!stages.length) {
      board.className = "board";
      board.replaceChildren(...COLUMNS.map((c) => column(c.title, tasks.filter((t) => c.statuses.includes(t.status)), byId)));
      boardHint.hidden = false;
      return;
    }
    boardHint.hidden = true;
    board.className = "board stages-board";
    const inStage = (s) => tasks.filter((t) => t.stageId === s.id && t.status !== "review" && t.status !== "done");
    // task-uri fara etapa inca deschise (create inainte de coloane): altfel n-ar aparea nicaieri
    const loose = tasks.filter((t) => !t.stageId && t.status !== "review" && t.status !== "done");
    board.replaceChildren(
      ...(loose.length ? [column("No stage", loose, byId)] : []),
      ...stages.map((s) => column(s.name, inStage(s), byId, s)),
      column("With lead", tasks.filter((t) => t.status === "review"), byId),
      column("Done", tasks.filter((t) => t.status === "done"), byId));
  }

  function column(title, items, byId, stage) {
    const agents = stage ? stage.agentIds.map((id) => byId.get(id)).filter(Boolean) : [];
    return el("div", { class: "col" },
      el("div", { class: "col-head" },
        el("span", {}, title, stage?.kind === "check" && el("span", { class: "tag col-kind" }, "check")),
        el("span", {}, items.length)),
      stage && el("div", { class: "col-agents" },
        agents.length ? agents.map((a) => avatar(a.name, kindOf(a), 18)) : el("span", { class: "badge-blocked" }, "no agents")),
      items.map((t) => taskCard(t, byId.get(t.assigneeId), byId)));
  }

  // boardPanel: ce e deschis deasupra board-ului: null, "stages", "task" sau "recurring"
  function showPanel(id, node) {
    boardPanel = node ? id : null;
    if (boardPanel !== "recurring") recurring = null;
    editorSlot.replaceChildren(...(node ? [node] : []));
    renderBoard(); // starea butoanelor
  }
  const closePanel = () => showPanel(null, null);

  function toggleStages() {
    if (boardPanel === "stages") return closePanel();
    showPanel("stages", stagesEditor({
      project: state.project,
      onSaved: async () => { closePanel(); await reload(); },
      onClose: closePanel,
    }));
  }

  function toggleRecurring() {
    if (boardPanel === "recurring") return closePanel();
    recurring = recurringPanel({ projectId, project: state.project, assignable, onClose: closePanel, onChanged: reload });
    showPanel("recurring", recurring.el);
  }

  // agentii care pot lua un task: legati; intr-o etapa, doar cei din etapa
  function assignable(stageId) {
    const p = state.project;
    const stage = stageId && (p.stages || []).find((s) => s.id === stageId);
    return p.agents.filter((a) => !a.detached && (!stage || stage.agentIds.includes(a.id)));
  }

  function toggleTaskForm() {
    if (boardPanel === "task") return closePanel();
    const p = state.project;
    const first = (p.stages || [])[0];
    const title = el("input", { type: "text", maxlength: 200, placeholder: "e.g. Login page", required: true });
    const description = el("textarea", { rows: 3, placeholder: "What needs to be done, acceptance criteria (optional)" });
    const who = el("select", {},
      first && el("option", { value: "" }, `First free agent in ${first.name}`),
      assignable(first?.id).map((a) => el("option", { value: a.id }, a.name + (a.role ? " \u00b7 " + a.role : ""))));
    const addBtn = el("button", { type: "submit", class: "btn primary" }, "Add");
    const form = el("form", { class: "section task-form", onsubmit: submit },
      el("h2", {}, "New task"),
      field("Title", title), field("Description", description),
      field("Assignee", who, (first ? "The task enters the first stage; " : "The agent picks it up automatically; ") +
        (p.autonomy ? "the root lead decides at the end (full autonomy)." : "you decide at the end.")),
      el("div", { class: "actions" }, el("span", { class: "grow" }),
        el("button", { type: "button", class: "btn", onclick: closePanel }, "Cancel"), addBtn));
    showPanel("task", form);
    title.focus();

    async function submit(e) {
      e.preventDefault();
      if (!title.value.trim()) return;
      addBtn.disabled = true;
      try {
        await api(`/api/projects/${projectId}/tasks`, { method: "POST", body: {
          title: title.value.trim(), description: description.value.trim(), assigneeId: Number(who.value) || 0,
        } });
        closePanel();
        await reload();
      } catch (err) {
        toast(err.message);
        addBtn.disabled = false;
      }
    }
  }

  // assign: utilizatorul muta un task in asteptare sau blocat la alt agent
  async function assign(t, agentId) {
    try {
      await api(`/api/tasks/${t.id}/assign`, { method: "POST", body: { agentId } });
      await reload();
      return true;
    } catch (err) { toast(err.message); await reload(); return false; }
  }

  // decide: utilizatorul decide un task creat de el, ajuns dupa ultima etapa.
  // feedback e obligatoriu la send_back (il scrie in modala). Intoarce true daca a reusit.
  async function decide(t, action, feedback = "") {
    if (action === "send_back" && !feedback.trim()) {
      toast(`Write what needs to change in ${ref(t)}.`);
      return false;
    }
    try {
      await api(`/api/tasks/${t.id}/decide`, { method: "POST", body: { action, feedback } });
      await reload();
      return true;
    } catch (err) { toast(err.message); return false; }
  }

  // stageName: la cine asteapta un task din review (reviewer-ul asignatului, altfel creatorul, altfel seful)
  // In modul autonom, un task din board il decide radacina asignatului (daca nu e chiar el).
  function stageName(t, who, byId) {
    if (t.byUser && !state.project.autonomy) return "you";
    if (t.byUser && t.reviewStage !== "reviewer") {
      let root = who;
      for (const seen = new Set(); root?.parentId && !seen.has(root.id);) { seen.add(root.id); root = byId.get(root.parentId) || root; }
      return root && root.id !== who?.id ? root.name : "you";
    }
    const target = t.reviewStage === "reviewer"
      ? byId.get(who?.reviewerId)
      : byId.get(t.creatorId) || byId.get(who?.parentId);
    return target ? target.name : null;
  }

  // taskCard: cardul compact de pe board; click / Enter / Space deschide modala
  // (prin adresa, ca refresh-ul si Back sa mearga)
  function taskCard(t, who, byId) {
    const blocked = t.status === "blocked";
    const stage = t.status === "review" ? stageName(t, who, byId) : null;
    const waiting = t.stageId && t.status === "queued"; // in etapa, il ia primul agent liber
    const stuck = (state.project.stuck || []).some((s) => s.taskId === t.id);
    return el("button", {
      type: "button", class: "task", "data-task-id": t.id,
      onclick: () => { location.hash = `#/p/${projectId}/tasks/${t.id}`; },
    },
      el("span", { class: "task-title" }, el("span", { class: "task-id" }, ref(t)),
        t.recurringId ? el("span", { class: "task-recurring", title: "From a recurring task", "aria-label": "From a recurring task" }, "\u21bb") : null,
        t.title),
      t.description ? el("span", { class: "task-desc" }, t.description) : null,
      el("span", { class: "task-who" },
        waiting ? el("span", { class: "hint" }, "queued") : [who ? avatar(who.name, kindOf(who), 20) : null, who ? who.name : "?"],
        blocked ? el("span", { class: "badge-blocked" }, "Blocked") : null,
        stuck ? el("span", { class: "badge-stuck", title: "No one is moving this task" }, "stuck") : null,
        t.filesChanged > 0 ? el("span", { class: "task-files" }, `${t.filesChanged} ${t.filesChanged === 1 ? "file" : "files"}`) : null,
        stage ? el("span", { class: "task-stage" }, `with ${stage}`) : null));
  }

  // syncModal: deschide / inchide modala dupa taskId din adresa
  function syncModal(taskId) {
    if (modal && modal.taskId === taskId) return;
    const closed = modal?.taskId ?? null;
    modal?.destroy();
    modal = null;
    if (taskId && !gone()) {
      modal = taskModal({
        projectId, taskId, project: state.project, info,
        onClose: () => { location.hash = `#/p/${projectId}/tasks`; },
        actions: {
          taskAction, decide, assign, assignable,
          // inchide modala (prin adresa) si deschide panoul Recurring
          openRecurring: () => {
            if (boardPanel !== "recurring") toggleRecurring();
            // dupa ce modala se inchide (focusul revine pe card), mutam focusul pe panou
            window.addEventListener("hashchange", () => recurring?.el.querySelector("button")?.focus(), { once: true });
            location.hash = `#/p/${projectId}/tasks`;
          },
          waitingOn: (t) => {
            const byId = new Map(state.project.agents.map((a) => [a.id, a]));
            return stageName(t, byId.get(t.assigneeId), byId);
          },
        },
      });
      return;
    }
    // focusul revine pe cardul task-ului inchis
    if (closed) board.querySelector(`[data-task-id="${closed}"]`)?.focus();
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
          ? "Detached: not working. Link it on the canvas to You or to a lead."
          : !boss
            ? "Direct thread. Give it goals here and it delegates to its team."
            : `Direct thread. ${boss.name} is copied on what you write.`)),
      a.detached ? null : el("button", {
        type: "button", class: "btn small new-chat", title: "Start a fresh session; earlier messages stay saved",
        onclick: () => newChat(a),
      }, "New chat"));
    input.placeholder = a.detached ? "Link the agent to message it" : isLead ? `Write to ${a.name}...` : `Message ${a.name}`;
    input.disabled = sendBtn.disabled = clip.disabled = !!a.detached;
    note.textContent = state.project.paused ? "The team is paused: the message waits until you start it." : "";
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
    renderChatQuestion();
    renderDetails();
    syncHash();
    if (!changed) return;
    await loadThread();
    syncLive();
  }

  async function loadThread() {
    const id = state.selected;
    if (!id) return;
    // implicit doar chat-ul curent (de la ultimul "New chat"); "Earlier messages" aduce tot
    const all = state.showAllFor === id;
    const data = all ? { messages: await api(`/api/agents/${id}/messages`), earlier: 0 }
      : await api(`/api/agents/${id}/messages?chat=1`);
    if (gone() || id !== state.selected) return; // s-a schimbat selectia intre timp
    stopLive();
    state.seen = new Set();
    thread.replaceChildren();
    if (data.earlier > 0) {
      thread.append(el("button", {
        type: "button", class: "btn small earlier",
        onclick: async () => { state.showAllFor = id; await loadThread(); syncLive(); },
      }, `Earlier messages (${data.earlier})`));
    }
    if (!data.messages.length) thread.append(el("div", { class: "empty" }, "No messages yet."));
    data.messages.forEach(addMessage);
    scrollDown(true);
  }

  // newChat: sesiune Claude noua cu agentul; istoricul ramane salvat (Earlier messages)
  async function newChat(a) {
    if (!confirm(`Start a new chat with ${a.name}? ${a.name} starts a fresh session and forgets this conversation. Earlier messages stay saved.`)) return;
    try {
      await api(`/api/agents/${a.id}/new-chat`, { method: "POST" });
      if (state.selected !== a.id) return;
      state.showAllFor = null;
      state.panel = "chat"; // chat-ul nou se vede imediat, chiar daca era deschis Details
      renderTabs();
      syncHash();
      await loadThread();
      syncLive();
    } catch (err) { toast(err.message); }
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
      el("span", { class: "msg-meta" }, `${mine ? "You" : m.author} · ${clock(m.createdAt)}`),
      m.text ? el("div", { class: "bubble" }, m.text) : null,
      m.attachments && m.attachments.length > 0 && el("div", { class: "attachments" }, m.attachments.map(attachmentNode)),
      mine && !m.delivered && el("span", { class: "pending" }, "queued"));
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
          type: "button", "aria-label": `Remove ${f.name}`,
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
      filesList.replaceChildren(el("p", { class: "hint" }, "No files. Drag files here or click Upload."));
      return;
    }
    filesList.replaceChildren(...files.map((f) =>
      el("div", { class: "file-row" },
        el("a", { href: `/api/files/${f.id}`, target: "_blank", rel: "noopener" }, f.name),
        el("span", { class: "hint" }, fmtSize(f.size)),
        el("button", {
          type: "button", class: "btn small", "aria-label": `Delete ${f.name}`,
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
    if (!confirm(`Delete file ${f.name}?`)) return;
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
    const box = el("div", { class: "live" }, el("div", { class: "live-head" }, `${a.name} is working now`));
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

  // taskAction: accept / unblock / nudge pe un task. Intoarce true daca a reusit.
  async function taskAction(t, what) {
    try {
      await api(`/api/tasks/${t.id}/${what}`, { method: "POST" });
      await reload();
      return true;
    } catch (err) {
      toast(err.message);
      return false;
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
    if (boardPanel === "recurring") recurring?.update(state.project); // formularul deschis nu se redeseneaza
    modal?.refresh(state.project); // modala deschisa isi reincarca task-ul
    syncLive();
  }
  const scheduleReload = () => {
    clearTimeout(reloadTimer);
    reloadTimer = setTimeout(reload, 150);
  };

  renderAll();
  syncModal(route.taskId); // refresh pe adresa unui task: modala se redeschide
  loadFiles();
  await loadThread();
  syncLive();

  // stream-ul proiectului se deschide abia acum: pana aici nu exista nimic de inchis
  syncHash();
  if (gone()) {
    stopLive();
    modal?.destroy();
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
    modal?.destroy();
    window.removeEventListener("dragover", guardOver);
    window.removeEventListener("drop", guardDrop);
    window.removeEventListener("dragend", guardEnd);
    events.close();
    canvas.destroy();
    stopLive();
    clearTimeout(reloadTimer);
    clearInterval(agesTimer);
  };
  return { cleanup, update };
}
