import { api } from "./api.js";
import { el, avatar, clock, kindOf } from "./ui.js";
import { changesView } from "./changes.js";
import { flowView } from "./flow.js";
import { renderMarkdown, changeStats, statBadge } from "./md.js";

// Etichetele starilor unui task (pill-ul din antetul modalei).
const STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
};

// Peste cate caractere o bula din conversatie se arata scurtata.
const LONG_TEXT = 600;

// taskModal: fereastra unui task deschisa peste board (adresa #/p/<id>/tasks/<taskId>).
// - project: proiectul curent (agenti, userDecides, userCanDecide, stuck); refresh(project) il inlocuieste.
// - onClose(): cerut la Esc / click pe fundal / x (apelantul schimba adresa si cheama destroy).
// - actions: functiile din workspace, ca sa nu dublam cererile:
//   taskAction(t, "accept"|"unblock"|"nudge"), decide(t, action, feedback), assign(t, agentId),
//   assignable(stageId) -> agenti, waitingOn(t) -> numele la care asteapta un task din review,
//   openRecurring() -> inchide modala si deschide panoul Recurring.
//   Actiunile intorc true daca au reusit.
// - initialTab: tab-ul de pornire (ex. "flow"); lipsa = alegere automata la primul raspuns.
// - flowOpen: Set cu pasii Flow desfasurati, preluat de la modala inlocuita (vezi flowView).
// Intoarce { el, taskId, refresh(project), tab(), flowOpen, destroy() }.
export function taskModal({ projectId, taskId, project, info = {}, onClose, actions, initialTab = null, flowOpen }) {
  let data = null;        // ultimul raspuns GET /api/tasks/{id}
  let seq = 0;            // numarul ultimei cereri: un raspuns vechi nu suprascrie unul nou
  let destroyed = false;
  let reportOpen = false; // raportul lung ramane desfasurat la refresh
  let descOpen = false;   // descrierea lunga ramane desfasurata la refresh
  let filesKey = null;    // ce s-a desenat ultima data in rezumatul fisierelor
  let actionsKey = null;  // ce s-a desenat ultima data in zona de actiuni
  let tab = initialTab;   // "conversation" | "history" | "changes" | "flow"; null = inca nealeasa (se alege la primul raspuns)
  let convKey = null;     // ce s-a desenat ultima data in conversatie
  let histKey = null;     // idem pentru istoric
  let chgCount = null;    // changesCount-ul pentru care s-au incarcat datele Changes (null = neincarcat)
  let chgSeq = 0;         // ca seq, pentru cererea Changes
  const changes = changesView();
  let flowSeq = 0;        // ca seq, pentru cererea Flow
  let flowKey = null;     // ultimul raspuns Flow desenat (serializat)
  const flow = flowView({ projectId, open: flowOpen });
  let stats = new Map();  // cale -> { added, removed, kind }: badge-urile din raport si conversatie
  let topKey = null;      // idem pentru descriere + feedback
  let reportKey = null;   // idem pentru raport
  const expanded = new Set(); // bulele cu "Show more" deschis (cheie: tip + at + from)
  // textarea de feedback se creeaza o singura data: textul scris ramane chiar
  // daca zona de actiuni se reconstruieste
  const feedback = el("textarea", { rows: 3, placeholder: "What needs to change? (needed for Send back)", "aria-label": "Feedback" });

  const titleId = `task-modal-title-${taskId}`;
  const closeBtn = el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: () => requestClose() }, "×");
  const head = el("div", { class: "modal-head-main" });
  // nodurile corpului raman pe loc intre refresh-uri (scroll-ul, tab-ul, "Show more" nu se pierd);
  // se rescrie doar continutul care s-a schimbat
  const topBox = el("div", { class: "modal-top" });
  const convPane = el("div", { class: "conv", id: `conv-${taskId}`, role: "tabpanel", hidden: true });
  const histPane = el("div", { id: `hist-${taskId}`, role: "tabpanel", hidden: true });
  const tabConv = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": convPane.id, onclick: () => setTab("conversation") }, "Conversation");
  const tabHist = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": histPane.id, onclick: () => setTab("history") }, "Activity");
  const chgPane = el("div", { id: `chg-${taskId}`, role: "tabpanel", hidden: true }, changes.el);
  const tabChg = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": chgPane.id, onclick: () => setTab("changes") }, "Changes");
  const flowPane = el("div", { id: `flow-${taskId}`, role: "tabpanel", hidden: true }, flow.el);
  const tabFlow = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": flowPane.id, onclick: () => setTab("flow") }, "Flow");
  const tabsSection = el("section", { class: "modal-section", hidden: true },
    el("div", { class: "modal-tabs", role: "tablist" }, tabHist, tabConv, tabChg, tabFlow), convPane, histPane, chgPane, flowPane);
  const reportBox = el("div", { class: "modal-top" });
  const filesBox = el("div", { class: "modal-top" }); // primele fisiere modificate, deasupra tab-urilor
  const body = el("div", { class: "modal-body" }, el("p", { class: "hint" }, "Loading..."));
  const actionsBox = el("div", { class: "modal-actions", hidden: true });
  const side = el("aside", { class: "modal-side", "aria-label": "Task details" });
  const copyBtn = el("button", { type: "button", class: "btn ghost small", onclick: copyLink }, "Copy link");
  const dialog = el("div", { class: "modal", role: "dialog", "aria-modal": "true", "aria-labelledby": titleId, tabindex: "-1" },
    el("div", { class: "modal-main" },
      el("div", { class: "modal-head" }, head),
      body, actionsBox),
    side);
  const overlay = el("div", { class: "modal-overlay" }, dialog);
  // click pe fundal (nu in fereastra) inchide; mousedown ca o selectie de text
  // inceputa in fereastra si terminata pe fundal sa nu inchida
  let downOnOverlay = false;
  overlay.addEventListener("mousedown", (e) => { downOnOverlay = e.target === overlay; });
  overlay.addEventListener("click", (e) => { if (downOnOverlay && e.target === overlay) requestClose(); });

  const onKey = (e) => {
    if (e.key === "Escape") { e.preventDefault(); requestClose(); return; }
    if (e.key === "Tab") trapFocus(e);
  };
  document.addEventListener("keydown", onKey);
  const prevOverflow = document.body.style.overflow;
  document.body.style.overflow = "hidden"; // pagina din spate nu se mai deruleaza
  document.body.append(overlay);
  dialog.focus();
  load();

  // copyLink: adresa task-ului in clipboard (deschide modala direct)
  async function copyLink() {
    const url = `${location.origin}${location.pathname}#/p/${projectId}/tasks/${taskId}`;
    try {
      await navigator.clipboard.writeText(url);
      copyBtn.textContent = "Copied";
    } catch {
      copyBtn.textContent = "Copy failed";
    }
    setTimeout(() => { if (!destroyed) copyBtn.textContent = "Copy link"; }, 1500);
  }

  // requestClose: feedback-ul netrimis nu se pierde in tacere
  function requestClose() {
    if (feedback.value.trim() && !confirm("Discard your feedback?")) return;
    onClose();
  }

  // whenText: "HH:MM" pentru azi, altfel data scurta + ora ("Sep 30 14:02")
  function whenText(iso) {
    const d = new Date(iso);
    if (d.toDateString() === new Date().toDateString()) return clock(iso);
    return `${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })} ${clock(iso)}`;
  }

  // trapFocus: Tab ramane in fereastra (aria-modal)
  function trapFocus(e) {
    const items = Array.from(dialog.querySelectorAll("button, select, textarea, input, a[href], [tabindex]:not([tabindex='-1'])"))
      .filter((n) => !n.disabled && n.offsetParent !== null);
    if (!items.length) return;
    const first = items[0], last = items[items.length - 1];
    if (!dialog.contains(document.activeElement) || document.activeElement === dialog) {
      e.preventDefault();
      (e.shiftKey ? last : first).focus();
    } else if (e.shiftKey && document.activeElement === first) {
      e.preventDefault(); last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault(); first.focus();
    }
  }

  async function load() {
    const my = ++seq;
    let res;
    try {
      res = await api(`/api/tasks/${taskId}`);
    } catch (err) {
      if (destroyed || my !== seq) return;
      // la un refresh esuat (ex. server repornit) pastram ce era desenat
      if (!data) showMissing(err.message);
      return;
    }
    if (destroyed || my !== seq) return;
    if (res.task.projectId !== projectId) { showMissing("Not found"); return; }
    data = res;
    render();
  }

  function showMissing(msg) {
    head.replaceChildren(el("div", { class: "modal-crumbs" }, el("span", { class: "grow" }), closeBtn), el("h2", { id: titleId, class: "modal-title" }, "Task not found"));
    side.replaceChildren();
    body.replaceChildren(el("p", { class: "hint" }, msg === "Not found" || /not found/i.test(msg) ? "This task does not exist (anymore)." : msg));
    actionsBox.hidden = true;
    actionsBox.replaceChildren();
    actionsKey = null;
    topKey = histKey = convKey = reportKey = flowKey = filesKey = null;
    chgCount = null; // la urmatorul succes se reincarca tot
  }

  function render() {
    const t = data.task;
    const agent = project.agents.find((a) => a.id === t.assigneeId);
    // etapa fara agent liber: asignatul e doar un loc ocupat (ca pe cardul din board)
    const queuedInStage = !!t.stageId && t.status === "queued";
    const who = queuedInStage ? "" : (data.assignee || agent?.name || "");
    const waiting = t.status === "review" ? actions.waitingOn(t) : null;
    const stuck = (project.stuck || []).some((s) => s.taskId === t.id);
    head.replaceChildren(
      el("div", { class: "modal-crumbs" },
        el("span", {}, project.name || "Project"), el("span", { class: "crumb-sep", "aria-hidden": "true" }, "/"),
        el("span", { class: "task-id" }, `T-${t.number}`),
        el("span", { class: "grow" }), copyBtn, closeBtn),
      el("h2", { id: titleId, class: "modal-title" }, t.title));
    renderSide(t, agent, who, waiting, stuck, queuedInStage);

    // prima data scoatem "Loading..." si punem nodurile stabile
    if (!body.contains(tabsSection)) body.replaceChildren(topBox, reportBox, filesBox, tabsSection);

    const topNow = JSON.stringify([t.description, t.feedback]);
    if (topNow !== topKey) {
      topKey = topNow;
      // replaceChildren nativ scrie null ca text: filtram sectiunile lipsa
      topBox.replaceChildren(...[
        el("section", { class: "modal-section" },
          el("h3", {}, "Description"),
          t.description ? collapsible(t.description, null, () => descOpen, (v) => { descOpen = v; }) : el("p", { class: "hint" }, "No description.")),
        t.feedback ? el("section", { class: "modal-section" },
          el("h3", {}, "Feedback"), el("div", { class: "modal-text" }, renderMarkdown(t.feedback))) : null,
      ].filter(Boolean));
    }
    tabsSection.hidden = false;
    if (tab === null) tab = (data.conversation || []).length ? "conversation" : "history";
    tabConv.replaceChildren("Conversation", (data.conversation || []).length ? el("span", { class: "count" }, data.conversation.length) : null);
    renderHistory(data.events || []);
    renderConversation();
    applyTab();
    renderReport();
    renderFiles();
    renderActions(t, stuck);
  }

  // collapsible: text markdown lung, taiat la cateva randuri cu "Show more" (starea prin get/set)
  function collapsible(text, withStats, get, set) {
    const box = el("div", { class: "modal-text" }, renderMarkdown(text, withStats));
    if (text.length < 520) return box;
    const btn = el("button", { type: "button", class: "more-btn" });
    const paint = () => {
      box.classList.toggle("collapsed", !get());
      btn.textContent = get() ? "Show less" : "Show more";
      btn.setAttribute("aria-expanded", String(get()));
    };
    btn.addEventListener("click", () => { set(!get()); paint(); });
    paint();
    return el("div", { class: "modal-section" }, box, btn);
  }

  // renderSide: coloana din dreapta: starea si cine e implicat (se redeseneaza la fiecare refresh)
  function renderSide(t, agent, who, waiting, stuck, queuedInStage) {
    const byId = new Map(project.agents.map((a) => [a.id, a]));
    const person = (name, a) => el("span", { class: "side-value" }, avatar(name, a ? kindOf(a) : "user", 26), name);
    const reviewer = agent?.reviewerId ? byId.get(agent.reviewerId) : null;
    const creator = t.byUser ? null : byId.get(t.creatorId);
    const parent = t.parentTaskId ? (project.tasks || []).find((x) => x.id === t.parentTaskId) : null;
    const rows = [];
    const row = (label, value) => { if (value) rows.push(el("span", { class: "side-label" }, label), value); };
    row("Assignee", queuedInStage
      ? el("span", { class: "side-value hint" }, data.stage ? `queued in ${data.stage}` : "queued in stage")
      : who ? person(who, agent) : el("span", { class: "side-value hint" }, "Unassigned"));
    row("Reviewer", reviewer ? person(reviewer.name, reviewer) : null);
    row("Reporter", t.byUser ? person("You", null) : creator ? person(creator.name, creator) : null);
    row("Waiting on", waiting ? el("span", { class: "side-value", style: "color:var(--amber)" }, waiting) : null);
    row("Stage", data.stage && !queuedInStage ? el("span", { class: "side-value" }, data.stage) : null);
    row("Parent", parent ? el("span", { class: "side-value" },
      el("a", { href: `#/p/${projectId}/tasks/${parent.id}` }, el("span", { class: "task-id" }, `T-${parent.number}`), " ", parent.title)) : null);
    row("From", t.fromProject ? el("span", { class: "side-value" }, el("span", { class: "dot", style: "background:var(--teal)" }), t.fromProject) : null);
    row("Recurring", data.recurring ? el("span", { class: "side-value" },
      el("button", { type: "button", class: "link-btn", onclick: () => actions.openRecurring() }, data.recurring.title)) : null);
    row("Files", data.changesCount ? el("span", { class: "side-value" },
      el("button", { type: "button", class: "link-btn", onclick: () => setTab("changes") }, filesLabel())) : null);
    if (t.sendBacks) row("Sent back", el("span", { class: "side-value" }, `${t.sendBacks}\u00d7`));
    // consumul run-urilor de pe task (toti agentii care au lucrat pe el)
    const u = data.usage || {};
    const modelOf = (id) => (info.models || []).find((m) => m.id === id)?.label.replace(/ \(latest\)$/, "") || id;
    row("Model", agent?.model ? el("span", { class: "side-value" }, modelOf(agent.model)) : null);
    row("Duration", u.seconds > 0 ? el("span", { class: "side-value", title: `${u.runs} ${u.runs === 1 ? "run" : "runs"}; time the agents spent working` }, duration(u.seconds)) : null);
    row("Cost", u.costUsd > 0 ? el("span", { class: "side-value side-cost" }, `$${u.costUsd.toFixed(2)}`,
      u.tokens > 0 ? el("span", { class: "hint" }, `${compactTokens(u.tokens)} tokens`) : null) : null);
    const when = (iso) => {
      const d = new Date(iso);
      return `${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })}, ${clock(iso)}`;
    };
    side.replaceChildren(
      el("div", { class: "side-status" },
        el("span", { class: `pill pill-task-${t.status}` }, STATUS[t.status] || t.status),
        stuck ? el("span", { class: "badge-stuck", title: "No one is moving this task" }, "stuck") : null),
      el("div", { class: "side-rows" }, rows),
      el("div", { class: "side-foot" },
        el("span", {}, "Created " + when(t.createdAt)),
        t.updatedAt && t.updatedAt !== t.createdAt ? el("span", {}, (t.status === "done" ? "Resolved " : "Updated ") + when(t.updatedAt)) : null));
  }

  // filesLabel: "11 files +437 −16" din statisticile incarcate (sau doar numarul de modificari)
  function filesLabel() {
    if (!stats.size) return `${data.changesCount} ${data.changesCount === 1 ? "change" : "changes"}`;
    let added = 0, removed = 0;
    for (const v of stats.values()) { added += v.added; removed += v.removed; }
    return el("span", { class: "md-stat" }, `${stats.size} ${stats.size === 1 ? "file" : "files"}\u00a0`, statBadge({ added, removed }));
  }

  // renderFiles: primele fisiere modificate, deasupra tab-urilor; click duce in Changes
  function renderFiles() {
    const list = [...stats.entries()].sort((a, b) => (b[1].added + b[1].removed) - (a[1].added + a[1].removed));
    const key = list.map(([p, v]) => `${p}:${v.added}:${v.removed}`).join("|");
    if (key === filesKey) return;
    filesKey = key;
    if (!list.length) { filesBox.replaceChildren(); return; }
    const open = () => { setTab("changes"); tabsSection.scrollIntoView({ block: "nearest" }); };
    const top = list.slice(0, 3);
    filesBox.replaceChildren(el("section", { class: "modal-section" },
      el("div", { class: "files-head" }, "Changes \u00b7 ", filesLabel()),
      el("div", { class: "files-box" },
        top.map(([path, v]) => el("button", { type: "button", class: "files-box-row", onclick: open, title: path },
          el("span", { class: "fb-dir" }, path.includes("/") ? path.slice(0, path.lastIndexOf("/") + 1) : ""),
          el("span", { class: "fb-name" }, path.slice(path.lastIndexOf("/") + 1)),
          el("span", { class: "grow" }), statBadge(v))),
        list.length > top.length ? el("button", { type: "button", class: "files-box-row files-box-more", onclick: open },
          `${list.length - top.length} more ${list.length - top.length === 1 ? "file" : "files"}`) : null)));
  }

  function renderReport() {
    const result = data.task.result || "";
    if (result === reportKey) return;
    reportKey = result;
    reportBox.replaceChildren(...(result ? [reportNode(result)] : []));
  }

  function setTab(name) {
    tab = name;
    applyTab();
  }

  function applyTab() {
    const names = ["conversation", "history", "changes", "flow"];
    const tabs = [tabConv, tabHist, tabChg, tabFlow];
    const panes = [convPane, histPane, chgPane, flowPane];
    names.forEach((n, i) => {
      const on = tab === n;
      tabs[i].setAttribute("aria-selected", String(on));
      tabs[i].tabIndex = on ? 0 : -1;
      panes[i].hidden = !on;
    });
    syncChanges();
    syncFlow();
  }

  // syncFlow: cat timp tab-ul e deschis, arborele se cere la fiecare desenare a modalei
  // (un sub-task se poate schimba fara ca task-ul curent sa se miste)
  async function syncFlow() {
    if (tab !== "flow" || !data) return;
    const my = ++flowSeq;
    try {
      const res = await api(`/api/tasks/${taskId}/flow`);
      if (destroyed || my !== flowSeq) return;
      // arborele se redeseneaza doar daca s-a schimbat (focusul si scroll-ul raman)
      const key = JSON.stringify(res);
      if (key !== flowKey) {
        flowKey = key;
        flow.show(res);
      }
    } catch (err) {
      if (destroyed || my !== flowSeq) return;
      flowKey = null;
      flow.error(err.message);
    }
  }

  // syncChanges: /changes se cere doar cand changesCount (din GET /api/tasks/{id}) difera de cel incarcat;
  // si cu alt tab deschis, cand exista modificari (raportul si conversatia arata +N −M langa fisiere)
  async function syncChanges() {
    if (!data) return;
    const count = data.changesCount;
    if (tab !== "changes" && !count) return;
    if (count === chgCount) return;
    const my = ++chgSeq;
    try {
      const res = await api(`/api/tasks/${taskId}/changes`);
      if (destroyed || my !== chgSeq) return;
      chgCount = count;
      stats = changeStats(res);
      convKey = reportKey = null; // se redeseneaza cu badge-urile noi
      render(); // conversatia, raportul, rezumatul si coloana din dreapta, cu statisticile noi
      const hasSubtasks = (project.tasks || []).some((x) => x.parentTaskId === taskId);
      changes.show(res, hasSubtasks);
    } catch (err) {
      if (destroyed || my !== chgSeq) return;
      chgCount = null; // la urmatorul refresh se incearca din nou
      changes.error(err.message);
    }
  }

  function renderHistory(events) {
    const key = events.map((ev) => `${ev.at}|${ev.text}`).join("\n");
    if (key === histKey) return;
    histKey = key;
    histPane.replaceChildren(el("ol", { class: "history" }, ...events.map((ev) => el("li", {},
      el("time", { datetime: ev.at, title: new Date(ev.at).toLocaleString() }, whenText(ev.at)),
      el("span", {}, ev.text)))));
  }

  // renderConversation: se redeseneaza doar cand se schimba numarul de elemente, ultimul
  // moment sau cine lucreaza (ca "Show more" si scroll-ul sa ramana)
  function renderConversation() {
    const items = data.conversation || [];
    const last = items.length ? items[items.length - 1].at : "";
    const key = `${items.length}|${last}|${data.working || ""}|${data.workingAgentId || 0}`;
    if (key === convKey) return;
    convKey = key;
    const nodes = items.map(bubble);
    if (!items.length && !data.working) nodes.push(el("p", { class: "hint" }, "No conversation recorded for this task."));
    if (data.working) {
      nodes.push(el("div", { class: "conv-working" },
        el("span", {}, `${data.working} is working on it…`),
        data.workingAgentId ? el("a", { href: `#/p/${projectId}/team/${data.workingAgentId}` }, "Watch live") : null));
    }
    convPane.replaceChildren(...nodes);
  }

  // bubble: un mesaj din conversatie; "You" la dreapta, antet "from → to · ora"
  function bubble(it, idx) {
    const from = it.from || "?";
    const a = project.agents.find((x) => x.name === from);
    const key = `${it.type}|${it.kind || ""}|${it.at}|${from}|${it.to || ""}|${it.runId || ""}|${idx}`;
    const long = it.text.length > LONG_TEXT;
    const text = el("div", { class: "conv-text" });
    const more = long ? el("button", { type: "button", class: "btn small conv-more" }) : null;
    const paint = () => {
      const open = expanded.has(key);
      text.replaceChildren(renderMarkdown(long && !open ? it.text.slice(0, LONG_TEXT).trimEnd() + "…" : it.text, stats));
      if (more) {
        more.textContent = open ? "Show less" : "Show more";
        more.setAttribute("aria-expanded", String(open));
      }
    };
    if (more) {
      more.addEventListener("click", () => {
        if (expanded.has(key)) expanded.delete(key); else expanded.add(key);
        paint();
      });
    }
    paint();
    const showTo = it.type === "event" && it.to;
    return el("div", { class: `conv-msg${from === "You" ? " me" : ""}` },
      el("div", { class: "conv-head" },
        avatar(from, a ? kindOf(a) : "user", 20),
        el("span", { class: "conv-from" }, from),
        showTo ? el("span", { class: "conv-arrow", "aria-hidden": "true" }, "→") : null,
        showTo ? el("span", { class: "conv-from" }, it.to) : null,
        el("span", { class: "conv-sep", "aria-hidden": "true" }, "·"),
        el("time", { datetime: it.at, title: new Date(it.at).toLocaleString() }, whenText(it.at))),
      text, more);
  }

  // reportNode: raportul agentului, cu "Show more" cand e lung
  function reportNode(result) {
    const author = data.assignee || "the agent";
    return el("section", { class: "modal-section" },
      el("h3", {}, `Report from ${author}`),
      collapsible(result, stats, () => reportOpen, (v) => { reportOpen = v; }));
  }

  // rootName: numele radacinii agentului (urcand pe parentId); garda contra ciclurilor
  function rootName(agentId) {
    const byId = new Map(project.agents.map((a) => [a.id, a]));
    let a = byId.get(agentId);
    const seen = new Set();
    while (a?.parentId && byId.has(a.parentId) && !seen.has(a.id)) { seen.add(a.id); a = byId.get(a.parentId); }
    return a ? a.name : "The root lead";
  }

  // renderActions: zona se reconstruieste doar cand se schimba ce trebuie aratat
  // (starea, decizia utilizatorului, stuck, asignatul); altfel butoanele si
  // textarea raman pe loc la fiecare refresh.
  function renderActions(t, stuck) {
    // mine: utilizatorul poate decide din board (si in modul autonom, unde decide radacina)
    const mine = t.status === "review" && (project.userCanDecide || []).includes(t.id);
    // byRoot: in modul autonom decide radacina asignatului; utilizatorul poate si el
    const byRoot = mine && !(project.userDecides || []).includes(t.id) ? rootName(t.assigneeId) : null;
    const canAssign = t.status === "queued" || t.status === "blocked";
    const choices = canAssign ? actions.assignable(t.stageId) : [];
    const key = [t.status, mine, byRoot, stuck, t.assigneeId, choices.map((a) => a.id).join(".")].join("|");
    if (key === actionsKey) return;
    actionsKey = key;

    // run: apasa o actiune o singura data; butoanele se reactiveaza daca a esuat
    const run = (btn, fn) => async () => {
      btn.disabled = true;
      const ok = await fn();
      if (!destroyed) btn.disabled = false;
      return ok;
    };
    const parts = [];
    if (mine) {
      const accept = el("button", { type: "button", class: "btn small primary" }, "Accept");
      const back = el("button", { type: "button", class: "btn small" }, "Send back");
      accept.addEventListener("click", run(accept, () => actions.decide(t, "accept", "")));
      back.addEventListener("click", run(back, async () => {
        if (!feedback.value.trim()) { feedback.focus(); return false; }
        const ok = await actions.decide(t, "send_back", feedback.value.trim());
        if (ok) feedback.value = "";
        return ok;
      }));
      parts.push(el("p", { class: "hint" }, byRoot ? `${byRoot} decides this; you can decide it too.` : "You decide this task."), feedback,
        el("div", { class: "actions" }, accept, back));
    }
    if (t.status === "blocked") {
      const accept = el("button", { type: "button", class: "btn small primary" }, "Accept");
      const unblock = el("button", { type: "button", class: "btn small" }, "Unblock");
      accept.addEventListener("click", run(accept, () => actions.taskAction(t, "accept")));
      unblock.addEventListener("click", run(unblock, () => actions.taskAction(t, "unblock")));
      parts.push(el("div", { class: "actions" }, accept, unblock));
    }
    if (canAssign) {
      const current = project.agents.find((a) => a.id === t.assigneeId);
      parts.push(el("label", { class: "assign-row" },
        el("span", { class: "hint" }, "Assignee"),
        el("select", {
          class: "select-small", value: String(t.assigneeId), "aria-label": `Assignee for T-${t.number}`,
          onchange: (e) => actions.assign(t, Number(e.target.value)),
        }, choices.map((a) => el("option", { value: a.id }, a.name)),
          !choices.some((a) => a.id === t.assigneeId) ? el("option", { value: t.assigneeId }, current ? current.name : "?") : null)));
    }
    if (stuck) {
      const nudge = el("button", { type: "button", class: "btn small" }, "Nudge");
      nudge.addEventListener("click", run(nudge, () => actions.taskAction(t, "nudge")));
      parts.push(el("div", { class: "actions" }, el("span", { class: "hint" }, "No one is moving this task."), nudge));
    }
    actionsBox.hidden = !parts.length;
    actionsBox.replaceChildren(...parts); // replaceChildren nativ nu aplatizeaza array-uri
  }

  return {
    el: overlay,
    taskId,
    tab: () => tab,
    flowOpen: flow.open,
    // refresh: dupa un reload al proiectului (SSE) reincarcam task-ul
    refresh(p) {
      if (destroyed) return;
      if (p) project = p;
      load();
    },
    destroy() {
      if (destroyed) return;
      destroyed = true;
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
      overlay.remove();
    },
  };
}

// duration: 45 s, 23 min, 1 h 05 min
function duration(sec) {
  if (sec < 60) return `${sec} s`;
  const m = Math.round(sec / 60);
  if (m < 60) return `${m} min`;
  return `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, "0")} min`;
}

// compactTokens: 1234567 -> "1.2M"
function compactTokens(n) {
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
  if (n >= 1e3) return Math.round(n / 1e3) + "k";
  return String(n);
}
