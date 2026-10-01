import { api } from "./api.js";
import { el, avatar, clock, kindOf } from "./ui.js";

// Etichetele starilor unui task (pill-ul din antetul modalei).
const STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
};

// taskModal: fereastra unui task deschisa peste board (adresa #/p/<id>/tasks/<taskId>).
// - project: proiectul curent (agenti, userDecides, stuck); refresh(project) il inlocuieste.
// - onClose(): cerut la Esc / click pe fundal / x (apelantul schimba adresa si cheama destroy).
// - actions: functiile din workspace, ca sa nu dublam cererile:
//   taskAction(t, "accept"|"unblock"|"nudge"), decide(t, action, feedback), assign(t, agentId),
//   assignable(stageId) -> agenti, waitingOn(t) -> numele la care asteapta un task din review.
//   Actiunile intorc true daca au reusit.
// Intoarce { el, taskId, refresh(project), destroy() }.
export function taskModal({ projectId, taskId, project, onClose, actions }) {
  let data = null;        // ultimul raspuns GET /api/tasks/{id}
  let seq = 0;            // numarul ultimei cereri: un raspuns vechi nu suprascrie unul nou
  let destroyed = false;
  let reportOpen = false; // "Show report" ramane desfasurat la refresh
  let actionsKey = null;  // ce s-a desenat ultima data in zona de actiuni
  // textarea de feedback se creeaza o singura data: textul scris ramane chiar
  // daca zona de actiuni se reconstruieste
  const feedback = el("textarea", { rows: 3, placeholder: "What needs to change? (needed for Send back)", "aria-label": "Feedback" });

  const titleId = `task-modal-title-${taskId}`;
  const closeBtn = el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: () => onClose() }, "×");
  const head = el("div", { class: "modal-head-main" });
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
  overlay.addEventListener("click", (e) => { if (downOnOverlay && e.target === overlay) onClose(); });

  const onKey = (e) => {
    if (e.key === "Escape") { e.preventDefault(); onClose(); return; }
    if (e.key === "Tab") trapFocus(e);
  };
  document.addEventListener("keydown", onKey);
  const prevOverflow = document.body.style.overflow;
  document.body.style.overflow = "hidden"; // pagina din spate nu se mai deruleaza
  document.body.append(overlay);
  dialog.focus();
  load();

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
  }

  function render() {
    const t = data.task;
    const agent = project.agents.find((a) => a.id === t.assigneeId);
    const who = data.assignee || agent?.name || "";
    const waiting = t.status === "review" ? actions.waitingOn(t) : null;
    const stuck = (project.stuck || []).some((s) => s.taskId === t.id);
    head.replaceChildren(
      el("h2", { id: titleId, class: "modal-title" }, el("span", { class: "task-id" }, `T-${t.number}`), "· ", t.title),
      el("div", { class: "modal-meta" },
        el("span", { class: `pill pill-task-${t.status}` }, STATUS[t.status] || t.status),
        who ? el("span", { class: "modal-who" }, avatar(who, kindOf(agent), 20), who) : null,
        data.stage ? el("span", { class: "hint" }, `Stage: ${data.stage}`) : null,
        waiting ? el("span", { class: "task-stage" }, `with ${waiting}`) : null,
        stuck ? el("span", { class: "badge-stuck", title: "No one is moving this task" }, "stuck") : null));

    const events = data.events || [];
    // replaceChildren nativ scrie null ca text: filtram sectiunile lipsa
    body.replaceChildren(...[
      el("section", { class: "modal-section" },
        el("h3", {}, "Description"),
        t.description ? el("div", { class: "modal-text" }, t.description) : el("p", { class: "hint" }, "No description.")),
      t.feedback ? el("section", { class: "modal-section" },
        el("h3", {}, "Feedback"), el("div", { class: "modal-text" }, t.feedback)) : null,
      el("section", { class: "modal-section" },
        el("h3", {}, "History"),
        el("ol", { class: "history" }, ...events.map((ev) => el("li", {},
          el("time", { datetime: ev.at, title: new Date(ev.at).toLocaleString() }, clock(ev.at)),
          el("span", {}, ev.text))))),
      t.result ? reportNode(t.result) : null,
    ].filter(Boolean));
    renderActions(t, stuck);
  }

  // reportNode: raportul AI, ascuns implicit
  function reportNode(result) {
    const text = el("pre", { class: "modal-report", hidden: !reportOpen }, result);
    const btn = el("button", {
      type: "button", class: "btn small", "aria-expanded": String(reportOpen),
      onclick: () => {
        reportOpen = !reportOpen;
        text.hidden = !reportOpen;
        btn.textContent = reportOpen ? "Hide report" : "Show report";
        btn.setAttribute("aria-expanded", String(reportOpen));
      },
    }, reportOpen ? "Hide report" : "Show report");
    return el("section", { class: "modal-section" }, el("div", {}, btn), text);
  }

  // renderActions: zona se reconstruieste doar cand se schimba ce trebuie aratat
  // (starea, decizia utilizatorului, stuck, asignatul); altfel butoanele si
  // textarea raman pe loc la fiecare refresh.
  function renderActions(t, stuck) {
    const mine = t.status === "review" && (project.userDecides || []).includes(t.id);
    const canAssign = t.status === "queued" || t.status === "blocked";
    const choices = canAssign ? actions.assignable(t.stageId) : [];
    const key = [t.status, mine, stuck, t.assigneeId, choices.map((a) => a.id).join(".")].join("|");
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
      parts.push(el("p", { class: "hint" }, "You decide this task."), feedback,
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
