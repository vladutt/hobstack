import { api } from "./api.js";
import { el, avatar, kindOf, toast } from "./ui.js";

// agentRequestBox: un lider cere un agent nou ("Lead wants to add Quinn"), cu
// rolul, modelul, tool-urile, motivul si instructiunile (restranse). Butoane:
// Approve, Dismiss. onDone se cheama dupa o actiune reusita; la eroare
// (ex. numele s-a ocupat intre timp) apare un toast si cardul ramane.
export function agentRequestBox(r, { lead, onDone }) {
  const s = r.spec || {};
  const buttons = [];
  const act = (label, cls, path) => {
    const b = el("button", { type: "button", class: cls, onclick: async () => {
      for (const x of buttons) x.disabled = true;
      try { await api(path, { method: "POST" }); await onDone?.(); }
      catch (err) { toast(err.message); for (const x of buttons) x.disabled = false; }
    } }, label);
    buttons.push(b);
    return b;
  };
  const tools = (s.tools || []).length ? s.tools.join(", ") : "read only";
  const more = el("details", { class: "agent-request-more" },
    el("summary", {}, "Show instructions"),
    el("p", { class: "question-text" }, s.instructions || ""));
  return el("div", { class: "question agent-request" },
    el("div", { class: "question-head" }, lead ? avatar(lead.name, kindOf(lead), 20) : null,
      el("strong", {}, `${r.leadName || "A lead"} wants to add ${s.name}`),
      r.ref ? ` · ${r.ref}` : null),
    el("p", { class: "question-text" }, `${s.role} · ${s.model} · tools: ${tools}`),
    el("p", { class: "question-text" }, r.reason),
    more,
    el("div", { class: "actions" },
      act("Approve", "btn small primary", `/api/agent-requests/${r.id}/approve`),
      act("Dismiss", "btn small", `/api/agent-requests/${r.id}/dismiss`)));
}
