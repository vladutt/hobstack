// Brainstorm: modala ideii (textul complet al unui tile) si ajutoarele comune tile-ului si modalei.
// Tile-urile sunt in brainstorm.js; aici: openIdea, syncIdea (dupa evenimentul SSE), plainText, ideaTags.
import { el } from "./ui.js";
import { renderMarkdown } from "./md.js";

// isCombined: cardul e o idee combinata (are surse)
export const isCombined = (c) => c.sourceA != null;

// plainText: textul simplu dintr-un markdown, pentru extrasul de pe tile (fara #, *, ```, linkuri).
// Randurile se lipesc cu un spatiu, punctele de lista consecutive cu " · ", ca sa nu se amestece cuvintele.
export function plainText(md) {
  const text = String(md || "")
    .replace(/\r\n?/g, "\n")
    .replace(/^\s*```.*$/gm, "")               // gardurile blocurilor de cod (continutul ramane)
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")   // imagini
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")    // linkuri: doar textul
    .replace(/^[ \t]{0,3}#{1,6}[ \t]+(.*?)[ \t:.]*$/gm, "$1:") // titluri: "## Plan" -> "Plan:"
    .replace(/^[ \t]*>[ \t]?/gm, "")            // citate
    .replace(/^[ \t]*([-*+]|\d+[.)])[ \t]+/gm, "\u0001") // marcajele listelor -> semn (pentru " · ")
    .replace(/(\*\*|__|\*|_|~~|`)/g, "");       // accent, cod inline
  let out = "";
  let prevItem = false;
  for (const raw of text.split("\n")) {
    const item = raw.startsWith("\u0001");
    const line = raw.replace(/\u0001/g, "").replace(/\s+/g, " ").trim();
    if (!line) continue;
    if (out) out += item && prevItem ? " · " : " ";
    out += line;
    prevItem = item;
  }
  return out;
}

// ideaTags: etichetele unui card terminat (Picked, Wild, Merged, Combined, Edited); onJump(id) duce la alt card
export function ideaTags(c, onJump) {
  const out = [];
  if (c.status !== "done") return out;
  if (c.picked) out.push(el("span", { class: "bs-tag picked" }, "Picked"));
  if (c.kind === "wild") out.push(el("span", { class: "bs-tag wild" }, "Wild"));
  if (c.kind === "question") out.push(el("span", { class: "bs-tag question" }, c.answer ? "Answered" : "Question"));
  if (isCombined(c)) out.push(el("span", { class: "bs-tag combined" }, "Combined"));
  if (c.edited) out.push(el("span", { class: "bs-tag edited", title: "You edited this idea" }, "Edited"));
  if (c.mergedInto != null) {
    out.push(el("button", {
      type: "button", class: "bs-tag merged", title: "Show the combined idea",
      onclick: (e) => { e.stopPropagation(); onJump?.(c.mergedInto); },
    }, "Merged"));
  }
  return out;
}

// stateText: textul starii pentru cardurile neterminate
export function stateText(c) {
  switch (c.status) {
    case "waiting": return (c.busy ? `Waiting, busy on ${c.busy}` : "Waiting") + (c.priority ? " · runs next" : "");
    case "running": return "Thinking…";
    case "stopped": return c.error || "Stopped.";
    case "failed": return c.error || "Failed.";
    case "skipped": return c.error || "Skipped.";
  }
  return "";
}

let cur = null; // modala deschisa: { overlay, onKey, ids, id, ctx, editing, other, sig }

