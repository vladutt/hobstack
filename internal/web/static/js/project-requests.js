import { api } from "./api.js";
import { el, toast } from "./ui.js";

// workRequestBox: un proiect legat cere munca ("Frontend (Lead) asks for work: Add /export"),
// cu descrierea restransa si Approve / Dismiss. onDone se cheama dupa o actiune reusita;
// la eroare (ex. legatura a fost stearsa) apare un toast si cardul ramane.
export function workRequestBox(r, { onDone }) {
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
  return el("div", { class: "question work-request" },
    el("div", { class: "question-head" },
      el("strong", {}, `${r.fromProject} (${r.fromAgent}) asks for work: ${r.title}`),
      ` · ${r.ref}`),
    r.text ? el("details", { class: "agent-request-more" },
      el("summary", {}, "Show description"),
      el("p", { class: "question-text" }, r.text)) : null,
    el("div", { class: "actions" },
      act("Approve", "btn small primary", `/api/project-requests/${r.id}/approve`),
      act("Dismiss", "btn small", `/api/project-requests/${r.id}/dismiss`)));
}
