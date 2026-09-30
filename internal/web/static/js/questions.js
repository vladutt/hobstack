import { api } from "./api.js";
import { el, toast } from "./ui.js";

// questionBox: o intrebare a unui agent catre utilizator, cu optiunile ca
// butoane si un raspuns liber (Send / Dismiss). onDone se cheama dupa trimitere.
export function questionBox(q, { agentName, taskRef, onDone }) {
  const answer = el("textarea", { rows: 2, maxlength: 4000, placeholder: "Your answer", "aria-label": `Answer to ${agentName}` });
  const send = async (text) => {
    if (!text.trim()) { answer.focus(); return; }
    busy(true);
    try {
      await api(`/api/questions/${q.id}/answer`, { method: "POST", body: { text } });
      await onDone?.();
    } catch (err) {
      toast(err.message);
      busy(false);
    }
  };
  const dismiss = async () => {
    busy(true);
    try {
      await api(`/api/questions/${q.id}/dismiss`, { method: "POST" });
      await onDone?.();
    } catch (err) {
      toast(err.message);
      busy(false);
    }
  };
  const buttons = [];
  const button = (label, cls, onclick) => {
    const b = el("button", { type: "button", class: cls, onclick }, label);
    buttons.push(b);
    return b;
  };
  function busy(on) { for (const b of buttons) b.disabled = on; answer.disabled = on; }
  answer.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); send(answer.value); }
  });
  const options = q.options || [];
  return el("div", { class: "question" },
    el("div", { class: "question-head" }, el("strong", {}, agentName), taskRef ? " · " + taskRef : null),
    el("p", { class: "question-text" }, q.text),
    options.length ? el("div", { class: "actions" }, options.map((o) => button(o, "btn small", () => send(o)))) : null,
    el("div", { class: "question-answer" }, answer,
      button("Send", "btn primary small", () => send(answer.value)),
      button("Dismiss", "btn small", dismiss)));
}