// openIdea(c, siblings, ctx): modala cu textul complet si actiunile.
//   siblings: cardurile din acelasi rand (sagetile stanga / dreapta trec intre ele)
//   ctx: { face(c, size), role(c), busy(c, action), act(c, action, body), edit(c, title, text) -> bool, jump(id) }
//   opts.other: pune cursorul in campul "Other" al intrebarii (butonul Other de pe tile)
export function openIdea(c, siblings, ctx, opts = {}) {
  closeIdea();
  const list = (siblings && siblings.length ? siblings : [c]);
  const box = el("div", { class: "bs-idea", tabindex: "-1" });
  const overlay = el("div", {
    class: "modal-overlay bs-idea-overlay", role: "dialog", "aria-modal": "true", "aria-label": "Idea",
    onclick: (e) => { if (e.target === overlay) closeIdea(); },
  }, box);
  // in faza de captura: Esc inchide doar modala ideii; sagetile nu se fura din campurile de text
  const onKey = (e) => {
    if (!cur) return;
    const typing = e.target.closest?.("input, textarea, select, [contenteditable]");
    if (e.key === "Escape") {
      if (cur.editing) { cur.editing = null; paint(); } else closeIdea();
    } else if ((e.key === "ArrowLeft" || e.key === "ArrowRight") && !typing && !cur.editing) {
      if (!go(e.key === "ArrowLeft" ? -1 : 1)) return;
    } else return;
    e.preventDefault();
    e.stopPropagation();
  };
  document.addEventListener("keydown", onKey, true);
  document.body.append(overlay);
  cur = { overlay, box, onKey, ctx, cards: new Map(list.map((x) => [x.id, x])), ids: list.map((x) => x.id),
    id: c.id, editing: null, other: null, sig: "" };
  cur.cards.set(c.id, c);
  if (!cur.ids.includes(c.id)) cur.ids.unshift(c.id);
  paint();
  const other = opts.other && box.querySelector(".bs-other textarea");
  (other || box).focus();
}

export function closeIdea() {
  if (!cur) return;
  document.removeEventListener("keydown", cur.onKey, true);
  cur.overlay.remove();
  cur = null;
}

// syncIdea(lookup): dupa reincarcarea subiectului; lookup(id) -> cardul nou (sau nimic daca a disparut).
// Se redeseneaza doar daca cardul afisat s-a schimbat si nu e editat chiar acum.
export function syncIdea(lookup) {
  if (!cur) return;
  for (const id of cur.ids) {
    const x = lookup(id);
    if (x) cur.cards.set(id, x); else cur.cards.delete(id);
  }
  cur.ids = cur.ids.filter((id) => cur.cards.has(id));
  if (!cur.cards.has(cur.id)) { closeIdea(); return; }
  if (cur.editing) return;
  if (JSON.stringify(cur.cards.get(cur.id)) !== cur.sig) paint();
}

// go: trece la cardul vecin; intoarce false daca nu are unde
function go(d) {
  if (cur.ids.length < 2) return false;
  const i = cur.ids.indexOf(cur.id);
  const j = i + d;
  if (j < 0 || j >= cur.ids.length) return false;
  cur.id = cur.ids[j];
  cur.other = null;
  paint();
  return true;
}

