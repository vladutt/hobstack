import { api } from "./api.js";
import { el, avatar, kindOf, toast } from "./ui.js";

// stuckBox: un task care nu se mai misca, cu agentul responsabil.
// onOpen se cheama la click pe "Open <agent>" (apelantul alege panoul Chat).
export function stuckBox(s, { agent, projectId, onDone, onOpen }) {
  const mins = Math.max(1, Math.round((Date.now() - new Date(s.since)) / 60000));
  const why = s.kind === "orphan"
    ? `in progress but nothing is running · ${mins} min`
    : `waiting for ${s.agentName} to decide · ${mins} min`;
  const nudge = el("button", { type: "button", class: "btn small primary", onclick: async () => {
    nudge.disabled = true;
    try { await api(`/api/tasks/${s.taskId}/nudge`, { method: "POST" }); await onDone?.(); }
    catch (err) { toast(err.message); nudge.disabled = false; }
  } }, "Nudge again");
  const href = `#/p/${projectId}/team/${s.agentId}`;
  return el("div", { class: "question stuck" },
    el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
      el("strong", {}, `${s.ref} · ${s.title}`)),
    el("p", { class: "question-text" }, why),
    el("div", { class: "actions" }, nudge,
      el("a", { class: "btn small", href, onclick: () => onOpen?.(href) }, `Open ${s.agentName}`)));
}
