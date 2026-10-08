import { el, avatar, kindOf } from "./ui.js";

// agentPicker: asignatul din coloana modalei de task, ca buton; la click se
// deschide o lista cu cautare (scrii o parte din nume sau rol, sagetile aleg,
// Enter confirma, Esc inchide).
// - onPick(agent): utilizatorul a ales alt agent decat cel curent.
// - onClose(): lista s-a inchis (modala redeseneaza coloana amanata cat a fost deschisa).
// Intoarce { el, update({ label, agent, choices, currentId }), isOpen(), close() }.
export function agentPicker({ onPick, onClose }) {
  let choices = [];       // agentii care pot lua task-ul
  let currentId = 0;      // asignatul curent (bifat in lista)
  let shown = [];         // agentii ramasi dupa filtru
  let active = 0;         // randul evidentiat cu sagetile
  let open = false;

  const button = el("button", { type: "button", class: "side-value picker-btn", "aria-haspopup": "listbox", "aria-expanded": "false", onclick: toggle });
  const input = el("input", { type: "text", class: "picker-search", placeholder: "Search agents...", autocomplete: "off", "aria-label": "Search agents" });
  const list = el("div", { class: "picker-list", role: "listbox" });
  const panel = el("div", { class: "picker-panel", hidden: true }, input, list);
  const root = el("div", { class: "picker" }, button, panel);

  input.addEventListener("input", () => { active = 0; draw(); });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      // Esc inchide doar lista, nu si modala (asculta pe document)
      e.preventDefault();
      e.stopPropagation();
      close(true);
    } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!shown.length) return;
      active = (active + (e.key === "ArrowDown" ? 1 : shown.length - 1)) % shown.length;
      draw();
    } else if (e.key === "Enter") {
      e.preventDefault();
      if (shown[active]) pick(shown[active]);
    }
  });
  // click in afara inchide lista
  const onDocDown = (e) => { if (!root.contains(e.target)) close(false); };
  // derularea in spate sau redimensionarea ar lasa lista in aer: o inchidem
  const onScroll = (e) => { if (!panel.contains(e.target)) close(false); };

  function toggle() {
    if (open) { close(true); return; }
    open = true;
    input.value = "";
    active = Math.max(0, choices.findIndex((a) => a.id === currentId));
    panel.hidden = false;
    place();
    button.setAttribute("aria-expanded", "true");
    draw();
    input.focus();
    document.addEventListener("mousedown", onDocDown);
    document.addEventListener("scroll", onScroll, true);
    addEventListener("resize", onScroll);
  }

  // place: lista e position: fixed (coloana modalei taie si deruleaza ce iese din ea);
  // o punem sub buton, cat mai la stanga incape in fereastra
  function place() {
    const r = button.getBoundingClientRect();
    const w = panel.offsetWidth;
    panel.style.left = `${Math.max(8, Math.min(r.left - 4, innerWidth - w - 12))}px`;
    const below = r.bottom + 6;
    panel.style.top = below + panel.offsetHeight > innerHeight - 8 ? `${Math.max(8, r.top - 6 - panel.offsetHeight)}px` : `${below}px`;
  }

  function close(refocus) {
    if (!open) return;
    open = false;
    panel.hidden = true;
    button.setAttribute("aria-expanded", "false");
    document.removeEventListener("mousedown", onDocDown);
    document.removeEventListener("scroll", onScroll, true);
    removeEventListener("resize", onScroll);
    if (refocus) button.focus();
    onClose?.();
  }

  function pick(a) {
    close(true);
    if (a.id !== currentId) onPick(a);
  }

  // draw: randurile care se potrivesc cu textul cautat (nume sau rol)
  function draw() {
    const q = input.value.trim().toLowerCase();
    shown = q ? choices.filter((a) => a.name.toLowerCase().includes(q) || (a.role || "").toLowerCase().includes(q)) : choices;
    if (active >= shown.length) active = 0;
    if (!shown.length) {
      list.replaceChildren(el("p", { class: "picker-empty" }, "No matching agent."));
      return;
    }
    list.replaceChildren(...shown.map((a, i) => {
      const row = el("button", { type: "button", role: "option", class: "picker-option" + (i === active ? " active" : ""),
        "aria-selected": String(a.id === currentId), onclick: () => pick(a) },
        avatar(a.name, kindOf(a), 24),
        el("span", { class: "picker-name" }, a.name, a.role ? el("span", { class: "picker-role" }, a.role) : null),
        a.id === currentId ? el("span", { class: "picker-check", "aria-hidden": "true" }, "✓") : null);
      row.addEventListener("mousemove", () => { if (active !== i) { active = i; draw(); } });
      if (i === active) queueMicrotask(() => row.scrollIntoView({ block: "nearest" }));
      return row;
    }));
  }

  return {
    el: root,
    isOpen: () => open,
    close: () => close(false),
    // update: ce arata butonul (label = numele sau textul de stare) si lista de agenti
    update({ label, agent, choices: next, currentId: cur }) {
      choices = next;
      currentId = cur;
      // replaceChildren nativ scrie null ca text: filtram avatarul lipsa
      button.replaceChildren(...[
        agent ? avatar(agent.name, kindOf(agent), 26) : null,
        el("span", { class: agent ? "" : "hint" }, label),
        el("span", { class: "picker-caret", "aria-hidden": "true" }),
      ].filter(Boolean));
      button.setAttribute("aria-label", `Assignee: ${label}. Change`);
    },
  };
}
