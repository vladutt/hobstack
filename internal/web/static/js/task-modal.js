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
// Intoarce { el, taskId, refresh(project), destroy() }.
export function taskModal({ projectId, taskId, project, onClose, actions }) {
  let data = null;        // ultimul raspuns GET /api/tasks/{id}
  let seq = 0;            // numarul ultimei cereri: un raspuns vechi nu suprascrie unul nou
  let destroyed = false;
  let reportOpen = false; // "Show report" ramane desfasurat la refresh
  let actionsKey = null;  // ce s-a desenat ultima data in zona de actiuni
  let tab = null;         // "conversation" | "history" | "changes" | "flow"; null = inca nealeasa (se alege la primul raspuns)
  let convKey = null;     // ce s-a desenat ultima data in conversatie
  let histKey = null;     // idem pentru istoric
  let chgCount = null;    // changesCount-ul pentru care s-au incarcat datele Changes (null = neincarcat)
  let chgSeq = 0;         // ca seq, pentru cererea Changes
  const changes = changesView();
  let flowSeq = 0;        // ca seq, pentru cererea Flow
  const flow = flowView({ projectId });
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
  const tabHist = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": histPane.id, onclick: () => setTab("history") }, "History");
  const chgPane = el("div", { id: `chg-${taskId}`, role: "tabpanel", hidden: true }, changes.el);
  const tabChg = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": chgPane.id, onclick: () => setTab("changes") }, "Changes");
  const flowPane = el("div", { id: `flow-${taskId}`, role: "tabpanel", hidden: true }, flow.el);
  const tabFlow = el("button", { type: "button", class: "tab", role: "tab", "aria-controls": flowPane.id, onclick: () => setTab("flow") }, "Flow");
  const tabsSection = el("section", { class: "modal-section", hidden: true },
    el("div", { class: "modal-tabs", role: "tablist" }, tabConv, tabHist, tabChg, tabFlow), convPane, histPane, chgPane, flowPane);
  const reportBox = el("div", { class: "modal-top" });
  const body = el("div", { class: "modal-body" }, el("p", { class: "hint" }, "Loading..."));
  const actionsBox = el("div", { class: "modal-actions", hidden: true });
  const dialog = el("div", { class: "modal", role: "dialog", "aria-modal": "true", "aria-labelledby": titleId, tabindex: "-1" },
    el("div", { class: "modal-head" }, head, closeBtn),
    body, actionsBox);
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
    head.replaceChildren(el("h2", { id: titleId }, "Task not found"));
    body.replaceChildren(el("p", { class: "hint" }, msg === "Not found" || /not found/i.test(msg) ? "This task does not exist (anymore)." : msg));
    actionsBox.hidden = true;
    actionsBox.replaceChildren();
    actionsKey = null;
    topKey = histKey = convKey = reportKey = null;
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
      el("h2", { id: titleId, class: "modal-title" }, el("span", { class: "task-id" }, `T-${t.number}`), "· ", t.title),
      el("div", { class: "modal-meta" },
        el("span", { class: `pill pill-task-${t.status}` }, STATUS[t.status] || t.status),
        data.recurring ? el("button", { type: "button", class: "link-btn", onclick: () => actions.openRecurring() },
          `From recurring: ${data.recurring.title}`) : null,
        queuedInStage ? el("span", { class: "hint" }, data.stage ? `queued in stage ${data.stage}` : "queued in stage") : null,
        who ? el("span", { class: "modal-who" }, avatar(who, kindOf(agent), 20), who) : null,
        data.stage && !queuedInStage ? el("span", { class: "hint" }, `Stage: ${data.stage}`) : null,
        waiting ? el("span", { class: "task-stage" }, `with ${waiting}`) : null,
        stuck ? el("span", { class: "badge-stuck", title: "No one is moving this task" }, "stuck") : null));

    // prima data scoatem "Loading..." si punem nodurile stabile
    if (!body.contains(tabsSection)) body.replaceChildren(topBox, tabsSection, reportBox);

    const topNow = JSON.stringify([t.description, t.feedback]);
    if (topNow !== topKey) {
      topKey = topNow;
      // replaceChildren nativ scrie null ca text: filtram sectiunile lipsa
      topBox.replaceChildren(...[
        el("section", { class: "modal-section" },
          el("h3", {}, "Description"),
          t.description ? el("div", { class: "modal-text" }, renderMarkdown(t.description)) : el("p", { class: "hint" }, "No description.")),
        t.feedback ? el("section", { class: "modal-section" },
          el("h3", {}, "Feedback"), el("div", { class: "modal-text" }, renderMarkdown(t.feedback))) : null,
      ].filter(Boolean));
    }
    tabsSection.hidden = false;
    if (tab === null) tab = (data.conversation || []).length ? "conversation" : "history";
    renderHistory(data.events || []);
    renderConversation();
    applyTab();
    renderReport();
    renderActions(t, stuck);
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
      flow.show(res);
    } catch (err) {
      if (destroyed || my !== flowSeq) return;
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
      renderConversation();
      renderReport();
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

  // reportNode: raportul AI, ascuns implicit; langa buton, rezumatul fisierelor modificate
  function reportNode(result) {
    const text = el("div", { class: "modal-report", hidden: !reportOpen }, renderMarkdown(result, stats));
    const btn = el("button", {
      type: "button", class: "btn small", "aria-expanded": String(reportOpen),
      onclick: () => {
        reportOpen = !reportOpen;
        text.hidden = !reportOpen;
        btn.textContent = reportOpen ? "Hide report" : "Show report";
        btn.setAttribute("aria-expanded", String(reportOpen));
      },
    }, reportOpen ? "Hide report" : "Show report");
    return el("section", { class: "modal-section" }, el("div", { class: "report-head" }, btn, filesSummary()), text);
  }

  // filesSummary: "2 files changed +3 −1"; click deschide tab-ul Changes
  function filesSummary() {
    if (!stats.size) return null;
    let added = 0, removed = 0;
    for (const s of stats.values()) { added += s.added; removed += s.removed; }
    const n = stats.size;
    return el("button", { type: "button", class: "report-files", title: "Open the Changes tab", onclick: () => { setTab("changes"); tabsSection.scrollIntoView({ block: "nearest" }); } },
      `${n} ${n === 1 ? "file" : "files"} changed`, statBadge({ added, removed }));
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
