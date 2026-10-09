import { workRequestBox } from "./project-requests.js";
import { api, upload } from "./api.js";
import { el, avatar, toast, clock, kindOf, agentBody, folderOf, field, topbar } from "./ui.js";
import { teamCanvas } from "./canvas.js";
import { agentEditor, editableKey } from "./agent.js";
import { liveRenderer } from "./live.js";
import { stagesEditor } from "./stages.js";
import { recurringPanel } from "./recurring.js";
import { questionBox } from "./questions.js";
import { stuckBox, refreshStuckAges } from "./stuck.js";
import { accessBox } from "./access.js";
import { agentRequestBox } from "./agent-requests.js";
import { taskModal } from "./task-modal.js";
import { statsView } from "./stats.js";
import { renderMarkdown } from "./md.js";
import { libraryProposalBox, librarySharePrompt } from "./library-cards.js";

const SUB_STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
};
const COLUMNS = [
  { title: "Queued", statuses: ["queued"] },
  { title: "In progress", statuses: ["in_progress"] },
  { title: "Review", statuses: ["review", "blocked"] },
  { title: "Done", statuses: ["done"] },
];

const ref = (t) => `T-${t.number}`;

// Coloana Done arata la inceput atatea carduri; "Show N more" le aduce pe toate.
const DONE_PAGE = 12;

// savedDrawer / saveDrawer: panoul din dreapta ramane deschis sau inchis intre pagini.
// Pe ecrane inguste panoul acopera board-ul, deci acolo pornim mereu cu el inchis.
function savedDrawer() {
  if (window.innerWidth <= 1100) return false;
  try { return localStorage.getItem("foreman-drawer") === "1"; } catch { return false; }
}
function saveDrawer(on) {
  try { localStorage.setItem("foreman-drawer", on ? "1" : "0"); } catch {}
}

