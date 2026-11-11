import { workRequestBox } from "./project-requests.js";
import { api, upload } from "./api.js";
import { openButtons } from "./open-file.js";
import { openImage, closeImage } from "./image-viewer.js";
import { pathLinker } from "./paths.js";
import { mcpRequestBox } from "./mcp-requests.js";
import { el, avatar, toast, clock, kindOf, agentBody, field, topbar } from "./ui.js";
import { teamCanvas } from "./canvas.js";
import { agentEditor, editableKey } from "./agent.js";
import { liveRenderer } from "./live.js";
import { stagesEditor } from "./stages.js";
import { recurringPanel } from "./recurring.js";
import { recurringHistory } from "./recurring-history.js";
import { questionBox } from "./questions.js";
import { stuckBox, refreshStuckAges } from "./stuck.js";
import { accessBox } from "./access.js";
import { saveCategory } from "./category.js";
import { agentRequestBox } from "./agent-requests.js";
import { taskModal } from "./task-modal.js";
import { statsView } from "./stats.js";
import { brainstormView } from "./brainstorm.js";
import { renderMarkdown, userText, collapsible } from "./md.js";
import { attachMentions } from "./mentions.js";
import { openCodeExplorer, shortcutLabel, modKey } from "./code-explorer.js";
import { libraryProposalBox, librarySharePrompt } from "./library-cards.js";
import { moveAction, moveTask } from "./board-move.js";
import { deletedPanel } from "./deleted.js";
import { onPasteFiles, fileThumb, releaseThumb } from "./paste.js";

// mesajele lungi din chat se arata restranse: liniile vizibile (mesaj al utilizatorului / bloc de cod al agentului)
const USER_LINES = 10;
const CODE_LINES = 14;

const SUB_STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
  archived: "Deleted",
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

// fold: litere mici, fara diacritice (cautarea pe board: "sterge" gaseste "Șterge")
const fold = (s) => String(s || "").normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase();
// Cati agenti se vad langa filtrele board-ului; peste, ultimul loc e "+N" cu lista restului.
const TEAM_SHOWN = 5;

// savedDrawer / saveDrawer: panoul din dreapta ramane deschis sau inchis intre pagini.
// Pe ecrane inguste panoul acopera board-ul, deci acolo pornim mereu cu el inchis.
function savedDrawer() {
  if (window.innerWidth <= 1100) return false;
  try { return localStorage.getItem("hobstack-drawer") === "1"; } catch { return false; }
}
function saveDrawer(on) {
  try { localStorage.setItem("hobstack-drawer", on ? "1" : "0"); } catch {}
}

// Tab-urile paginii de proiect; primul e cel implicit.
const TABS = [
  { id: "tasks", label: "Tasks" },
  { id: "team", label: "Team" },
  { id: "files", label: "Files" },
  { id: "brainstorm", label: "Brainstorm" },
  { id: "stats", label: "Stats" },
];

// parseWorkspaceRoute: restul hash-ului dupa #/p/3 -> { tab, agentId, taskId, recurringId, topicId }.
// #/p/3/brainstorm/4 deschide subiectul 4 din Brainstorm.
// Link-urile vechi #/p/3/agent/7 duc in tab-ul Echipa, cu agentul 7 selectat.
// #/p/3/tasks/12 deschide modala task-ului 12 peste board.
export function parseWorkspaceRoute(rest) {
  const parts = (rest || "").split("/").filter(Boolean);
  // adresele vechi (in romana) duc in tab-urile noi
  const legacy = { "task-uri": "tasks", "echipa": "team", "fisiere": "files" };
  if (parts[0] in legacy) parts[0] = legacy[parts[0]];
  if (parts[0] === "agent") {
    return { tab: "team", agentId: /^\d+$/.test(parts[1] || "") ? Number(parts[1]) : null, taskId: null, recurringId: null, topicId: null };
  }
  const tab = TABS.some((t) => t.id === parts[0]) ? parts[0] : TABS[0].id;
  const num = /^\d+$/.test(parts[1] || "") ? Number(parts[1]) : null;
  const agentId = tab === "team" ? num : null;
  const taskId = tab === "tasks" ? num : null;
  // #/p/3/tasks/recurring/5 deschide Run history al sarcinii recurente 5 peste board
  const recurringId = tab === "tasks" && parts[1] === "recurring" && /^\d+$/.test(parts[2] || "") ? Number(parts[2]) : null;
  const topicId = tab === "brainstorm" ? num : null;
  return { tab, agentId, taskId, recurringId, topicId };
}

