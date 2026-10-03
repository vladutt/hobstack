import { el, clock } from "./ui.js";
import { renderMarkdown } from "./md.js";

// Etichetele starilor (ca in task-modal.js).
const STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
};

// Culoarea unui pas dupa tip.
const TONE = {
  accepted: "ok", user_accepted: "ok", approved: "ok",
  sent_back: "bad", rejected: "bad", blocked: "bad", failed: "bad",
  asked: "warn", requested: "user",
};

// when: "HH:MM" azi, altfel "Sep 30 14:02"
function when(iso) {
  const d = new Date(iso);
  if (d.toDateString() === new Date().toDateString()) return clock(iso);
  return `${d.toLocaleDateString("en-US", { month: "short", day: "numeric" })} ${clock(iso)}`;
}

// span: "42m", "2h 05m", "3d 4h"
function span(from, to) {
  const min = Math.max(0, Math.round((new Date(to) - new Date(from)) / 60000));
  if (min < 60) return `${min}m`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h}h ${String(min % 60).padStart(2, "0")}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

// flowView: tab-ul "Flow" al unui task. Pasii desfasurati (cheie: task + ora + tip)
// raman intre reincarcari; open (Set optional) permite preluarea lor de la o alta instanta.
// show(data) cu raspunsul GET /api/tasks/{id}/flow. Intoarce { el, open, show, error }.
export function flowView({ projectId, open = new Set() }) {
  const root = el("div", { class: "flow" }, el("p", { class: "hint" }, "Loading..."));
  let focusId = null;

  function stepNode(task, s) {
    const key = `${task.id}|${s.at}|${s.kind}`;
    const body = s.body ? el("div", { class: "flow-body", hidden: !open.has(key) }) : null;
    let built = false;
    const build = () => { if (!built && body) { built = true; body.append(renderMarkdown(s.body)); } };
    if (body && open.has(key)) build();
    const btn = body ? el("button", { type: "button", class: "btn small flow-more", "aria-expanded": String(open.has(key)) },
      open.has(key) ? "Hide" : "Show") : null;
    btn?.addEventListener("click", () => {
      if (open.has(key)) open.delete(key); else open.add(key);
      build();
      body.hidden = !open.has(key);
      btn.textContent = open.has(key) ? "Hide" : "Show";
      btn.setAttribute("aria-expanded", String(open.has(key)));
    });
    return el("li", { class: `flow-step${TONE[s.kind] ? " tone-" + TONE[s.kind] : ""}` },
      el("time", { datetime: s.at, title: new Date(s.at).toLocaleString() }, when(s.at)),
      // pasul sintetic al task-urilor vechi nu are actor: fara eticheta
      s.actor ? el("span", { class: "flow-who" }, s.actor, s.to ? el("span", { class: "flow-arrow", "aria-hidden": "true" }, " → ") : null, s.to || null) : null,
      el("span", { class: "flow-text" }, s.kind === "requested" ? "asked in chat" : s.text),
      btn, body);
  }

  // childAt: ora la care a fost creat sub-task-ul (pasul "created", altfel createdAt)
  const childAt = (c) => (c.steps.find((s) => s.kind === "created") || { at: c.createdAt }).at;

  function taskNode(task) {
    const focus = task.id === focusId;
    const title = [el("span", { class: "task-id" }, `T-${task.number}`), "· ", task.title];
    const head = focus
      ? el("div", { class: "flow-head" }, el("span", { class: "flow-title" }, title))
      : el("a", { class: "flow-head", href: `#/p/${projectId}/tasks/${task.id}` }, el("span", { class: "flow-title" }, title));
    head.append(...[
      el("span", { class: `pill pill-task-${task.status}` }, STATUS[task.status] || task.status),
      task.assignee ? el("span", { class: "hint" }, task.assignee) : null,
      task.doneAt ? el("span", { class: "hint" }, `Done · ${span(task.createdAt, task.doneAt)}`) : null,
    ].filter(Boolean));
    // pasii si sub-task-urile, intercalate dupa ora
    const items = [
      ...task.steps.map((s) => ({ at: s.at, node: () => stepNode(task, s) })),
      ...task.children.map((c) => ({ at: childAt(c), node: () => el("li", { class: "flow-child" }, taskNode(c)) })),
    ].sort((a, b) => new Date(a.at) - new Date(b.at));
    const list = el("ol", { class: "flow-steps" });
    for (const it of items) list.append(it.node());
    return el("section", { class: `flow-task${focus ? " focus" : ""}` }, head, list);
  }

  return {
    el: root,
    open,
    show(data) {
      focusId = data.focusId;
      const nodes = [taskNode(data.root)];
      if (!data.root.children.length) nodes.push(el("p", { class: "hint" }, "No delegation: this task was done by one agent."));
      root.replaceChildren(...nodes);
    },
    error(msg) {
      root.replaceChildren(el("p", { class: "hint" }, msg));
    },
  };
}