function paint() {
  const c = cur.cards.get(cur.id);
  const { ctx } = cur;
  cur.sig = JSON.stringify(c);
  const i = cur.ids.indexOf(c.id);
  const many = cur.ids.length > 1 && !cur.editing; // in editare nu se trece la alt card
  const nav = (d, label, sym) => el("button", {
    type: "button", class: "bs-idea-nav", "aria-label": label, title: label,
    disabled: (d < 0 ? i <= 0 : i >= cur.ids.length - 1) || null, onclick: () => go(d),
  }, sym);
  const role = ctx.role?.(c);
  const head = el("div", { class: "bs-idea-head" },
    ctx.face(c, 26),
    el("span", { class: "bs-idea-who" }, el("b", {}, c.agentName || "Agent"), role ? el("span", {}, ` · ${role}`) : null),
    el("span", { class: "grow" }),
    many ? el("span", { class: "bs-idea-count" }, `${i + 1} / ${cur.ids.length}`) : null,
    many ? nav(-1, "Previous idea", "‹") : null,
    many ? nav(1, "Next idea", "›") : null,
    el("button", { type: "button", class: "modal-close", "aria-label": "Close", title: "Close (Esc)", onclick: closeIdea }, "×"));

  const jump = (id) => { closeIdea(); ctx.jump(id); };
  const body = el("div", { class: "bs-idea-body" });
  const tags = ideaTags(c, jump);

  if (cur.editing) {
    const ed = cur.editing;
    const title = el("input", { type: "text", class: "bs-idea-edit-title", maxlength: 200, value: ed.title, "aria-label": "Title" });
    const text = el("textarea", { class: "bs-idea-edit-text", rows: 12, value: ed.text, "aria-label": "Text" });
    title.addEventListener("input", () => { ed.title = title.value; });
    text.addEventListener("input", () => { ed.text = text.value; });
    const save = el("button", { type: "button", class: "btn primary small" }, "Save");
    save.addEventListener("click", async () => {
      if (!ed.title.trim() || !ed.text.trim()) return;
      save.disabled = true;
      const ok = await ctx.edit(c, ed.title.trim(), ed.text.trim());
      if (!cur) return;
      if (ok) { cur.editing = null; cur.sig = ""; paint(); } else save.disabled = false;
    });
    body.append(title, text,
      el("div", { class: "bs-idea-actions" },
        el("span", { class: "grow" }),
        el("button", { type: "button", class: "btn ghost small", onclick: () => { cur.editing = null; paint(); } }, "Cancel"),
        save));
    cur.box.replaceChildren(head, body);
    title.focus();
    return;
  }

  if (tags.length) body.append(el("div", { class: "bs-idea-tags" }, tags));
  body.append(el("h2", { class: "bs-idea-title" }, c.title || (c.status === "done" ? "Idea" : stateText(c))));
  if (c.combinedFrom?.length) {
    const links = [];
    c.combinedFrom.forEach((s, k) => {
      if (k) links.push(" + ");
      links.push(el("button", { type: "button", class: "link-btn", onclick: () => jump(s.id) }, s.title || "Idea"));
    });
    body.append(el("p", { class: "bs-idea-meta" }, el("span", {}, "Combined from: "), links));
  }
  if (c.status !== "done") {
    body.append(el("p", { class: "bs-state " + c.status }, stateText(c)));
  } else {
    if (c.text && !(c.kind === "question" && c.text === c.title)) body.append(renderMarkdown(c.text));
    if (c.basis) body.append(el("p", { class: "bs-idea-meta" }, el("span", {}, "Based on: "), c.basis));
    if (c.kind === "question") body.append(question(c));
  }

  const b = (action, label, cls = "btn small") => el("button", {
    type: "button", class: cls, disabled: ctx.busy(c, action) || null,
    onclick: () => ctx.act(c, action),
  }, label);
  const acts = [];
  switch (c.status) {
    case "waiting":
      if (!c.priority) acts.push(b("run-now", "Run now"));
      acts.push(b("stop", "Stop", "btn small ghost"));
      break;
    case "running":
      acts.push(b("stop", "Stop", "btn small ghost"));
      break;
    case "stopped":
    case "failed":
      acts.push(b("retry", "Retry"));
      break;
    case "done":
      if (c.kind === "question") break;
      acts.push(c.picked ? b("unpick", "Unpick", "btn small active") : b("pick", "Pick", "btn small primary"));
      if (isCombined(c)) {
        acts.push(el("button", {
          type: "button", class: "btn small ghost",
          onclick: () => { cur.editing = { title: c.title || "", text: c.text || "" }; paint(); },
        }, "Edit"));
      } else {
        acts.push(b("another", "Another idea", "btn small ghost"));
      }
  }
  cur.box.replaceChildren(head, body, ...(acts.length ? [el("div", { class: "bs-idea-actions" }, acts)] : []));
}

// question: optiunile intrebarii si campul Other; dupa raspuns doar raspunsul
function question(c) {
  if (c.answer) return el("p", { class: "bs-answer" }, el("span", {}, "You answered: "), c.answer);
  const { ctx } = cur;
  const answer = (text) => {
    text = String(text || "").trim();
    if (text) ctx.act(c, "answer", { answer: text });
  };
  if (cur.other == null) cur.other = "";
  const box = el("textarea", { rows: 2, placeholder: "Other: write your own answer", value: cur.other, "aria-label": "Your answer" });
  box.addEventListener("input", () => { if (cur) cur.other = box.value; });
  box.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); answer(box.value); }
  });
  return el("div", { class: "bs-idea-question" },
    el("div", { class: "bs-options" }, c.options.map((o) => el("button", { type: "button", class: "btn small", onclick: () => answer(o) }, o))),
    el("div", { class: "bs-other" }, box,
      el("button", { type: "button", class: "btn small primary", onclick: () => answer(box.value) }, "Answer")));
}
