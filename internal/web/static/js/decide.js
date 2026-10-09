import { el } from "./ui.js";

// Motivele rapide pentru Send back: un click adauga textul in campul "Anything to add?".
export const QUICK_REASONS = ["Missing tests", "Doesn't match the mockup", "Needs more work", "Wrong approach"];

// Lungimea maxima a raspunsului "Other" (aceeasi limita ca pe server).
const MAX_OTHER = 300;

// collectAnswers: raspunsurile de trimis, doar pentru intrebarile fara raspuns.
// state: Map id -> { choices: [], other: "" } (ce a ales utilizatorul in pagina).
// Alegerile pastreaza ordinea optiunilor, cele necunoscute se ignora, la multi: false ramane una;
// intrebarile fara alegeri si fara other nu intra (raman deschise).
export function collectAnswers(decisions, state) {
  const out = [];
  for (const d of decisions || []) {
    if (d.answeredAt) continue;
    const s = state.get(d.id);
    if (!s) continue;
    let choices = (d.options || []).filter((o) => (s.choices || []).includes(o));
    if (!d.multi) choices = choices.slice(0, 1);
    const other = (s.other || "").trim();
    if (!choices.length && !other) continue;
    out.push({ id: d.id, choices, other });
  }
  return out;
}

// appendReason: adauga motivul la sfarsitul textului, pe linie noua.
export function appendReason(text, reason) {
  const base = (text || "").replace(/\s+$/, "");
  return base ? `${base}\n${reason}` : reason;
}

// decideBox: sectiunea Decide (intrebarile fara raspuns, cu optiuni radio / checkbox si "Other...")
// si zona de actiune (nota, motive rapide, Accept / Send back).
// - decisions: data.decisions din GET /api/tasks/{id}; cele deja raspunse nu se arata.
// - onSubmit(action, feedback, answers) -> Promise<bool>; action = "accept" | "send_back".
// - hint: textul de deasupra butoanelor ("You decide this task.").
// - note: textul initial al notei (pastrat cand caseta se reconstruieste).
// Alegerile raman in pagina pana la Accept / Send back (nu se salveaza la fiecare click).
// Intoarce { el, actions, answers(), note(), dirty() }; el e null cand nu sunt intrebari deschise.
export function decideBox({ decisions, onSubmit, hint = "", note = "" }) {
  const open = (decisions || []).filter((d) => !d.answeredAt);
  const state = new Map(); // id -> { choices, other }
  const get = (id) => {
    if (!state.has(id)) state.set(id, { choices: [], other: "" });
    return state.get(id);
  };

  const questions = open.map((d, i) => {
    const name = `decide-${d.id}`;
    const opts = (d.options || []).map((o) => {
      const input = el("input", { type: d.multi ? "checkbox" : "radio", name, value: o, onchange: () => {
        const s = get(d.id);
        if (!d.multi) s.choices = input.checked ? [o] : [];
        else if (input.checked) s.choices = [...s.choices.filter((c) => c !== o), o];
        else s.choices = s.choices.filter((c) => c !== o);
      } });
      return el("label", { class: "decide-opt" }, input, el("span", {}, o));
    });
    const other = el("input", { type: "text", class: "decide-other", maxlength: MAX_OTHER, placeholder: "Other...",
      "aria-label": `Other answer to question ${i + 1}`, oninput: () => { get(d.id).other = other.value; } });
    return el("fieldset", { class: "decide-q" },
      el("legend", {}, el("span", { class: "decide-num" }, `${i + 1}`), el("span", {}, d.question)),
      el("div", { class: "decide-meta" }, d.askedBy ? `Asked by ${d.askedBy}` : "",
        d.multi ? el("span", { class: "decide-multi" }, "Pick any") : null),
      el("div", { class: "decide-opts" }, opts), other);
  });
  const box = open.length ? el("section", { class: "decide" },
    el("div", { class: "decide-head" }, el("span", { class: "dot", style: "background:var(--amber)" }),
      el("strong", {}, "Decide"),
      el("span", { class: "hint" }, `${open.length === 1 ? "1 question" : `${open.length} questions`} waiting for your answer. Your answers go out with Accept or Send back.`)),
    questions) : null;

  // zona de actiune
  const noteInput = el("textarea", { rows: 2, class: "decide-note", placeholder: "Anything to add?", "aria-label": "Anything to add?" });
  noteInput.value = note;
  const reasons = QUICK_REASONS.map((r) => el("button", { type: "button", class: "btn small ghost decide-reason", onclick: () => {
    noteInput.value = appendReason(noteInput.value, r);
    noteInput.focus();
  } }, r));
  const accept = el("button", { type: "button", class: "btn primary decide-btn" }, "Accept");
  const back = el("button", { type: "button", class: "btn decide-btn" }, "Send back");
  const submit = (action) => async () => {
    const feedback = noteInput.value.trim();
    const answers = collectAnswers(open, state);
    // Send back fara nimic de spus: mesajul il da apelantul; aici doar focus pe camp
    if (action === "send_back" && !feedback && !answers.length) noteInput.focus();
    accept.disabled = back.disabled = true;
    const ok = await onSubmit(action, feedback, answers);
    accept.disabled = back.disabled = false;
    if (ok) noteInput.value = "";
  };
  accept.addEventListener("click", submit("accept"));
  back.addEventListener("click", submit("send_back"));
  const actions = el("div", { class: "decide-zone" },
    hint ? el("p", { class: "hint" }, hint) : null,
    noteInput,
    el("div", { class: "decide-reasons", role: "group", "aria-label": "Quick reasons to send back" }, reasons),
    el("div", { class: "decide-buttons" }, accept, back));

  return {
    el: box,
    actions,
    answers: () => collectAnswers(open, state),
    note: () => noteInput.value,
    // dirty: ceva netrimis (nota sau alegeri) -> confirmare la inchidere
    dirty: () => !!noteInput.value.trim() || collectAnswers(open, state).length > 0,
  };
}
