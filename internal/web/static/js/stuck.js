import { api } from "./api.js";
import { el, avatar, kindOf, toast } from "./ui.js";

// stuckAge: de cat timp sta task-ul, scurt: "X min" (sub 120 min), "X h" (sub 48 h), altfel "X d".
export function stuckAge(since) {
  const mins = Math.max(1, Math.round((Date.now() - new Date(since)) / 60000));
  if (mins < 120) return `${mins} min`;
  const hours = Math.floor(mins / 60);
  if (hours < 48) return `${hours} h`;
  return `${Math.floor(hours / 24)} d`;
}

// refreshStuckAges: actualizeaza varstele afisate in root, fara sa redeseneze
// casetele (un raspuns tastat in banda ramane pe loc).
export function refreshStuckAges(root) {
  for (const span of root.querySelectorAll(".stuck-age[data-since]")) {
    span.textContent = stuckAge(span.dataset.since);
  }
}

// stuckBox: un task care nu se mai misca, cu agentul responsabil.
// onOpen(href, event) se cheama la click pe "Open <agent>" (apelantul alege panoul Chat).
export function stuckBox(s, { agent, projectId, onDone, onOpen }) {
  const why = s.kind === "orphan"
    ? "in progress but nothing is running · "
    : `waiting for ${s.agentName} to decide · `;
  const nudge = el("button", { type: "button", class: "btn small primary", onclick: async () => {
    nudge.disabled = true;
    try { await api(`/api/tasks/${s.taskId}/nudge`, { method: "POST" }); await onDone?.(); }
    catch (err) { toast(err.message); nudge.disabled = false; }
  } }, "Nudge again");
  const href = `#/p/${projectId}/team/${s.agentId}`;
  return el("div", { class: "question stuck" },
    el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
      el("strong", {}, `${s.ref} · ${s.title}`)),
    el("p", { class: "question-text" }, why,
      el("span", { class: "stuck-age", "data-since": s.since }, stuckAge(s.since))),
    el("div", { class: "actions" }, nudge,
      el("a", { class: "btn small", href, onclick: (e) => onOpen?.(href, e) }, `Open ${s.agentName}`)));
}
