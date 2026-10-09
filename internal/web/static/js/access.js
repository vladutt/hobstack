import { api } from "./api.js";
import { el, avatar, kindOf, toast } from "./ui.js";
import { stuckAge } from "./stuck.js";

// accessBox: un tool refuzat unui agent ("Beacon was denied Bash" + comanda).
// Butoane: Enable Shell for <agent> (doar daca r.canEnableShell), Allow package installs
// (doar daca r.canEnablePackages), Open <agent>, Dismiss.
// Cu prefix (r.kind "ask" sau "command"): Allow "<prefix>" on T-N (sau in this chat), Allow in this
// project (doar ask, cu Shell), Block (doar ask), Enable Shell (doar fara Shell), Dismiss.
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
    // ask = regula Ask (cu Shell); command = comanda in afara tool-urilor unui agent fara Shell
    // dupa taskId, nu dupa ref: un task care lipseste din lista proiectului tot pe task primeste grant-ul
    const where = r.taskId ? `on ${r.ref || "this task"}` : "in this chat";
    return el("div", { class: "question access cmd-ask" },
      el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
        el("span", { class: "label" }, r.kind === "ask" ? "COMMAND · ASK" : "COMMAND"),
        el("strong", {}, `${name} wants to run`),
        el("span", {}, (r.count > 1 ? ` (${r.count}×)` : "") + (r.ref ? ` · ${r.ref}` : ""))),
      el("p", { class: "question-text" }, el("code", {}, r.detail || r.prefix), " · ",
        el("span", { class: "stuck-age", "data-since": r.updatedAt }, stuckAge(r.updatedAt)), " ago"),
      el("div", { class: "actions" },
        act(`Allow "${r.prefix}" ${where}`, "btn small primary", `/api/access/${r.id}/allow-task`),
        r.kind === "ask" && r.hasShell ? act(`Allow "${r.prefix}" in this project`, "btn small", `/api/access/${r.id}/allow-project`) : null,
        r.kind === "ask" ? act("Block", "btn small", `/api/access/${r.id}/block-project`) : null,
        r.canEnableShell ? act(`Enable Shell for ${name}`, "btn small", `/api/access/${r.id}/enable-shell`) : null,
        act("Dismiss", "btn small", `/api/access/${r.id}/dismiss`)));
  }
  const href = `#/p/${projectId}/team/${r.agentId}`;
  return el("div", { class: "question access" },
    el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
      el("strong", {}, `${name} was denied ${r.tool}`),
      el("span", {}, (r.count > 1 ? ` (${r.count}×)` : "") + (r.ref ? ` · ${r.ref}` : ""))),
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