export async function workspaceView(root, projectId, route) {
  const state = {
    project: await api(`/api/projects/${projectId}`),
    tab: route.tab,
    panel: route.tab === "team" ? "details" : "chat", // panoul din dreapta: "details" sau "chat"
    selected: null,   // agentul selectat (chat si detalii)
    seen: new Set(),  // id-urile mesajelor afisate in thread
    delivered: new Set(), // mesajele vazute deja livrate (un raspuns vechi nu le mai pune "queued")
    agentFilter: null,  // doar task-urile unui agent; null = toti
    liveRunId: null,
    liveStream: null,
    pending: [],      // fisiere alese pentru urmatorul mesaj
    doneRunId: null,  // ultimul run terminat: activeRunId poate ramane vechi pana la reload
    // panoul din dreapta (chat / detalii) e deschis; pe Echipa cu un agent in adresa se deschide singur
    drawer: route.tab === "team" && !!route.agentId || savedDrawer(),
    doneShown: DONE_PAGE, // cate carduri arata coloana Done (restul sub "Show N more")
    query: "",            // cautarea pe board (titlu sau T-N)
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
  let runHistory = null; // modala Run history deschisa (adresa #/p/<id>/tasks/recurring/<id>); nu "history", ar ascunde window.history

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
  const mcpReqList = el("div", { class: "needs-mcp-requests" });
  let mcpReqKey = null;
  const workReqList = el("div", { class: "needs-work-requests" });
  const libraryList = el("div", { class: "needs-library" });
  const accountList = el("div", { class: "needs-account" });
  const needsBox = el("section", { class: "needs-you", "aria-live": "polite", hidden: true },
    needsTitle, accountList, questionsBox, fillList, stuckList, accessList, agentReqList, mcpReqList, workReqList, libraryList);
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
    // clic pe un agent: sertarul se deschide cu chat-ul lui (dublu-clic = detaliile)
    onSelect: (id) => { setDrawer(true); select(id, "chat"); },
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
  const folderChips = el("div", { class: "folder-chips", role: "group", "aria-label": "Filter tasks" });
  // cautarea dupa titlu (sau numar: T-215 / 215); campul ramane acelasi intre redesenari, ca sa nu piarda focusul
  const taskSearch = el("input", { type: "search", class: "task-search", placeholder: "Search tasks", "aria-label": "Search tasks by title" });
  taskSearch.addEventListener("input", () => { state.query = taskSearch.value; renderBoard(); });
  taskSearch.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && taskSearch.value) { e.stopPropagation(); taskSearch.value = ""; state.query = ""; renderBoard(); }
  });
  // avatarele echipei, langa filtre: click = chat cu agentul; peste TEAM_SHOWN agenti, ultimul e "+N" cu restul in lista
  const teamBox = el("div", { class: "board-team", role: "group", "aria-label": "Team" });
  let teamKey = null; // ce s-a desenat ultima data (lista ramane deschisa intre refresh-uri)
  let teamOutside = null; // ascultatorul "clic in afara" al listei "+N" deschise
  const subsOpen = new Set(); // grupurile desfasurate pe board (id-ul radacinii); raman la refresh
  let boardPanel = null; // "stages" | "task" | "recurring" | "deleted" | null
  let dragging = null; // task-ul tras acum pe board (null = niciunul)
  let explorer = null; // explorer-ul de cod deschis (Cmd/Ctrl+P), null = inchis
  let dragFrom = null, dragOver = null; // coloana de unde a plecat cardul si cea peste care e acum
  let cardMenu = null; // meniul "⋯" deschis ({ btn, close }); unul singur odata
  let boardStale = false; // un refresh venit cat tragi / cat e meniul deschis: se deseneaza dupa
  let recurring = null; // panoul Recurring cand e deschis ({ el, update, edit })
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
  // stickBottom: utilizatorul e la capatul chat-ului (sub 120px); daca a urcat sa citeasca, mesajele noi nu-l mai coboara
  let stickBottom = true;
  thread.addEventListener("scroll", () => {
    stickBottom = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
  });
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
  onPasteFiles(input, addPending);
  // campul creste cu textul (pana la max-height din CSS, apoi scroll); dupa trimitere revine la un rand
  const grow = () => {
    input.style.height = "auto";
    input.style.height = input.scrollHeight + "px";
  };
  input.addEventListener("input", grow);
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
  // caile din chat devin link (Show in Finder); raspunsurile serverului se tin minte per chat
  const paths = pathLinker(projectId, () => state.selected);
  const quick = el("div", { class: "quick-actions" }); // Ask for status, Give a task, Open T-<nr>
  const chatBox = el("div", { class: "chat" }, threadHint, thread, chatShare, chatQuestion, el("div", { class: "composer" }, chips, composer, quick, note));
  // @ in mesaj: fisierele de cod ale proiectului (caile relative la folderul agentului selectat)
  attachMentions(input, chatBox.querySelector(".composer"), async (q) => {
    const agent = state.selected ? `&agent=${state.selected}` : "";
    const res = await api(`/api/projects/${projectId}/code-files?q=${encodeURIComponent(q)}${agent}`);
    return res.files;
  });
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
    agentRole: (id) => state.project.agents.find((a) => a.id === id)?.role || "",
  });
  // tab-ul Brainstorm: lista de subiecte se incarca de la inceput (insigna cu intrebarile deschise)
  let bsBadge = 0;
  const brainstorm = brainstormView(projectId, {
    agents: () => state.project.agents,
    onBadge: (n) => { if (n !== bsBadge) { bsBadge = n; renderTabs(); } },
  });
  const bodies = {
    "team": el("div", { class: "tab-body team-body" }, canvas.el),
    "tasks": el("section", { class: "tab-body tasks-body" },
      el("div", { class: "board-toolbar" }, taskSearch, folderChips, teamBox, pausedNote, el("span", { class: "grow" }), boardTools, count),
      editorSlot,
      el("div", { class: "board-row" }, needsBox, board),
      boardHint),
    "files": el("div", { class: "tab-body files-body" }, filesSection),
    "brainstorm": brainstorm.el,
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

  // toggleExplorer: deschide / inchide explorer-ul de cod (Cmd/Ctrl+P, butonul Code)
  function toggleExplorer() {
    if (explorer) { explorer.close(); return; }
    const rootId = state.project.agents.find((a) => a.isRoot && !a.detached)?.id ?? firstRootId();
    explorer = openCodeExplorer({
      projectId, project: state.project,
      // agentul din panou, daca poate primi mesaje (un agent nelegat nu lucreaza): altfel radacina
      defaultAgentId: state.drawer && state.project.agents.some((a) => a.id === state.selected && !a.detached) ? state.selected : rootId,
      onAsk: prefillChat,
      onClose: () => { explorer = null; },
    });
  }

  // prefillChat: chat-ul agentului deschis, cu text la inceputul campului (textul scris ramane dupa el)
  async function prefillChat(agentId, text) {
    if (!(state.drawer && state.selected === agentId && state.panel === "chat")) await openAgent(agentId, "chat");
    input.value = text + input.value.replace(/^\s+/, "");
    input.setSelectionRange(text.length, text.length);
    input.dispatchEvent(new Event("input", { bubbles: true })); // inaltimea campului, ciorna
    input.focus();
  }

  // Cmd/Ctrl+P pe paginile proiectului: explorer-ul de cod in locul dialogului de print
  const onShortcut = (e) => {
    if (modKey(e) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "p") {
      e.preventDefault();
      toggleExplorer();
    }
  };
  document.addEventListener("keydown", onShortcut);

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
    const mcpReqs = state.project.mcpRequests || [];
    const total = mcpReqs.length + proposals.length + questions.length + fills.length + stuck.length + access.length + agentReqs.length + workReqs.length + (state.project.accountError ? 1 : 0);
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
    const mKey = mcpReqs.map((r) => r.id).join(",");
    if (mKey !== mcpReqKey) {
      mcpReqKey = mKey;
      mcpReqList.replaceChildren(...mcpReqs.map((r) => mcpRequestBox(r, { agent: state.project.agents.find((a) => a.id === r.agentId), onDone: reload })));
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
    }, t.label, t.id === "brainstorm" && bsBadge
      ? el("span", { class: "tab-badge", title: `${bsBadge} open ${bsBadge === 1 ? "question" : "questions"}` }, bsBadge) : null)));
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
      tools: ["write", "shell"], responsibilities: "", parentId, reviewerId: null, detached,
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
    if (!a || !confirm(`Delete agent ${a.name}? Its finished tasks stay on the board, without an assignee.`)) return;
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
    syncHistory(r.recurringId);
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
    if (r.tab === "brainstorm") brainstorm.open(r.topicId);
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
    const root = state.project.agents.find((a) => a.id === firstRootId());
    const chatOpen = state.drawer && state.panel === "chat" && state.selected === root?.id;
    header.replaceChildren(topbar(
      [["Projects", "#/"], [p.name, null]],
      el("span", { class: "topbar-status" }, status,
        p.autonomy ? el("span", { class: "status-chip auto", title: "Agents don't ask you questions; the root lead decides" }, "Autonomous") : null,
        p.accountName ? el("span", { class: "status-chip account", title: "Claude account used by this project's agents" }, p.accountName) : null,
        p.category ? el("a", { class: "status-chip cat", href: "#/", title: `Projects in ${p.category}`,
          onclick: () => saveCategory(p.category) }, p.category) : null,
        needsChip),
      el("span", { class: "grow" }),
      el("button", { type: "button", class: "btn ghost", title: `Code explorer (${shortcutLabel})`, onclick: toggleExplorer }, "Code"),
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

  // renderTeamFaces: avatarele echipei langa filtre. Pana la TEAM_SHOWN se vad toti; peste, primii
  // TEAM_SHOWN-1 si "+N", care deschide lista cu restul. Se redeseneaza doar cand se schimba echipa.
  function renderTeamFaces(agents) {
    const working = (a) => (a.status || "").toLowerCase() === "working";
    const key = JSON.stringify(agents.map((a) => [a.id, a.name, kindOf(a), working(a)]));
    if (key === teamKey) return;
    teamKey = key;
    const shown = agents.length > TEAM_SHOWN ? agents.slice(0, TEAM_SHOWN - 1) : agents;
    const rest = agents.slice(shown.length);
    const face = (a) => el("button", {
      type: "button", class: "team-face" + (working(a) ? " working" : ""), title: `${a.name}${working(a) ? " (working)" : ""}: chat`,
      "aria-label": `Chat with ${a.name}`, onclick: () => openAgent(a.id, "chat"),
    }, avatar(a.name, kindOf(a), 30));
    const wasOpen = !!teamBox.querySelector(".team-more[open]");
    if (teamOutside) { document.removeEventListener("click", teamOutside); teamOutside = null; }
    let more = null;
    if (rest.length) {
      more = el("details", { class: "menu team-more" },
        el("summary", { class: "avatar more", style: "--size:30px", title: `${rest.length} more`, "aria-label": `${rest.length} more agents` }, `+${rest.length}`),
        el("div", { class: "menu-panel filter-panel" },
          el("span", { class: "menu-label" }, "Team"),
          rest.map((a) => el("button", {
            type: "button", class: "filter-item", title: `Chat with ${a.name}`,
            onclick: () => { more.open = false; openAgent(a.id, "chat"); },
          }, avatar(a.name, kindOf(a), 22), el("span", { class: "grow" }, a.name),
            working(a) ? el("span", { class: "dot working", title: "Working" }) : null))));
    }
    if (more) {
      // clic in afara inchide lista; ascultatorul exista doar cat e deschisa
      const outside = (e) => { if (!more.contains(e.target)) more.open = false; };
      more.addEventListener("toggle", () => {
        if (more.open) { document.addEventListener("click", outside); teamOutside = outside; }
        else { document.removeEventListener("click", outside); if (teamOutside === outside) teamOutside = null; }
      });
    }
    teamBox.replaceChildren(...shown.map(face), ...(more ? [more] : []));
    if (more && wasOpen) more.open = true; // redesenata cat era deschisa (ex. un agent a inceput sa lucreze)
    teamBox.hidden = !agents.length;
  }

  // newTaskFromTop: "+ Task" din bara de sus (sau "+ Add a task" dintr-o coloana): board-ul cu formularul
  // deschis; stageId (din coloana unei etape) preselecteaza etapa la Assignee
  function newTaskFromTop(stageId = null) {
    if (typeof stageId !== "number") stageId = null; // din onclick vine evenimentul
    if (state.tab !== "tasks") {
      window.addEventListener("hashchange", () => { if (boardPanel !== "task") toggleTaskForm(stageId); }, { once: true });
      location.hash = `#/p/${projectId}/tasks`;
      return;
    }
    if (boardPanel !== "task") return toggleTaskForm(stageId);
    const who = editorSlot.querySelector(".task-form select");
    if (who && stageId) { who.value = "stage:" + stageId; who.dispatchEvent(new Event("change")); }
    editorSlot.querySelector("input")?.focus();
  }

  function buildOpenBar(editors) {
    let saved = null;
    try { saved = localStorage.getItem("hobstack-editor"); } catch {}
    const known = editors.some((e) => e.id === saved);
    const select = el("select", {
      class: "select-small", "aria-label": "Editor",
      value: known ? saved : editors[0]?.id,
      onchange: () => { try { localStorage.setItem("hobstack-editor", select.value); } catch {} },
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

  // renderBoard: coloanele standard, cu etapele proiectului (coloanele proprii) intre In progress si Review
  function renderBoard() {
    // cat tragi un card sau e deschis meniul lui, board-ul nu se redeseneaza (cardul ar disparea de sub mouse)
    if (dragging || cardMenu) { boardStale = true; return; }
    boardStale = false;
    const p = state.project;
    const byId = new Map(p.agents.map((a) => [a.id, a]));
    const stages = p.stages || [];
    if (!p.agents.some((a) => a.id === state.agentFilter)) state.agentFilter = null;
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
    folderChips.replaceChildren(filter); // doar Filter (agentul): All tasks / Mine / folderele au fost scoase
    renderTeamFaces(p.agents);
    pausedNote.hidden = !p.paused;
    pausedNote.replaceChildren(el("span", { class: "dot" }), "Paused: agents answer messages but don't pick up tasks. ",
      el("button", { type: "button", class: "link-btn", onclick: () => projectAction("resume") }, "Start team"));
    const q = fold(state.query.trim());
    const num = /^(?:t-?)?(\d+)$/.exec(q)?.[1];
    const matches = (t) => !q || fold(t.title).includes(q) || (num && String(t.number) === num);
    const passes = (t) => (!state.agentFilter || t.assigneeId === state.agentFilter) && matches(t);
    // doar in Done task-urile se grupeaza (arbore sau fir de lucru, din task-urile gata); in
    // celelalte coloane fiecare task are cardul lui, in coloana statusului sau
    const single = (t) => ({ root: t, subs: [], follows: [] });
    const groups = [
      ...p.tasks.filter((t) => t.status !== "done" && t.status !== "archived").map(single),
      ...doneGroups(taskGroups(p.tasks)),
    ].filter((g) => passes(g.root) || g.subs.some((s) => passes(s.task)));
    // in fiecare coloana, cele mai recente primele: ultima activitate din grup (radacina sau
    // orice alt membru); la egalitate, task-ul creat mai tarziu (numar mai mare)
    const lastActivity = (g) => Math.max(Date.parse(g.root.updatedAt) || 0,
      ...g.subs.map((s) => Date.parse(s.task.updatedAt) || 0));
    for (const g of groups) g.last = lastActivity(g);
    groups.sort((a, b) => b.last - a.last || b.root.number - a.root.number);
    // coloana unui task, ca cheie: un task care asteapta / lucreaza / e blocat intr-o etapa sta in coloana
    // etapei; restul dupa status (Review tine si task-urile trecute prin etape care asteapta decizia)
    const colKey = (t) => {
      if (t.stageId && t.status !== "review" && t.status !== "done" && stages.some((s) => s.id === t.stageId)) return "s" + t.stageId;
      return "c" + COLUMNS.findIndex((c) => c.statuses.includes(t.status));
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
    // Deleted (N): doar cand exista task-uri sterse (sau panoul e deschis, ca sa se poata inchide)
    const archived = p.archived || 0;
    boardTools.replaceChildren(...[
      archived > 0 || boardPanel === "deleted" ? toggle("deleted", archived ? `Deleted (${archived})` : "Deleted", toggleDeleted) : null,
      toggle("recurring", "\u21bb Recurring", toggleRecurring), toggle("stages", "Columns", toggleStages),
    ].filter(Boolean));
    board.className = "board" + (stages.length ? " stages-board" : "");
    // tintele de drop: Queued, etapele si Done; In progress si Review primesc task-uri doar prin munca agentilor
    const std = (i) => {
      const c = COLUMNS[i];
      const last = i === COLUMNS.length - 1;
      return column(c.title, inCol("c" + i), byId, null,
        { add: i === 0, page: last, target: i === 0 ? "queued" : last ? "done" : null });
    };
    board.replaceChildren(
      std(0), std(1),
      ...stages.map((s) => column(s.name, inCol("s" + s.id), byId, s, { add: true, target: "stage:" + s.id })),
      std(2), std(3));
    boardHint.hidden = stages.length > 0;
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
  // opts.target: ce inseamna sa lasi un card aici ("queued" | "done" | "stage:<id>"); lipsa = nu primeste carduri.
  function column(title, items, byId, stage, opts = {}) {
    const agents = stage ? stage.agentIds.map((id) => byId.get(id)).filter(Boolean) : [];
    const visible = opts.page && !state.query.trim() ? items.slice(0, state.doneShown) : items; // cu cautare: toate
    const hidden = items.length - visible.length;
    const target = opts.target || null;
    return el("div", {
      class: "col" + (items.length ? "" : " empty-col"),
      "data-target": target || "", "data-title": title,
      // preventDefault pe dragover = aici se poate lasa cardul
      ondragover: (e) => {
        if (!dragging) return;
        dragOver = e.currentTarget; // coloana de sub card (pentru motivul refuzului la dragend)
        if (moveAction(dragging, target).ok) e.preventDefault();
      },
      ondragleave: (e) => { if (!e.currentTarget.contains(e.relatedTarget) && dragOver === e.currentTarget) dragOver = null; },
      ondrop: async (e) => {
        e.preventDefault();
        const t = dragging;
        const inPlace = dragFrom === e.currentTarget; // lasat pe coloana de unde a plecat
        endDrag();
        if (t && await moveTask(t, target, askReason, { confirmUnblock: inPlace })) reload();
      },
    },
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
        opts.add ? el("button", { type: "button", class: "col-add", onclick: () => newTaskFromTop(stage?.id || null) }, "+ Add a task") : null));
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
    const card = cardWrap(g.root, taskCard(g.root, byId.get(g.root.assigneeId), byId, members));
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

  // toggleDeleted: panoul cu task-urile sterse (Restore)
  function toggleDeleted() {
    if (boardPanel === "deleted") return closePanel();
    openDeleted();
  }
  // openDeleted: (re)deschide panoul cu lista proaspata (si dupa un Delete / Undo facut in alta parte)
  function openDeleted() {
    showPanel("deleted", deletedPanel({ projectId, onClose: closePanel, onChanged: reload }).el);
  }

  // ---------- mutare, meniul cardului, stergere ----------

  // startDrag: coloanele arata unde se poate lasa cardul (drop-ok) si unde nu (drop-no, cu motivul)
  // Coloana de unde pleaca cardul (from) ramane neschimbata cand nu-l primeste (lasat acolo, nu se misca);
  // un task blocat in etapa lui o vede ca tinta (acolo = Unblock).
  function startDrag(t, from = null) {
    closeCardMenu();
    dragging = t;
    dragFrom = from;
    dragOver = null;
    for (const col of board.querySelectorAll(".col")) {
      const a = moveAction(t, col.dataset.target || null);
      if (col === from && !a.ok) continue;
      col.classList.add(a.ok ? "drop-ok" : "drop-no");
      col.title = a.ok ? `${a.label} T-${t.number}` : a.why;
    }
  }
  function endDrag() {
    if (!dragging) return;
    dragging = dragFrom = dragOver = null;
    for (const col of board.querySelectorAll(".col")) { col.classList.remove("drop-ok", "drop-no"); col.removeAttribute("title"); }
    if (boardStale) renderBoard();
  }

  // dropTargets: coloanele board-ului care primesc carduri ({ target, title }), in ordinea de pe ecran
  function dropTargets() {
    return [...board.querySelectorAll(".col")]
      .filter((c) => c.dataset.target)
      .map((c) => ({ target: c.dataset.target, title: c.dataset.title }));
  }

  // cardWrap: cardul plus butonul "⋯" (un buton nu poate sta in alt buton, deci stau alaturi)
  function cardWrap(t, card) {
    const btn = el("button", {
      type: "button", class: "card-menu-btn", "aria-label": "Task actions", title: "Task actions",
      "aria-haspopup": "menu", "aria-expanded": "false",
      onclick: (e) => { e.stopPropagation(); openCardMenu(t, btn); },
    }, "⋯");
    return el("div", { class: "card-wrap" }, card, btn);
  }

  // stopItem: "Stop" pe un task in lucru (sau in coada, inca nepornit), "Resume" pe unul oprit; altfel nimic.
  function stopItem(t, pick) {
    if (t.status === "queued" && t.stopped) {
      return el("button", { type: "button", role: "menuitem", onclick: pick(() => taskAction(t, "resume")) }, "Resume");
    }
    const working = (state.project.agents || []).some((a) => a.workingTaskId === t.id);
    if (working || t.status === "queued") {
      return el("button", { type: "button", role: "menuitem", onclick: pick(() => taskAction(t, "stop")) }, "Stop");
    }
    return null;
  }

  // openCardMenu: meniul cardului (Move to… / Delete); se inchide la Esc, click in afara, scroll si dupa o alegere.
  // Sta pe document.body cu position: fixed, ca sa nu fie taiat de board-ul care se deruleaza.
  function openCardMenu(t, btn) {
    const again = cardMenu?.btn === btn;
    closeCardMenu();
    if (again) return; // al doilea click pe acelasi ⋯ doar inchide
    const pick = (fn) => async () => { closeCardMenu(); await fn(); };
    const cols = dropTargets();
    const targets = cols.map((c) => ({ ...c, a: moveAction(t, c.target) })).filter((c) => c.a.ok);
    const sub = el("div", { class: "card-menu-sub", role: "group", "aria-label": "Move to", hidden: true },
      targets.length
        ? targets.map((c) => el("button", {
          type: "button", role: "menuitem",
          onclick: pick(async () => { if (await moveTask(t, c.target, askReason, { confirmUnblock: true })) reload(); }),
        }, `${c.a.label} → ${c.title}`))
        : el("span", { class: "card-menu-note" }, moveAction(t, cols[0]?.target || null).why));
    const moveBtn = el("button", {
      type: "button", role: "menuitem", "aria-expanded": "false",
      onclick: () => {
        sub.hidden = !sub.hidden;
        moveBtn.setAttribute("aria-expanded", String(!sub.hidden));
        place(); // lista deschisa poate sa nu mai incapa sub buton
        if (!sub.hidden) sub.querySelector("button")?.focus();
      },
    }, "Move to…");
    const menu = el("div", { class: "card-menu", role: "menu", "aria-label": `Actions for T-${t.number}` },
      stopItem(t, pick),
      moveBtn, sub,
      el("button", { type: "button", role: "menuitem", class: "danger", onclick: pick(() => deleteTask(t)) }, "Delete"));
    document.body.append(menu);
    // place: sub buton, aliniat la dreapta lui; daca nu incape dedesubt se deschide deasupra.
    // Ramane in ecran (8px margine); mai inalt decat ecranul -> se deruleaza in interior.
    function place() {
      const m = 8, gap = 4;
      const r = btn.getBoundingClientRect();
      const vh = window.innerHeight;
      menu.style.maxHeight = "";
      menu.style.overflowY = "";
      menu.style.top = "0px";
      const w = menu.offsetWidth;
      menu.style.left = `${Math.max(m, Math.min(r.right - w, window.innerWidth - w - m))}px`;
      let h = menu.offsetHeight;
      if (h > vh - 2 * m) {
        menu.style.maxHeight = `${vh - 2 * m}px`;
        menu.style.overflowY = "auto";
        h = vh - 2 * m;
      }
      let top = r.bottom + gap;
      if (top + h > vh - m) {
        // deasupra: marginea de jos a meniului la 4px deasupra butonului
        const above = r.top - gap - h;
        top = above >= m ? above : Math.max(m, vh - m - h);
      }
      menu.style.top = `${top}px`;
    }
    place();
    btn.setAttribute("aria-expanded", "true");
    const onDown = (e) => { if (!menu.contains(e.target) && e.target !== btn) closeCardMenu(); };
    const onKey = (e) => { if (e.key === "Escape") { e.preventDefault(); closeCardMenu(); btn.focus(); } };
    const onScroll = (e) => { if (!menu.contains(e.target)) closeCardMenu(); };
    document.addEventListener("mousedown", onDown, true);
    document.addEventListener("keydown", onKey);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", closeCardMenu);
    cardMenu = {
      btn,
      close() {
        document.removeEventListener("mousedown", onDown, true);
        document.removeEventListener("keydown", onKey);
        window.removeEventListener("scroll", onScroll, true);
        window.removeEventListener("resize", closeCardMenu);
        menu.remove();
        btn.setAttribute("aria-expanded", "false");
      },
    };
    moveBtn.focus();
  }
  function closeCardMenu() {
    if (!cardMenu) return;
    const m = cardMenu;
    cardMenu = null;
    m.close();
    if (boardStale) renderBoard();
  }

  // askReason: fereastra pentru Send back / Reopen; intoarce textul (nevid) sau null la Cancel.
  // opts.optional (confirmarea la Unblock): nota poate lipsi, atunci intoarce "".
  function askReason(label, t, opts = {}) {
    return new Promise((resolve) => {
      const text = el("textarea", {
        rows: 4, "aria-label": opts.optional ? "Note (optional)" : "Reason",
        placeholder: opts.optional ? "Optional: anything the agent should know before it resumes." : "What should change? The agent reads this.",
      });
      const ok = el("button", { type: "submit", class: "btn primary" }, label);
      const titleId = `reason-${t.id}`;
      const form = el("form", {
        class: "reason-form",
        onsubmit: (e) => {
          e.preventDefault();
          const v = text.value.trim();
          if (!v && !opts.optional) { text.focus(); return; }
          done(v);
        },
      },
        el("h2", { id: titleId }, `${label} T-${t.number}`),
        el("p", { class: "hint" }, t.title),
        opts.optional ? el("p", { class: "hint" }, "The task goes back to work. A note is optional; it becomes the agent's feedback.") : null,
        text,
        el("div", { class: "actions" }, el("span", { class: "grow" }),
          el("button", { type: "button", class: "btn ghost", onclick: () => done(null) }, "Cancel"), ok));
      const dialog = el("div", { class: "modal reason-modal", role: "dialog", "aria-modal": "true", "aria-labelledby": titleId }, form);
      const overlay = el("div", { class: "modal-overlay" }, dialog);
      let downOnOverlay = false;
      overlay.addEventListener("mousedown", (e) => { downOnOverlay = e.target === overlay; });
      overlay.addEventListener("click", (e) => { if (downOnOverlay && e.target === overlay) done(null); });
      const onKey = (e) => { if (e.key === "Escape") { e.preventDefault(); done(null); } };
      document.addEventListener("keydown", onKey);
      document.body.append(overlay);
      text.focus();
      function done(v) {
        document.removeEventListener("keydown", onKey);
        overlay.remove();
        resolve(v);
      }
    });
  }

  // deleteTask: sterge (arhiveaza) task-ul cu sub-task-urile lui, dupa confirmare; toast cu Undo.
  // Folosit de meniul cardului si de modala (actions.deleteTask). Intoarce true daca s-a sters.
  async function deleteTask(t) {
    const tasks = state.project.tasks || [];
    // sub-task-urile din grup, la orice adancime (serverul arhiveaza tot arborele)
    const subs = new Set();
    for (let grew = true; grew;) {
      grew = false;
      for (const x of tasks) {
        if (x.id !== t.id && !subs.has(x.id) && (x.parentTaskId === t.id || subs.has(x.parentTaskId))) { subs.add(x.id); grew = true; }
      }
    }
    const ids = new Set([t.id, ...subs]);
    const busy = (state.project.agents || []).find((a) => ids.has(a.workingTaskId));
    const n = subs.size;
    let msg = `Delete T-${t.number}` + (n ? ` and its ${n} ${n === 1 ? "subtask" : "subtasks"}` : "") +
      `? You can restore ${n ? "them" : "it"} from Deleted.`;
    if (busy) msg += `\n${busy.name} is working on it; the run will be stopped.`;
    if (!confirm(msg)) return false;
    try {
      await api(`/api/tasks/${t.id}`, { method: "DELETE" });
    } catch (err) { toast(err.message); return false; }
    toast(`Deleted T-${t.number}`, "ok", { label: "Undo", onClick: async () => {
      try {
        await api(`/api/tasks/${t.id}/restore`, { method: "POST" });
        await reload();
        if (boardPanel === "deleted") openDeleted();
      } catch (err) { toast(err.message); }
    } });
    await reload();
    if (boardPanel === "deleted") openDeleted();
    return true;
  }

  // agentii care pot lua un task: legati; intr-o etapa, doar cei din etapa
  function assignable(stageId) {
    const p = state.project;
    const stage = stageId && (p.stages || []).find((s) => s.id === stageId);
    return p.agents.filter((a) => !a.detached && (!stage || stage.agentIds.includes(a.id)));
  }

  // toggleTaskForm: formularul New task. Assignee: un agent (drumul standard) sau o etapa ("stage:<id>",
  // o ia primul agent liber din etapa); stageId preselecteaza etapa.
  function toggleTaskForm(stageId = null) {
    if (boardPanel === "task") return closePanel();
    const p = state.project;
    const stages = p.stages || [];
    const title = el("input", { type: "text", maxlength: 200, placeholder: "e.g. Login page", required: true });
    const description = el("textarea", { rows: 3, placeholder: "What needs to be done, acceptance criteria (optional)" });
    const agentOptions = assignable(null).map((a) => el("option", { value: a.id }, a.name + (a.role ? " \u00b7 " + a.role : "")));
    const who = el("select", { onchange: () => { whoHint.textContent = assignHint(); } },
      stages.length ? el("optgroup", { label: "Agent" }, agentOptions) : agentOptions,
      stages.length ? el("optgroup", { label: "Column (first free agent)" },
        stages.map((s) => el("option", { value: "stage:" + s.id }, s.name + (s.agentIds.length ? "" : " \u00b7 no agents")))) : null);
    if (stageId && stages.some((s) => s.id === stageId)) who.value = "stage:" + stageId;
    const pickedStage = () => (who.value.startsWith("stage:") ? Number(who.value.slice(6)) : 0);
    const end = p.autonomy ? "the root lead decides at the end (full autonomy)." : "you decide at the end.";
    const assignHint = () => {
      const st = stages.find((s) => s.id === pickedStage());
      return (st ? `The task enters ${st.name} and moves through the next columns; ` : "The agent picks it up automatically; ") + end;
    };
    const whoHint = el("span", {}, assignHint());
    // fisierele pentru agent: alese cu butonul sau trase pe formular, trimise multipart la Add
    const files = [];
    const fileList = el("ul", { class: "tf-files" });
    const picker = el("input", { type: "file", multiple: true, hidden: true, class: "tf-picker",
      onchange: () => { addFiles(picker.files); picker.value = ""; } });
    const attach = el("div", { class: "tf-attach" },
      el("button", { type: "button", id: "tf-attach-btn", class: "btn small", onclick: () => picker.click() }, "\u{1F4CE} Attach files"),
      el("span", { class: "hint" }, "or drop or paste them on this form. The agent reads them with the task."), picker);
    const addBtn = el("button", { type: "submit", class: "btn primary" }, "Add");
    const form = el("form", { class: "section task-form", onsubmit: submit },
      el("h2", {}, "New task"),
      field("Title", title), field("Description", description),
      el("div", { class: "field" }, el("label", { for: "tf-attach-btn" }, "Files"), attach, fileList),
      field("Assignee", who, whoHint),
      el("div", { class: "actions" }, el("span", { class: "grow" }),
        el("button", { type: "button", class: "btn", onclick: closePanel }, "Cancel"), addBtn));
    setupDrop(form, addFiles);
    onPasteFiles(form, addFiles);
    showPanel("task", form);
    title.focus();

    function addFiles(list) {
      for (const f of Array.from(list)) {
        if (!files.some((x) => x.name === f.name && x.size === f.size)) files.push(f);
      }
      drawFiles();
    }
    function drawFiles() {
      fileList.replaceChildren(...files.map((f, i) => el("li", { class: "tf-file" },
        fileThumb(f),
        el("span", { class: "tf-file-name", title: f.name }, f.name),
        el("span", { class: "hint" }, fileSize(f.size)),
        el("button", { type: "button", class: "tf-remove", "aria-label": `Remove ${f.name}`, title: "Remove",
          onclick: () => { releaseThumb(f); files.splice(i, 1); drawFiles(); } }, "\u00d7"))));
    }

    async function submit(e) {
      e.preventDefault();
      if (!title.value.trim()) return;
      addBtn.disabled = true;
      const path = `/api/projects/${projectId}/tasks`;
      try {
        if (files.length) {
          const fd = new FormData();
          fd.append("title", title.value.trim());
          fd.append("description", description.value.trim());
          fd.append("assigneeId", String(Number(who.value) || 0));
          fd.append("stageId", String(pickedStage()));
          for (const f of files) fd.append("files", f);
          await upload(path, fd);
        } else {
          await api(path, { method: "POST", body: {
            title: title.value.trim(), description: description.value.trim(), assigneeId: Number(who.value) || 0,
            stageId: pickedStage(),
          } });
        }
        closePanel();
        files.forEach(releaseThumb);
        await reload();
      } catch (err) {
        toast(err.message);
        addBtn.disabled = false;
      }
    }
  }

  function fileSize(n) {
    if (n < 1024) return `${n} B`;
    if (n < 1 << 20) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / (1 << 20)).toFixed(1)} MB`;
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
  // answers: raspunsurile la intrebarile task-ului ([{ id, choices, other }], din decide.js); Send back
  // cere text sau cel putin un raspuns.
  async function decide(t, action, feedback = "", answers = []) {
    if (action === "send_back" && !feedback.trim() && !answers.length) {
      toast(`Write what needs to change in ${ref(t)}.`);
      return false;
    }
    try {
      await api(`/api/tasks/${t.id}/decide`, { method: "POST", body: { action, feedback, answers } });
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
    else if (t.status === "queued" && t.stopped) line = el("span", { class: "task-line warn", title: "You stopped it. Resume it from ⋯ or drag it to Queued." }, "Stopped by you");
    else if (working) line = el("span", { class: "task-line working" }, el("span", { class: "dot working" }), `${worker.name} \u00b7 working`);
    else if ((state.project.fillIns || []).some((f) => f.taskId === t.id)) line = el("span", { class: "task-line warn" }, "Waiting on you");
    // spune ce are de facut: Accept / Send back sunt in modala (cu intrebarile din Decide, daca sunt)
    else if (decides) line = el("span", { class: "task-line warn", title: "Open the task to read the report and decide" }, "Your call: accept or send back");
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
      draggable: "true",
      ondragstart: (e) => {
        e.dataTransfer.effectAllowed = "move";
        // tip propriu (nu text/plain): lasat pe chat sau pe un camp nu lipeste id-ul; board-ul citeste doar `dragging`
        e.dataTransfer.setData("application/x-hobstack-task", String(t.id));
        startDrag(t, e.currentTarget.closest(".col"));
      },
      // dragend fara drop (drop-ul face endDrag inainte): lasat pe o coloana care nu-l primeste -> motivul
      ondragend: () => {
        const why = dragging && dragOver && dragOver !== dragFrom ? moveAction(dragging, dragOver.dataset.target || null).why : null;
        endDrag();
        if (why) toast(why);
      },
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
          taskAction, decide, assign, assignable, deleteTask,
          // "Watch live": inchide modala si deschide doar chat-ul agentului, pe pagina curenta (nu pe Team)
          watchAgent: (agentId) => {
            window.addEventListener("hashchange", () => {
              state.panel = "chat";
              setDrawer(true);
              select(agentId, "chat");
              renderHeader();
            }, { once: true });
            location.hash = `#/p/${projectId}/tasks`;
          },
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

  // syncHistory: deschide / inchide Run history dupa recurringId din adresa
  function syncHistory(recurringId) {
    if (runHistory && runHistory.recurringId === recurringId) return;
    runHistory?.destroy();
    runHistory = null;
    if (!recurringId || gone()) return;
    runHistory = recurringHistory({
      projectId, recurringId,
      onClose: () => { location.hash = `#/p/${projectId}/tasks`; },
      // Edit: inchide modala (prin adresa), apoi deschide panoul Recurring cu formularul sarcinii
      onEdit: (rec) => {
        window.addEventListener("hashchange", () => {
          if (boardPanel !== "recurring") toggleRecurring();
          recurring?.edit(rec);
        }, { once: true });
        location.hash = `#/p/${projectId}/tasks`;
      },
    });
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
      clearPending();
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
    paths.reset(); // alt agent sau chat reincarcat: fisierele se pot fi schimbat
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
    // evenimentul "livrat" poate veni pe SSE inaintea raspunsului la POST (agent liber): livrarea nu se pierde
    if (m.delivered) state.delivered.add(m.id);
    else if (state.delivered.has(m.id)) m = { ...m, delivered: true };
    if (state.seen.has(m.id)) {
      // acelasi mesaj, stare noua (ex. livrat): il redesenam pe loc
      const fresh = messageNode(m);
      if (m.kind !== "event") paths.link(fresh);
      thread.querySelector(`[data-id="${m.id}"]`)?.replaceWith(fresh);
      return;
    }
    state.seen.add(m.id);
    thread.querySelector(".empty")?.remove();
    const node = messageNode(m);
    if (m.kind !== "event") paths.link(node); // caile din mesaj: clic = Show in Finder
    const live = thread.querySelector(".live");
    if (live) thread.insertBefore(node, live); // blocul live ramane ultimul
    else thread.append(node);
    scrollDown();
  }

  // eventNode: notificarile echipei (aprobari, rapoarte, trimiteri inapoi) intra in chat doar ca un rand scurt;
  // raportul intreg e pe task (Conversation). T-N din rand deschide task-ul.
  function eventNode(m) {
    const text = m.text || "";
    let head = text;
    const long = text.length > 160 || text.includes("\n");
    if (long) {
      const i = text.indexOf(": ");
      head = i > 0 && i <= 200 ? text.slice(0, i) : text.split("\n")[0].slice(0, 140).trimEnd() + "…";
    }
    const parts = [];
    let last = 0;
    for (const mt of head.matchAll(/\bT-(\d+)\b/g)) {
      parts.push(head.slice(last, mt.index));
      const t = (state.project.tasks || []).find((x) => x.number === Number(mt[1]));
      parts.push(t ? el("a", { class: "event-task", href: `#/p/${projectId}/tasks/${t.id}` }, mt[0]) : mt[0]);
      last = mt.index + mt[0].length;
    }
    parts.push(head.slice(last));
    return el("div", { class: "event", "data-id": m.id, title: long ? "The full text is on the task" : null }, parts);
  }

  function messageNode(m) {
    if (m.kind === "event") return eventNode(m);
    const mine = m.author === "user";
    return el("div", { class: "msg" + (mine ? " me" : ""), "data-id": m.id },
      el("span", { class: "msg-meta" }, `${mine ? "You" : m.author} · ${clock(m.createdAt)}`),
      m.text ? messageBody(m.text, mine) : null,
      m.attachments && m.attachments.length > 0 && el("div", { class: "attachments" }, m.attachments.map(attachmentNode)),
      mine && !m.delivered && el("span", { class: "pending" }, "queued"));
  }

  // messageBody: mesajele agentilor vin in markdown; ale utilizatorului raman text simplu (doar ``` devine cod).
  // Un mesaj al utilizatorului lung (cod lipit) si blocurile de cod lungi ale agentilor se arata restranse.
  function messageBody(text, mine) {
    const lines = text.split("\n").length;
    if (mine) {
      const bubble = el("div", { class: "bubble" }, userText(text));
      const long = lines > USER_LINES + 2 || text.length > 1200;
      return long ? collapsible(bubble, USER_LINES, lines) : bubble;
    }
    const bubble = el("div", { class: "bubble md-bubble" }, renderMarkdown(text));
    for (const pre of bubble.querySelectorAll(".md-pre")) {
      const n = pre.textContent.split("\n").length;
      if (n > CODE_LINES + 4) {
        const at = pre.nextSibling, parent = pre.parentNode;
        parent.insertBefore(collapsible(pre, CODE_LINES, n), at);
      }
    }
    return bubble;
  }

  // scrollDown: coboara la capat daca utilizatorul era jos inainte de mesajul nou (stickBottom, din ultimul scroll).
  // Masurat dupa adaugare, un raspuns lung ar parea "departe de jos" si chat-ul n-ar mai cobori.
  function scrollDown(force) {
    if (force || stickBottom) {
      thread.scrollTop = thread.scrollHeight;
      stickBottom = true;
    }
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
        grow();
        clearPending();
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
    state.pending.push(...Array.from(fileList));
    renderChips();
  }

  function clearPending() {
    state.pending.forEach(releaseThumb);
    state.pending = [];
    renderChips();
  }

  function renderChips() {
    chips.replaceChildren(...state.pending.map((f, i) =>
      el("span", { class: "chip" }, fileThumb(f), f.name,
        el("button", {
          type: "button", "aria-label": `Remove ${f.name}`,
          onclick: () => { releaseThumb(f); state.pending.splice(i, 1); renderChips(); },
        }, "\u00d7"))));
  }

  // attachmentNode: o imagine (miniatura) sau un link, plus Open / Show in Finder (fisierul e pe masina asta)
  function attachmentNode(a) {
    const url = `/api/files/${a.id}`;
    // o poza se deschide intr-o modala (cu sagetile intre pozele din chat), nu in alt tab
    const item = a.mime && a.mime.startsWith("image/")
      ? el("a", { href: url, title: a.name, onclick: (e) => { e.preventDefault(); showImage(a.id); } },
        el("img", { src: url, alt: a.name, loading: "lazy", "data-file": a.id, "data-name": a.name }))
      : el("a", { class: "file-link", href: url, target: "_blank", rel: "noopener" }, a.name);
    return el("div", { class: "attachment" }, item,
      el("div", { class: "attachment-actions" }, ...openButtons(url, a.name, "link-btn")));
  }

  // showImage: modala cu pozele din chat-ul deschis, pornind de la cea apasata
  function showImage(id) {
    const list = [...thread.querySelectorAll("img[data-file]")].map((img) => ({
      src: `/api/files/${img.dataset.file}`, name: img.dataset.name, base: `/api/files/${img.dataset.file}`, id: img.dataset.file }));
    openImage(list, Math.max(0, list.findIndex((x) => x.id === String(id))));
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
    // Stop opreste doar run-ul acestui agent (echipa merge mai departe); pe un task, task-ul asteapta Resume
    const label = el("span", {}, `${a.name} is working now`);
    const stopBtn = el("button", {
      type: "button", class: "btn small live-stop", title: `Stop ${a.name} (the team keeps working)`,
      onclick: async () => {
        stopBtn.disabled = true;
        try {
          await api(`/api/agents/${a.id}/stop`, { method: "POST" });
        } catch (err) {
          stopBtn.disabled = false;
          toast(err.message);
        }
      },
    }, "Stop");
    const head = el("div", { class: "live-head" }, label, stopBtn);
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
      if (tool) label.textContent = `${a.name} is working \u00b7 ${toolLabel(tool)}`;
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
  syncHistory(route.recurringId);
  loadFiles();
  await loadThread();
  syncLive();

  // stream-ul proiectului se deschide abia acum: pana aici nu exista nimic de inchis
  syncHash();
  if (gone()) {
    stopLive();
    modal?.destroy();
    closeImage();
    runHistory?.destroy();
    brainstorm.destroy();
    window.removeEventListener("dragover", guardOver);
    window.removeEventListener("drop", guardDrop);
    window.removeEventListener("dragend", guardEnd);
    document.removeEventListener("pointerdown", closeMenus);
    document.removeEventListener("keydown", onShortcut);
    if (teamOutside) document.removeEventListener("click", teamOutside);
    explorer?.close();
    return () => {};
  }
  let hadError = false;
  const events = new EventSource(`/api/projects/${projectId}/events`);
  events.onmessage = (msg) => {
    let ev;
    try { ev = JSON.parse(msg.data); } catch { return; }
    if (ev.type === "message" && ev.data.agentId === state.selected) addMessage(ev.data);
    if (ev.type === "files") loadFiles();
    // Brainstorm: subiectul deschis se reincarca, la celelalte doar lista (insigna); nu atinge restul paginii
    if (ev.type === "brainstorm") { brainstorm.refresh(ev.data?.topicId || 0); return; }
    // Run history: doar la schimbarile de task (o rulare noua, un status, fisiere atasate), nu la fiecare mesaj
    if (ev.type === "task") runHistory?.refresh();
    scheduleReload();
  };
  events.onerror = () => { hadError = true; };
  events.onopen = async () => {
    // dupa o reconectare (ex. restart server) reincarcam tot, am putut pierde evenimente
    if (!hadError) return;
    hadError = false;
    await reload();
    loadFiles();
    brainstorm.refresh(0);
    await loadThread();
    syncLive();
  };


  const cleanup = () => {
    disposed = true;
    editor?.destroy();
    modal?.destroy();
    runHistory?.destroy();
    brainstorm.destroy();
    window.removeEventListener("dragover", guardOver);
    window.removeEventListener("drop", guardDrop);
    window.removeEventListener("dragend", guardEnd);
    events.close();
    document.removeEventListener("pointerdown", closeMenus);
    document.removeEventListener("keydown", onShortcut);
    if (teamOutside) document.removeEventListener("click", teamOutside);
    explorer?.close();
    canvas.destroy();
    stopLive();
    clearTimeout(reloadTimer);
    clearInterval(agesTimer);
  };
  if (state.tab === "stats") stats.load(); // pagina deschisa direct pe #/p/3/stats
  if (state.tab === "brainstorm") brainstorm.open(route.topicId); // sau direct pe #/p/3/brainstorm/4
  else brainstorm.refresh(0); // doar pentru insigna
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
