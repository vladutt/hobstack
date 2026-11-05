import { api } from "./api.js";
import { toast } from "./ui.js";

// moveAction: ce inseamna sa lasi task-ul pe coloana target (aceleasi reguli ca team.MoveTask).
// target: "queued" | "done" | "stage:<id>" | null (coloana nu primeste carduri).
// Intoarce { ok: true, label, needsReason } sau { ok: false, why } (motivul pentru utilizator).
export function moveAction(t, target) {
  const ref = `T-${t.number}`;
  if (!target) return { ok: false, why: "Tasks get here only through the agents' work." };
  if (t.status === "archived") return { ok: false, why: `${ref} was deleted.` };
  if (t.status === "in_progress") return { ok: false, why: `An agent is working on ${ref}; wait for it to finish.` };
  if (target === "done") {
    if (t.status === "review") return { ok: true, label: "Accept" };
    if (t.status === "blocked") return { ok: true, label: "Accept" };
    if (t.status === "queued") return { ok: true, label: "Close" };
    return { ok: false, why: `${ref} is already done.` };
  }
  if (target === "queued") {
    if (t.status === "review") return { ok: true, label: "Send back", needsReason: true };
    if (t.status === "blocked") return { ok: true, label: "Unblock" };
    if (t.status === "done") return { ok: true, label: "Reopen", needsReason: true };
    // dintr-o etapa inapoi pe drumul standard, la asignatul de acum
    if (t.status === "queued" && t.stageId) return { ok: true, label: "Move" };
    return { ok: false, why: `${ref} is already queued.` };
  }
  if (target.startsWith("stage:")) {
    // in etapa: o ia primul agent liber de acolo; din Review / Done = trimis inapoi / redeschis in etapa
    const id = Number(target.slice(6));
    if (t.status === "done") return { ok: true, label: "Reopen", needsReason: true };
    if (t.status === "review") return { ok: true, label: "Send back", needsReason: true };
    if (t.status === "blocked") return { ok: true, label: t.stageId === id ? "Unblock" : "Move" };
    if (t.status !== "queued") return { ok: false, why: `${ref} can't be moved there.` };
    if (t.stageId === id) return { ok: false, why: `${ref} is already here.` };
    return { ok: true, label: "Move" };
  }
  return { ok: false, why: "Tasks can't be moved there." };
}

// moveTask: cere motivul daca trebuie (askReason(label, t, opts) -> text sau null la Cancel), apoi muta.
// opts.confirmUnblock: un Unblock cere intai confirmare, cu o nota optionala pentru agent
// (cardul lasat pe coloana lui si "Move to…": o tragere pe loc din greseala nu deblocheaza nimic).
// Intoarce true daca mutarea a reusit (apelantul reincarca board-ul).
export async function moveTask(t, target, askReason, opts = {}) {
  const a = moveAction(t, target);
  if (!a.ok) { toast(a.why); return false; }
  let reason = "";
  if (a.needsReason || (a.label === "Unblock" && opts.confirmUnblock)) {
    reason = await askReason(a.label, t, { optional: !a.needsReason });
    if (reason == null) return false;
  }
  try {
    await api(`/api/tasks/${t.id}/move`, { method: "POST", body: { to: target, reason } });
    return true;
  } catch (err) {
    toast(err.message);
    return false;
  }
}
