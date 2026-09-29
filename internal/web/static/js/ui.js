// Utilitare mici pentru construit DOM fara framework.

// el("div", { class: "x", onclick: fn }, copii...) creeaza un element.
// Textul se pune mereu ca text, nu ca HTML, deci continutul venit de la
// agenti nu poate injecta cod in pagina.
export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  let value;
  for (const [key, val] of Object.entries(attrs || {})) {
    if (val == null || val === false) continue;
    if (key === "class") node.className = val;
    else if (key === "value") value = val; // se seteaza dupa copii (conteaza la <select>)
    else if (key.startsWith("on")) node.addEventListener(key.slice(2), val);
    else node.setAttribute(key, val === true ? "" : val);
  }
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false) continue;
    node.append(child instanceof Node ? child : String(child));
  }
  if (value !== undefined) node.value = value;
  return node;
}

export function avatar(name, position, size = 32) {
  return el("span", {
    class: `avatar ${position === "lead" ? "lead" : "worker"}`,
    style: `--size:${size}px`,
    "aria-hidden": "true",
  }, (name || "?").charAt(0).toUpperCase());
}

export const STATUS_LABELS = {
  Running: "Ruleaza", Idle: "Inactiv", Paused: "Pe pauza",
  Working: "Lucreaza", Queued: "In asteptare",
};

export function pill(status) {
  return el("span", { class: `pill pill-${status.toLowerCase()}` }, STATUS_LABELS[status] || status);
}

let fieldSeq = 0;
export function field(label, input, hint) {
  if (!input.id) input.id = `f-${++fieldSeq}`;
  return el("div", { class: "field" },
    el("label", { for: input.id }, label),
    input,
    hint && el("span", { class: "hint" }, hint));
}

let toastTimer;
export function toast(message) {
  const box = document.getElementById("toast");
  box.textContent = message;
  box.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { box.hidden = true; }, 5000);
}

export function clock(iso) {
  return new Date(iso).toLocaleTimeString("ro-RO", { hour: "2-digit", minute: "2-digit" });
}

export const MODELS = ["opus", "sonnet", "haiku"];

export const TOOLS = [
  { id: "write", label: "Scrie fisiere" },
  { id: "shell", label: "Comenzi shell" },
  { id: "git", label: "Git: status, diff, commit, branch" },
  { id: "web", label: "Cautare web" },
];