// Tab-urile paginii de proiect; primul e cel implicit.
const TABS = [
  { id: "tasks", label: "Tasks" },
  { id: "team", label: "Team" },
  { id: "files", label: "Files" },
  { id: "stats", label: "Stats" },
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
    mine: false,        // doar task-urile create de utilizator (din board / recurente)
    agentFilter: null,  // doar task-urile unui agent; null = toti
    liveRunId: null,
    liveStream: null,
    pending: [],      // fisiere alese pentru urmatorul mesaj
    doneRunId: null,  // ultimul run terminat: activeRunId poate ramane vechi pana la reload
    // panoul din dreapta (chat / detalii) e deschis; pe Echipa cu un agent in adresa se deschide singur
    drawer: route.tab === "team" && !!route.agentId || savedDrawer(),
    doneShown: DONE_PAGE, // cate carduri arata coloana Done (restul sub "Show N more")
  };
  // butoanele "Deschide": create o singura data si mutate in header la fiecare
  // redesenare, ca selectia de editor sa nu se piarda
  const info = await api("/api/info").catch(() => ({ editors: [] }));
  const openFolder = el("select", { class: "select-small", "aria-label": "Folder to open" });
  const openBar = buildOpenBar(info.editors || []);
  // meniul "Open" din bara de sus: creat o data, ca starea deschis/inchis sa ramana la reload
  const openMenu = el("details", { class: "menu" },
    el("summary", { class: "btn ghost", title: "Open the project folder, an editor or the browser" }, "Open"),
    el("div", { class: "menu-panel" }, openBar));
  const closeMenus = (e) => { if (!openMenu.contains(e.target)) openMenu.open = false; };
  document.addEventListener("pointerdown", closeMenus);
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
  const tabsNav = el("nav", { class: "top-tabs", "aria-label": "Project sections" });
  // intrebarile deschise ale agentilor ("Needs you"): containerul se creeaza o
  // singura data si nu se scoate din pagina, ca raspunsul tastat sa nu se piarda
  // Doua sub-containere create o data: intrebarile si task-urile blocate se
  // redeseneaza separat, ca o schimbare in lista de blocate sa nu stearga
  // raspunsul tastat la o intrebare.
  const needsTitle = el("h2", { class: "col-head needs-head" });
  const questionsBox = el("div", { class: "needs-questions" });
  const stuckList = el("div", { class: "needs-stuck" });
  const fillList = el("div", { class: "needs-fill" });
  const accessList = el("div", { class: "needs-access" });
  const agentReqList = el("div", { class: "needs-agent-requests" });
  const workReqList = el("div", { class: "needs-work-requests" });
  const libraryList = el("div", { class: "needs-library" });
  const accountList = el("div", { class: "needs-account" });
  const needsBox = el("section", { class: "needs-you", "aria-live": "polite", hidden: true },
    needsTitle, accountList, questionsBox, fillList, stuckList, accessList, agentReqList, workReqList, libraryList);
  // varstele task-urilor blocate ("N min") se actualizeaza la fiecare minut,
  // fara sa redesenam casetele
  const agesTimer = setInterval(() => refreshStuckAges(needsBox), 60000);
  let keepPanel = false; // urmatorul update() pastreaza panoul ales (vezi openChat)
  let questionsKey = null; // id-urile intrebarilor desenate ultima data in banda
  let fillKey = null;      // task-urile "To fill in" desenate ultima data
  let stuckKey = null;     // id-urile task-urilor blocate desenate ultima data
  let agentReqKey = null;  // id-urile cererilor de agent desenate ultima data
  let libraryKey = null;   // id-urile propunerilor Library desenate ultima data
  let workReqKey = null;   // id-urile cererilor de lucru desenate ultima data
  let accountKey = null;   // eroarea contului desenata ultima data
  let accessKey = null;    // id:count ale cererilor de acces desenate ultima data
  const canvas = teamCanvas({
    onSelect: (id) => select(id),
    onEdit: (id) => select(id, "details"),
    onChanged: () => reload(),
    onCreate: (pos, link) => createAgent(pos, link),
    onDelete: (id) => deleteAgent(id),
    onAdd: () => { location.hash = `#/p/${projectId}/agents/new` + (state.selected ? `?parent=${state.selected}` : ""); },
  });
  // in capul panoului din dreapta alegi cu cine vorbesti (tab-uri cu agentii)
  const strip = el("div", { class: "team-strip", role: "group", "aria-label": "Team" });
  // pe Echipa si Fisiere: "N need you" in bara de sus duce la board
  const needsChip = el("a", { class: "needs-chip", href: `#/p/${projectId}/tasks`, hidden: true });
  const board = el("div", { class: "board" });
  const count = el("span", { class: "board-count" });
  const boardTools = el("div", { class: "actions" }); // Recurring, Coloane
  const pausedNote = el("span", { class: "paused-note", hidden: true });
  const editorSlot = el("div"); // editorul de coloane, deschis la cerere
  const folderChips = el("div", { class: "folder-chips", role: "group", "aria-label": "Filter by folder" });
  const subsOpen = new Set(); // grupurile desfasurate pe board (id-ul radacinii); raman la refresh
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
  const chatShare = el("div", { class: "chat-share", hidden: true }); // propunerile Library ale agentului selectat
  let chatShareKey = null;
  let chatQuestionKey = null; // id-ul intrebarii desenate ultima data in chat
  const threadHint = el("div", { class: "thread-hint" });
  const quick = el("div", { class: "quick-actions" }); // Ask for status, Give a task, Open T-<nr>
  const chatBox = el("div", { class: "chat" }, threadHint, thread, chatShare, chatQuestion, el("div", { class: "composer" }, chips, composer, quick, note));
  const drawerClose = el("button", {
    type: "button", class: "drawer-close", "aria-label": "Close panel", title: "Close",
    onclick: () => setDrawer(false),
  }, "×");
  const consoleBox = el("aside", { class: "console", "aria-label": "Agent panel" },
    el("div", { class: "drawer-top" }, strip, drawerClose),
    consoleHead, panelTabs, detailsBox, chatBox);
  setupDrop(chatBox, addPending);

  // tab-ul Stats: datele se incarca doar la intrarea pe tab (fara auto-refresh)
  const stats = statsView(projectId, {
    agentKind: (id) => kindOf(state.project.agents.find((a) => a.id === id)),
  });
  const bodies = {
    "team": el("div", { class: "tab-body team-body" }, canvas.el),
    "tasks": el("section", { class: "tab-body tasks-body" },
      el("div", { class: "board-toolbar" }, folderChips, pausedNote, el("span", { class: "grow" }), boardTools, count),
      editorSlot,
      el("div", { class: "board-row" }, needsBox, board),
      boardHint),
    "files": el("div", { class: "tab-body files-body" }, filesSection),
    "stats": stats.el,
  };
  const ws = el("div", { class: "ws" },
    header,
    el("div", { class: "ws-body" },
      el("div", { class: "ws-main" }, Object.values(bodies)),
      consoleBox));
  root.append(ws);

  // setDrawer: deschide / inchide panoul din dreapta (chat si detalii)
  function setDrawer(on) {
    state.drawer = on;
    saveDrawer(on);
    applyDrawer();
    if (on && !chatBox.hidden) scrollDown(true);
  }
  function applyDrawer() {
    ws.classList.toggle("drawer-open", state.drawer);
    consoleBox.hidden = !state.drawer;
  }
  // openAgent: agentul in panou, pe Chat sau Details (bara de sus, avatarele)
  async function openAgent(id, panel) {
    if (state.drawer && id === state.selected && state.panel === panel) { setDrawer(false); return; }
    state.panel = panel;
    setDrawer(true);
    await select(id, panel);
    renderHeader();
  }

  // ---------- desenare ----------
  function renderAll() {
    renderHeader();
    applyDrawer();
    renderTabs();
    renderTeam();
    renderStrip();
    renderBoard();
    renderConsoleHead();
    renderChatQuestion();
    renderChatShare();
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

  // agentReqNode: caseta unei cereri add_agent (un lider vrea un agent nou)
  function agentReqNode(r) {
    return agentRequestBox(r, {
      lead: state.project.agents.find((a) => a.id === r.leadId),
      onDone: reload,
    });
  }

  // accountNode: contul Claude al proiectului a cazut la autentificare; proiectul e pe pauza
  function accountNode(p) {
    return el("div", { class: "question access" },
      el("div", { class: "question-head" },
        el("span", { class: "label" }, "CLAUDE ACCOUNT"),
        el("strong", {}, `Claude account ${p.accountName} failed`)),
      el("p", { class: "question-text" }, el("code", {}, p.accountError)),
      el("p", { class: "question-text hint" }, p.paused
        ? "The project is paused. Replace the token or check the account in Settings, then Start team."
        : "Agents can't run on this account until you replace the token or check it again in Settings."),
      el("div", { class: "actions" }, el("a", { class: "btn small primary", href: "#/settings" }, "Open Settings")));
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
    state.drawer = true;
    applyDrawer();
    if (location.hash === href) { renderTabs(); scrollDown(true); return; }
    keepPanel = true;
  }

  // fillNode: card "To fill in": task terminat care mai are puncte lasate utilizatorului
  function fillNode(f) {
    const agent = state.project.agents.find((a) => a.id === f.agentId);
    return el("div", { class: "question fill-in" },
      el("div", { class: "question-head" }, el("strong", {}, `To fill in \u00b7 ${f.open}`)),
      el("p", { class: "question-text fill-title" }, f.title),
      f.first ? el("p", { class: "question-text hint" }, f.first) : null,
      el("div", { class: "actions" },
        el("span", { class: "task-id" }, `T-${f.number}`), agent ? el("span", { class: "hint" }, agent.name) : null,
        el("span", { class: "grow" }),
        el("a", { class: "btn small primary", href: `#/p/${projectId}/tasks/${f.taskId}` }, "Fill in")));
  }

  // renderNeeds: banda "Needs you". Titlul si vizibilitatea se actualizeaza
  // mereu; intrebarile si task-urile blocate se redeseneaza fiecare doar cand
  // se schimba setul lor: reload-urile dese (SSE) ar sterge raspunsul pe care il scrii.
  function renderNeeds() {
    const questions = state.project.questions || [];
    const stuck = state.project.stuck || [];
    const access = state.project.access || [];
    const agentReqs = state.project.agentRequests || [];
    const workReqs = state.project.workRequests || [];
    const fills = state.project.fillIns || [];
    const proposals = state.project.libraryProposals || [];
    const total = proposals.length + questions.length + fills.length + stuck.length + access.length + agentReqs.length + workReqs.length + (state.project.accountError ? 1 : 0);
    needsTitle.replaceChildren(el("span", {}, "Needs you"), el("span", { class: "col-count" }, total));
    needsChip.textContent = `${total} ${total === 1 ? "needs" : "need"} you`;
    needsChip.hidden = !total || state.tab === "tasks";
    needsBox.hidden = !total;
    const qKey = questions.map((q) => q.id).join(",");
    if (qKey !== questionsKey) {
      questionsKey = qKey;
      questionsBox.replaceChildren(...questions.map(questionNode)); // replaceChildren nativ nu aplatizeaza array-uri
    }
    const fKey = fills.map((f) => f.taskId + ":" + f.open).join(",");
    if (fKey !== fillKey) {
      fillKey = fKey;
      fillList.replaceChildren(...fills.map(fillNode));
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
    const rKey = agentReqs.map((r) => r.id).join(",");
    if (rKey !== agentReqKey) {
      agentReqKey = rKey;
      agentReqList.replaceChildren(...agentReqs.map(agentReqNode));
    }
    const wKey = workReqs.map((r) => r.id).join(",");
    if (wKey !== workReqKey) {
      workReqKey = wKey;
      workReqList.replaceChildren(...workReqs.map((r) => workRequestBox(r, { onDone: reload })));
    }
    const lKey = proposals.map((p) => p.id).join(",");
    if (lKey !== libraryKey) {
      libraryKey = lKey;
      libraryList.replaceChildren(...proposals.map((p) => libraryProposalBox(p, { onDone: reload })));
    }
    const acKey = state.project.accountError ? `${state.project.accountName}:${state.project.accountError}` : "";
    if (acKey !== accountKey) {
      accountKey = acKey;
      accountList.replaceChildren(...(acKey ? [accountNode(state.project)] : []));
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

  // renderChatShare: propunerile Library deschise ale agentului selectat (cardul "Share to ... library?");
  // se redeseneaza doar cand se schimba setul (agentul sau propunerile)
  function renderChatShare() {
    const mine = state.selected ? (state.project.libraryProposals || []).filter((p) => p.madeByAgentId === state.selected) : [];
    const key = state.selected + ":" + mine.map((p) => p.id).join(",");
    if (key === chatShareKey) return;
    chatShareKey = key;
    chatShare.hidden = !mine.length;
    chatShare.replaceChildren(...mine.map((p) => librarySharePrompt(p, { onDone: reload })));
    if (mine.length) scrollDown();
  }

  // renderTabs: tab-ul activ al paginii si al panoului din dreapta
  function renderTabs() {
    ws.dataset.tab = state.tab;
    tabsNav.replaceChildren(...TABS.map((t) => el("a", {
      class: "tab", href: `#/p/${projectId}/${t.id}`,
      "aria-current": t.id === state.tab ? "page" : null,
    }, t.label)));
    for (const [id, body] of Object.entries(bodies)) body.hidden = id !== state.tab;
    const panel = state.panel;
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
      avatar(a.name, kindOf(a), 20),
      el("span", {}, a.name),
      openQuestionOf(a.id) ? el("span", { class: "ask-mark", title: "Waiting for your answer" }, "?") : null,
      (a.status || "").toLowerCase() === "working" ? el("span", { class: "dot working", title: "Working" }) : null)));
    strip.querySelector(".selected")?.scrollIntoView({ block: "nearest", inline: "nearest" });
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
    if (r.tab !== "team" && state.tab === "team" && !keep) state.panel = "chat"; // iesirea de pe Echipa: inapoi la chat
    const enteringStats = r.tab === "stats" && state.tab !== "stats";
    state.tab = r.tab;
    renderNeeds(); // eticheta "N need you" din bara de sus apare doar in afara board-ului
    if (r.tab === "team" && r.agentId && r.agentId !== state.selected) {
      if (!state.project.agents.some((a) => a.id === r.agentId)) { renderTabs(); return; }
      if (!keep) state.panel = "details";
      state.drawer = true;
      applyDrawer();
      await select(r.agentId);
      return;
    }
    if (keep) { state.drawer = true; applyDrawer(); }
    renderTabs();
    if (enteringStats) stats.load();
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
    const working = p.agents.filter((a) => (a.status || "").toLowerCase() === "working").length;
    const status = p.paused
      ? el("span", { class: "status-chip paused", title: "The team is paused: agents answer your messages but don't pick up tasks until you start it." },
        el("span", { class: "dot" }), "Paused")
      : el("span", { class: "status-chip" + (working ? " on" : "") },
        el("span", { class: "dot" + (working ? " working" : "") }),
        working ? `${working} ${working === 1 ? "agent" : "agents"} working` : "Idle");
    // avatarele echipei: click deschide chat-ul cu agentul ales (sau radacina)
    const shown = p.agents.slice(0, 4);
    const stack = el("button", {
      type: "button", class: "avatar-stack", title: "Chat with the team",
      onclick: () => openAgent(state.selected ?? firstRootId(), "chat"),
    }, shown.map((a) => avatar(a.name, kindOf(a), 30)),
      p.agents.length > shown.length ? el("span", { class: "avatar more", style: "--size:30px" }, `+${p.agents.length - shown.length}`) : null);
    const root = state.project.agents.find((a) => a.id === firstRootId());
    const chatOpen = state.drawer && state.panel === "chat" && state.selected === root?.id;
    header.replaceChildren(topbar(
      [["Projects", "#/"], [p.name, null]],
      el("span", { class: "topbar-status" }, status,
        p.autonomy ? el("span", { class: "status-chip auto", title: "Agents don't ask you questions; the root lead decides" }, "Autonomous") : null,
        p.accountName ? el("span", { class: "status-chip account", title: "Claude account used by this project's agents" }, p.accountName) : null,
        needsChip),
      el("span", { class: "grow" }),
      p.agents.length ? stack : null,
      tabsNav,
      openMenu,
      el("a", { class: "btn ghost", href: `#/p/${projectId}/edit`, title: "Goal, shared context, folders, linked projects" }, "Instructions"),
      p.paused
        ? el("button", { type: "button", class: "btn", onclick: () => projectAction("resume") }, "Start team")
        : el("button", { type: "button", class: "btn", onclick: () => projectAction("stop") }, "Stop team"),
      root ? el("button", {
        type: "button", class: "btn" + (chatOpen ? " active" : ""), "aria-pressed": String(chatOpen),
        onclick: () => openAgent(root.id, "chat"),
      }, `Chat with ${root.name}`) : null,
      el("button", { type: "button", class: "btn primary", onclick: newTaskFromTop }, "+ Task")));
    header.querySelector(".crumb.current").title = p.goal ? "Goal: " + p.goal : "";
  }

  // newTaskFromTop: "+ Task" din bara de sus: board-ul cu formularul deschis
  function newTaskFromTop() {
    if (state.tab !== "tasks") {
      window.addEventListener("hashchange", () => { if (boardPanel !== "task") toggleTaskForm(); }, { once: true });
      location.hash = `#/p/${projectId}/tasks`;
      return;
    }
    if (boardPanel !== "task") toggleTaskForm();
    else editorSlot.querySelector("input")?.focus();
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
    return el("div", { class: "open-bar" },
      el("span", { class: "menu-label" }, "Open in"),
      openFolder,
      el("button", { type: "button", class: "btn small", onclick: () => openIn("folder") }, "Folder"),
      el("button", { type: "button", class: "btn small", onclick: () => openIn("browser") }, "Browser"),
      editors.length > 0 && [select,
        el("button", { type: "button", class: "btn small", onclick: () => openIn("editor", select.value) }, "Editor")]);
  }

  async function openIn(target, editor) {
    openMenu.open = false;
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
    if (!p.agents.some((a) => a.id === state.agentFilter)) state.agentFilter = null;
    const chip = (id, label) => el("button", {
      type: "button", class: "seg" + (state.folderFilter === id && (id !== null || (!state.mine && !state.agentFilter)) ? " active" : ""),
      "aria-pressed": String(state.folderFilter === id),
      onclick: () => { state.folderFilter = id; if (id === null) { state.mine = false; state.agentFilter = null; } renderBoard(); },
    }, label);
    const mineChip = el("button", {
      type: "button", class: "seg" + (state.mine ? " active" : ""), "aria-pressed": String(state.mine),
      title: "Tasks you created", onclick: () => { state.mine = !state.mine; renderBoard(); },
    }, "Mine");
    // Filter: agentul ales; meniul e un <details> nou la fiecare desenare, deci se inchide singur
    const who = p.agents.find((a) => a.id === state.agentFilter);
    const filter = el("details", { class: "menu board-filter" },
      el("summary", { class: "seg" + (who ? " active" : "") }, who ? `Assignee: ${who.name}` : "Filter"),
      el("div", { class: "menu-panel filter-panel" },
        el("span", { class: "menu-label" }, "Assignee"),
        [null, ...p.agents].map((a) => el("button", {
          type: "button", class: "filter-item" + ((a?.id ?? null) === state.agentFilter ? " on" : ""),
          onclick: () => { state.agentFilter = a ? a.id : null; renderBoard(); },
        }, a ? [avatar(a.name, kindOf(a), 20), a.name] : "Everyone"))));
    folderChips.replaceChildren(chip(null, "All tasks"), mineChip,
      ...(folders.length > 1 ? folders.map((f) => chip(f.id, f.name)) : []), filter);
    pausedNote.hidden = !p.paused;
    pausedNote.replaceChildren(el("span", { class: "dot" }), "Paused: agents answer messages but don't pick up tasks. ",
      el("button", { type: "button", class: "link-btn", onclick: () => projectAction("resume") }, "Start team"));
    // task-urile fara agent apar mereu; celelalte dupa folderul agentului
    const passes = (t) => (!state.folderFilter || !t.assigneeId ||
      folderOf(p, byId.get(t.assigneeId))?.id === state.folderFilter) &&
      (!state.mine || t.byUser) && (!state.agentFilter || t.assigneeId === state.agentFilter);
    // doar in Done task-urile se grupeaza (arbore sau fir de lucru, din task-urile gata); in
    // celelalte coloane fiecare task are cardul lui, in coloana statusului sau
    const single = (t) => ({ root: t, subs: [], follows: [] });
    const groups = [
      ...p.tasks.filter((t) => t.status !== "done").map(single),
      ...doneGroups(taskGroups(p.tasks)),
    ].filter((g) => passes(g.root) || g.subs.some((s) => passes(s.task)));
    // in fiecare coloana, cele mai recente primele: ultima activitate din grup (radacina sau
    // orice alt membru); la egalitate, task-ul creat mai tarziu (numar mai mare)
    const lastActivity = (g) => Math.max(Date.parse(g.root.updatedAt) || 0,
      ...g.subs.map((s) => Date.parse(s.task.updatedAt) || 0));
    for (const g of groups) g.last = lastActivity(g);
    groups.sort((a, b) => b.last - a.last || b.root.number - a.root.number);
    // coloana unui task, ca cheie: fara etape dupa status; cu etape etapa / "La lider" / "Gata" / "Fara etapa"
    const colKey = (t) => {
      if (!stages.length) return "c" + COLUMNS.findIndex((c) => c.statuses.includes(t.status));
      if (t.status === "done") return "done";
      if (t.status === "review") return "lead";
      return t.stageId ? "s" + t.stageId : "loose";
    };
    for (const g of groups) g.key = colKey(g.root);
    const inCol = (key) => groups.filter((g) => g.key === key);
    count.textContent = p.total ? `${p.done} of ${p.total} done` : "No tasks yet";
    // replaceChildren nu ignora false (el() da): filtram noi
    // butoanele deschid si inchid panoul lor (editorul de coloane sau formularul de task)
    const toggle = (id, label, onclick) => el("button", {
      type: "button", class: "btn ghost small" + (boardPanel === id ? " active" : ""),
      "aria-pressed": String(boardPanel === id), onclick,
    }, label);
    boardTools.replaceChildren(toggle("recurring", "\u21bb Recurring", toggleRecurring), toggle("stages", "Columns", toggleStages));
    if (!stages.length) {
      board.className = "board";
      board.replaceChildren(...COLUMNS.map((c, i) => column(c.title, inCol("c" + i), byId, null,
        { add: i === 0, page: i === COLUMNS.length - 1 })));
      boardHint.hidden = false;
      return;
    }
    boardHint.hidden = true;
    board.className = "board stages-board";
    // task-uri fara etapa inca deschise (create inainte de coloane): altfel n-ar aparea nicaieri
    const loose = inCol("loose");
    board.replaceChildren(
      ...(loose.length ? [column("No stage", loose, byId)] : []),
      ...stages.map((s, i) => column(s.name, inCol("s" + s.id), byId, s, { add: i === 0 })),
      column("With lead", inCol("lead"), byId),
      column("Done", inCol("done"), byId, null, { page: true }));
  }

  // doneGroups: grupurile din Done, din grupurile complete (arbore + fir): raman doar membrii gata.
  // Daca radacina nu e gata, cardul e primul membru gata (dupa numar), iar restul stau sub el.
  // Un membru al carui parinte nu e in grup (e deschis) nu se indenteaza: adancimea ar minti.
  function doneGroups(all) {
    const out = [];
    for (const g of all) {
      let subs = g.subs.filter((s) => s.task.status === "done");
      let root = g.root;
      let follows = g.follows.filter((t) => t.status === "done");
      if (root.status !== "done") {
        if (!subs.length) continue;
        const [first, ...rest] = [...subs].sort((a, b) => a.task.number - b.task.number);
        root = first.task;
        subs = rest;
        follows = [];
      }
      const ids = new Set([root.id, ...subs.map((s) => s.task.id)]);
      subs = subs.map((s) => (s.follow || ids.has(s.task.parentTaskId) ? s : { ...s, depth: 0 }));
      out.push({ root, subs, follows });
    }
    return out;
  }

  // column: o coloana a board-ului; items sunt grupuri (radacina + descendentii ei).
  // opts.add: "+ Add a task" sub carduri; opts.page: arata doar primele state.doneShown carduri.
  function column(title, items, byId, stage, opts = {}) {
    const agents = stage ? stage.agentIds.map((id) => byId.get(id)).filter(Boolean) : [];
    const visible = opts.page ? items.slice(0, state.doneShown) : items;
    const hidden = items.length - visible.length;
    return el("div", { class: "col" + (items.length ? "" : " empty-col") },
      el("div", { class: "col-head" },
        el("span", { class: "col-title" }, title, stage?.kind === "check" && el("span", { class: "tag col-kind" }, "check")),
        el("span", { class: "col-count" }, items.length)),
      stage && el("div", { class: "col-agents" },
        agents.length ? agents.map((a) => avatar(a.name, kindOf(a), 18)) : el("span", { class: "badge-blocked" }, "no agents")),
      el("div", { class: "col-cards" },
        visible.map((g) => groupCard(g, byId)),
        hidden > 0 ? el("button", {
          type: "button", class: "col-more",
          onclick: () => { state.doneShown = Infinity; renderBoard(); },
        }, `Show ${hidden} more`) : null,
        opts.add ? el("button", { type: "button", class: "col-add", onclick: newTaskFromTop }, "+ Add a task") : null));
  }

  // taskGroups: grupurile board-ului. Intai arborii: radacinile (fara parinte, cu parinte lipsa
  // sau in ciclu) si toti descendentii lor, la orice adancime. Apoi firele de lucru: un arbore
  // a carui radacina are threadId catre alt task din proiect intra in grupul arborelui acelui
  // task (follow-up). Intoarce [{ root, subs: [{ task, depth, follow }], follows }]
  // in ordinea task-urilor primite; follows sunt radacinile follow-up-urilor, dupa numar.
  function taskGroups(tasks) {
    const byTask = new Map(tasks.map((t) => [t.id, t]));
    const kids = new Map(); // parinte -> copiii lui
    const tops = [];
    for (const t of tasks) {
      if (t.parentTaskId && t.parentTaskId !== t.id && byTask.has(t.parentTaskId)) {
        if (!kids.has(t.parentTaskId)) kids.set(t.parentTaskId, []);
        kids.get(t.parentTaskId).push(t);
      } else {
        tops.push(t);
      }
    }
    const seen = new Set();
    const treeOf = new Map(); // task -> arborele lui
    const walk = (id, depth, out) => {
      for (const c of kids.get(id) || []) {
        if (seen.has(c.id)) continue;
        seen.add(c.id);
        out.push({ task: c, depth });
        walk(c.id, depth + 1, out);
      }
    };
    const tree = (root) => {
      seen.add(root.id);
      const subs = [];
      walk(root.id, 0, subs);
      const tr = { root, subs };
      treeOf.set(root.id, tr);
      for (const s of subs) treeOf.set(s.task.id, tr);
      return tr;
    };
    const trees = tops.map(tree);
    // ciclu in parent_task_id (date stricate): ce n-a fost atins devine radacina, ca sa nu pierdem task-ul
    for (const t of tasks) if (!seen.has(t.id)) trees.push(tree(t));

    // hostOf: arborele in al carui grup intra un arbore. thread_id e mereu canonic (pornirea
    // firului), dar daca tinta e la randul ei un follow-up urcam mai departe; o tinta lipsa
    // opreste urcarea (ultimul arbore gasit e gazda). Un ciclu in thread_id (date stricate):
    // gazda e arborele din ciclu cu numarul de radacina cel mai mic, acelasi pentru toti
    // arborii care ajung in ciclu, ca exact unul sa gazduiasca restul.
    const hostOf = (tr) => {
      const path = [tr];
      for (;;) {
        const cur = path[path.length - 1];
        const target = cur.root.threadId && byTask.get(cur.root.threadId);
        const next = target && treeOf.get(target.id);
        if (!next) return cur;
        const at = path.indexOf(next);
        if (at >= 0) {
          return path.slice(at).reduce((a, b) => (b.root.number < a.root.number ? b : a));
        }
        path.push(next);
      }
    };
    const followers = new Map(); // arbore gazda -> arborii follow-up
    for (const tr of trees) {
      const host = hostOf(tr);
      if (host === tr) continue;
      if (!followers.has(host)) followers.set(host, []);
      followers.get(host).push(tr);
    }
    const groups = [];
    const shown = new Set(); // arborii deja pusi intr-un grup
    const emit = (tr, fs) => {
      fs.sort((a, b) => a.root.number - b.root.number);
      const subs = [...tr.subs];
      shown.add(tr);
      for (const f of fs) {
        shown.add(f);
        subs.push({ task: f.root, depth: 0, follow: true });
        for (const s of f.subs) subs.push({ task: s.task, depth: s.depth + 1 });
      }
      groups.push({ root: tr.root, subs, follows: fs.map((f) => f.root) });
    };
    for (const tr of trees) {
      if (hostOf(tr) !== tr) continue; // e deja in grupul firului lui
      emit(tr, followers.get(tr) || []);
    }
    // plasa de siguranta: un arbore neatins (n-ar trebui) devine grupul lui, ca sa nu pierdem task-uri
    for (const tr of trees) if (!shown.has(tr)) emit(tr, []);
    return groups;
  }

  // groupCard: cardul radacinii plus "▸ 3 more done", care desfasoara restul membrilor ca randuri
  // compacte (doar Done se grupeaza); click pe un rand deschide modala task-ului (ca un card)
  function groupCard(g, byId) {
    const members = new Set([g.root.id, ...g.subs.map((s) => s.task.id)]);
    const card = taskCard(g.root, byId.get(g.root.assigneeId), byId, members);
    if (!g.subs.length) return card;
    const n = g.subs.length;
    const label = (open) => `${open ? "\u25be" : "\u25b8"} ${n} more done`;
    const listId = `task-subs-${g.root.id}`;
    const list = el("ul", { class: "task-subs", id: listId, hidden: !subsOpen.has(g.root.id) },
      g.subs.map(({ task, depth, follow }) => {
        const who = byId.get(task.assigneeId);
        // follow: radacina unui follow-up din fir (separata vizual de randul de deasupra)
        return el("li", { style: `--depth:${depth}`, class: follow ? "follow" : null },
          el("button", {
            type: "button", class: "task-sub", "data-sub-id": task.id,
            onclick: () => { location.hash = `#/p/${projectId}/tasks/${task.id}`; },
          },
            el("span", { class: "task-id" }, ref(task)),
            el("span", { class: "task-sub-title" }, task.title),
            who ? el("span", { class: "hint" }, who.name) : null,
            el("span", { class: `pill pill-task-${task.status}` }, SUB_STATUS[task.status] || task.status)));
      }));
    const btn = el("button", {
      type: "button", class: "subs-toggle", "aria-controls": listId, "aria-expanded": String(subsOpen.has(g.root.id)),
      onclick: () => {
        if (subsOpen.has(g.root.id)) subsOpen.delete(g.root.id); else subsOpen.add(g.root.id);
        const open = subsOpen.has(g.root.id);
        list.hidden = !open;
        btn.setAttribute("aria-expanded", String(open));
        btn.textContent = label(open);
      },
    }, label(subsOpen.has(g.root.id)));
    const head = el("div", { class: "subs-head" }, btn);
    return el("div", { class: "task-group" }, card, head, list);
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
  // (prin adresa, ca refresh-ul si Back sa mearga). Sus etichetele (proiect legat,
  // recurent), titlul, o linie de stare (cine lucreaza, la cine asteapta), jos
  // T-<nr>, cine si fisierele, cu avatarul asignatului in dreapta.
  // relationTag: pe un card deschis, de cine tine task-ul (sub-task al lui T-3 / follow-up al lui T-3);
  // in Done etichetele apar doar cand parintele / firul nu e in grup (members = ids din grup)
  function relationTag(t, members) {
    const of = (id) => state.project.tasks.find((x) => x.id === id && !(t.status === "done" && members?.has(id)));
    const parent = t.parentTaskId && t.parentTaskId !== t.id ? of(t.parentTaskId) : null;
    if (parent) return el("span", { class: "label-tag", title: `Subtask of ${ref(parent)}: ${parent.title}` }, `\u21b3 ${ref(parent)}`);
    const start = t.threadId && t.threadId !== t.id ? of(t.threadId) : null;
    if (start) return el("span", { class: "label-tag", title: `Follow-up of ${ref(start)}: ${start.title}` }, `follow-up of ${ref(start)}`);
    return null;
  }

  // idleReason: de ce nu lucreaza nimeni acum pe un task In progress (primul motiv care se aplica)
  function idleReason(t, who, byId, kids, stuck) {
    const p = state.project;
    const line = (cls, text) => el("span", { class: "task-line " + cls }, text);
    if ((p.questions || []).some((q) => q.taskId === t.id)) return line("warn", "Waiting for your answer");
    if ((p.access || []).some((r) => r.taskId === t.id)) return line("warn", "Waiting for your permission");
    const open = kids.filter((x) => x.status !== "done").sort((a, b) => a.number - b.number);
    if (open.length > 1) return line("idle", `Waiting on ${open.length} subtasks`);
    if (open.length === 1) {
      const sub = open[0], subWho = byId.get(sub.assigneeId);
      return line("idle", `Waiting on ${ref(sub)}` + (subWho ? ` \u00b7 ${subWho.name}` : ""));
    }
    if (p.paused) return line("idle", "Team paused");
    if (who && who.workingTaskId && who.workingTaskId !== t.id) {
      const other = p.tasks.find((x) => x.id === who.workingTaskId);
      return line("idle", other ? `${who.name} is busy on ${ref(other)}` : `${who.name} is busy`);
    }
    if (stuck) return line("idle", "No one is working on it");
    return line("idle", who ? `Waiting for ${who.name}` : "Waiting for an agent");
  }

  function taskCard(t, who, byId, members) {
    const blocked = t.status === "blocked";
    const stage = t.status === "review" ? stageName(t, who, byId) : null;
    const waiting = t.stageId && t.status === "queued"; // in etapa, il ia primul agent liber
    const stuck = (state.project.stuck || []).some((s) => s.taskId === t.id);
    const decides = t.status === "review" && (state.project.userDecides || []).includes(t.id);
    // workingTaskId = task-ul run-ului activ; lucreaza orice agent, nu doar asignatul
    const worker = (state.project.agents || []).find((a) => a.workingTaskId === t.id);
    const working = !!worker;
    // progres pe un task deschis cu sub-task-uri (copiii directi)
    const kids = t.status === "done" ? [] : state.project.tasks.filter((x) => x.parentTaskId === t.id && x.id !== t.id);
    const kidsDone = kids.filter((x) => x.status === "done").length;
    let line = null;
    if (blocked) line = el("span", { class: "task-line bad" }, "Blocked");
    else if (working) line = el("span", { class: "task-line working" }, el("span", { class: "dot working" }), `${worker.name} \u00b7 working`);
    else if ((state.project.fillIns || []).some((f) => f.taskId === t.id)) line = el("span", { class: "task-line warn" }, "Waiting on you");
    else if (decides) line = el("span", { class: "task-line warn" }, "Waiting for you to decide");
    else if (stage) line = el("span", { class: "task-line warn" }, `With ${stage}`);
    else if (t.status === "in_progress") line = idleReason(t, who, byId, kids, stuck);
    const labels = [
      t.fromProject ? el("span", { class: "label-tag linked", title: `Requested by ${t.fromProject}` }, `\u2194 ${t.fromProject}`) : null,
      relationTag(t, members),
      t.recurringId ? el("span", { class: "label-tag recurring", title: "From a recurring task" }, "\u21bb recurring") : null,
      stuck ? el("span", { class: "label-tag stuck", title: "No one is moving this task" }, "stuck") : null,
    ].filter(Boolean);
    const files = t.filesChanged > 0 ? `${t.filesChanged} ${t.filesChanged === 1 ? "file" : "files"}` : null;
    return el("button", {
      type: "button", class: "task" + (blocked ? " is-blocked" : "") + (decides ? " is-yours" : ""), "data-task-id": t.id,
      onclick: () => { location.hash = `#/p/${projectId}/tasks/${t.id}`; },
    },
      labels.length ? el("span", { class: "task-labels" }, labels) : null,
      el("span", { class: "task-title" }, t.title),
      line,
      el("span", { class: "task-foot" },
        el("span", { class: "task-id" }, ref(t)),
        kids.length ? el("span", { class: "task-meta", title: "Subtasks done" }, `${kidsDone}/${kids.length} subtasks`) : null,
        t.attachments > 0 ? el("span", { class: "task-meta", title: `${t.attachments} attached ${t.attachments === 1 ? "file" : "files"}` }, `📎 ${t.attachments}`) : null,
        waiting ? el("span", { class: "task-meta" }, "queued in stage")
          : files ? el("span", { class: "task-meta" }, files)
          : who && !working ? el("span", { class: "task-meta" }, who.name) : null,
        el("span", { class: "grow" }),
        waiting || !who
          ? el("span", { class: "avatar empty", style: "--size:24px", title: "Unassigned" })
          : el("span", { title: who.name, class: "task-avatar" }, avatar(who.name, kindOf(who), 24))));
  }

  // syncModal: deschide / inchide modala dupa taskId din adresa
  function syncModal(taskId) {
    if (modal && modal.taskId === taskId) return;
    const closed = modal?.taskId ?? null;
    // un link din Flow muta modala pe alt task: ramanem pe Flow, cu pasii desfasurati
    const keepFlow = taskId && modal?.tab() === "flow" ? { initialTab: "flow", flowOpen: modal.flowOpen } : {};
    modal?.destroy();
    modal = null;
    if (taskId && !gone()) {
      modal = taskModal({
        projectId, taskId, project: state.project, info, ...keepFlow,
        onClose: () => { location.hash = `#/p/${projectId}/tasks`; },
        actions: {
          taskAction, decide, assign, assignable,
          // inchide modala (prin adresa) si deschide panoul Recurring
          openRecurring: () => {
            if (boardPanel !== "recurring") toggleRecurring();
            // dupa ce modala se inchide (focusul revine pe card), mutam focusul pe panou
            window.addEventListener("hashchange", () => recurring?.el.querySelector("button:not([hidden])")?.focus(), { once: true });
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
    const model = (info.models || []).find((m) => m.id === a.model)?.label.replace(/ \(latest\)$/, "") || a.model;
    const status = (a.status || "Idle").toLowerCase();
    const cur = a.currentTask;
    consoleHead.replaceChildren(
      avatar(a.name, kindOf(a), 40),
      el("div", { class: "console-who" },
        el("div", { class: "member-name" }, a.name),
        el("div", { class: "member-sub" }, [a.role, a.detached ? "detached" : boss ? `reports to ${boss.name}` : "reports to you", model]
          .filter(Boolean).join(" \u00b7 "))),
      el("span", { class: "agent-status " + status },
        el("span", { class: "dot " + status }),
        a.status || "Idle", cur ? ` \u00b7 T-${cur.number}` : null),
      a.detached ? null : el("button", {
        type: "button", class: "btn ghost small new-chat", title: "Start a fresh session; earlier messages stay saved",
        onclick: () => newChat(a),
      }, "New chat"));
    threadHint.textContent = a.detached
      ? "Detached: not working. Link it on the canvas to You or to a lead."
      : !boss
        ? "Direct thread. Give it goals here and it delegates to its team."
        : `Direct thread. ${boss.name} is copied on what you write.`;
    input.placeholder = a.detached ? "Link the agent to message it" : isLead ? `Write to ${a.name}...` : `Message ${a.name}...`;
    const qa = (label, onclick, title) => el("button", { type: "button", class: "quick", onclick, title }, label);
    quick.replaceChildren(...(a.detached ? [] : [
      qa("Ask for status", () => sendText(`What's your status? Briefly: what you're working on, what's blocked, what's next.`), "Sends a short status question"),
      qa("Give a task", () => giveTask(a.id), `New task assigned to ${a.name}`),
      cur ? qa(`Open T-${cur.number}`, () => { location.hash = `#/p/${projectId}/tasks/${cur.id}`; }, cur.title) : null,
    ].filter(Boolean)));
    input.disabled = sendBtn.disabled = clip.disabled = !!a.detached;
    note.textContent = state.project.paused ? "The team is paused: agents answer your messages but don't pick up tasks until you start it." : "";
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
    renderChatShare();
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
      // mesajele agentilor vin in markdown; ale utilizatorului raman text simplu
      m.text ? el("div", { class: "bubble" + (mine ? "" : " md-bubble") }, mine ? m.text : renderMarkdown(m.text)) : null,
      m.attachments && m.attachments.length > 0 && el("div", { class: "attachments" }, m.attachments.map(attachmentNode)),
      mine && !m.delivered && el("span", { class: "pending" }, "queued"));
  }

  function scrollDown(force) {
    const nearBottom = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (force || nearBottom) thread.scrollTop = thread.scrollHeight;
  }

  // sendText: un mesaj gata scris (actiunile rapide); campul de scris ramane cum era
  async function sendText(text) {
    if (!state.selected || sending) return;
    const agentId = state.selected;
    sending = true;
    try {
      const m = await api(`/api/agents/${agentId}/messages`, { method: "POST", body: { text } });
      if (state.selected === agentId) { addMessage(m); scrollDown(true); }
    } catch (err) {
      toast(err.message);
    } finally {
      sending = false;
    }
  }

  // giveTask: formularul de task pe board, cu agentul deja ales
  function giveTask(agentId) {
    const open = () => {
      if (boardPanel !== "task") toggleTaskForm();
      const sel = editorSlot.querySelector("select");
      if (sel && [...sel.options].some((o) => o.value === String(agentId))) sel.value = String(agentId);
      editorSlot.querySelector("input")?.focus();
    };
    if (state.tab === "tasks") { open(); return; }
    window.addEventListener("hashchange", open, { once: true });
    location.hash = `#/p/${projectId}/tasks`;
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
    const head = el("div", { class: "live-head" }, `${a.name} is working now`);
    const box = el("div", { class: "live" }, head);
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
      // capul blocului arata ultimul tool folosit ("Forge is working · Edit CLAUDE.md")
      const tool = ev.type === "assistant" ? (ev.message?.content || []).filter((b) => b.type === "tool_use").pop() : null;
      if (tool) head.textContent = `${a.name} is working \u00b7 ${toolLabel(tool)}`;
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
    document.removeEventListener("pointerdown", closeMenus);
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
    document.removeEventListener("pointerdown", closeMenus);
    canvas.destroy();
    stopLive();
    clearTimeout(reloadTimer);
    clearInterval(agesTimer);
  };
  if (state.tab === "stats") stats.load(); // pagina deschisa direct pe #/p/3/stats
  return { cleanup, update };
}

// toolLabel: "editing CLAUDE.md", "running npm test", "reading app.js" (capul blocului live)
function toolLabel(block) {
  const i = block.input || {};
  const file = (i.file_path || "").split("/").pop();
  switch (block.name) {
    case "Edit": case "MultiEdit": case "Write": return `editing ${file}`;
    case "Read": return `reading ${file}`;
    case "Bash": return `running ${String(i.command || "").split("\n")[0].slice(0, 60)}`;
    case "Grep": case "Glob": return `searching ${i.pattern || ""}`.trim();
    case "WebSearch": case "WebFetch": return "on the web";
    default: return block.name.replace(/^mcp__team__/, "").replaceAll("_", " ");
  }
}
