import { api } from "./api.js";
import { el, avatar, kindOf, toast } from "./ui.js";
import { stuckAge } from "./stuck.js";

// accessBox: un tool refuzat unui agent ("Beacon was denied Bash" + comanda).
// Butoane: Enable Shell for <agent> (doar daca r.canEnableShell), Allow package installs
// (doar daca r.canEnablePackages), Open <agent>, Dismiss.
// onDone se cheama dupa o actiune reusita; onOpen(href, event) ca la stuckBox.
export function accessBox(r, { agent, projectId, onDone, onOpen }) {
  const name = r.agentName || "Agent";
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
  if (r.prefix) {
    return el("div", { class: "question access cmd-ask" },
      el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
        el("span", { class: "label" }, "COMMAND · ASK"),
        el("strong", {}, `${name} wants to run`),
        r.count > 1 ? ` (${r.count}×)` : null,
        r.ref ? ` · ${r.ref}` : null),
      el("p", { class: "question-text" }, el("code", {}, r.detail || r.prefix), " · ",
        el("span", { class: "stuck-age", "data-since": r.updatedAt }, stuckAge(r.updatedAt)), " ago"),
      el("div", { class: "actions" },
        act("Allow once", "btn small primary", `/api/access/${r.id}/allow-once`),
        act(`Allow "${r.prefix}" in this project`, "btn small", `/api/access/${r.id}/allow-project`),
        act("Block", "btn small", `/api/access/${r.id}/block-project`),
        act("Dismiss", "btn small", `/api/access/${r.id}/dismiss`)));
  }
  const href = `#/p/${projectId}/team/${r.agentId}`;
  return el("div", { class: "question access" },
    el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
      el("strong", {}, `${name} was denied ${r.tool}`),
      r.count > 1 ? ` (${r.count}×)` : null,
      r.ref ? ` · ${r.ref}` : null),
    el("p", { class: "question-text" },
      r.detail ? el("code", {}, r.detail) : null,
      r.detail ? " · " : null,
      el("span", { class: "stuck-age", "data-since": r.updatedAt }, stuckAge(r.updatedAt)), " ago"),
    el("div", { class: "actions" },
      r.canEnableShell ? act(`Enable Shell for ${name}`, "btn small primary", `/api/access/${r.id}/enable-shell`) : null,
      r.canEnablePackages ? act(`Allow package installs for ${name}`, "btn small primary", `/api/access/${r.id}/enable-packages`) : null,
      el("a", { class: "btn small", href, onclick: (e) => onOpen?.(href, e) }, `Open ${name}`),
      act("Dismiss", "btn small", `/api/access/${r.id}/dismiss`)));
}
