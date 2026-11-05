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

const NS = "http://www.w3.org/2000/svg";

// svg("g", { class: "node" }, copii...) creeaza un element SVG, ca el().
// Textul se pune ca text, niciodata ca HTML.
export function svg(tag, attrs = {}, ...children) {
  const node = document.createElementNS(NS, tag);
  for (const [key, val] of Object.entries(attrs || {})) {
    if (val == null || val === false) continue;
    if (key.startsWith("on")) node.addEventListener(key.slice(2), val);
    else node.setAttribute(key, val === true ? "" : val);
  }
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false) continue;
    node.append(child instanceof Node ? child : String(child));
  }
  return node;
}

// kindOf: culoarea avatarului; amber pentru radacini (vorbesc cu utilizatorul),
// in rest o culoare stabila dupa nume (vezi toneOf).
export const kindOf = (a) => (a && a.isRoot ? "lead" : "worker");

// TONES: culorile avatarelor (fundal, text). Amber e rezervat radacinilor.
const TONES = [
  ["#8c9eff", "#111a3d"], ["#c79bf2", "#2a1640"], ["#5fd0a8", "#0b2a20"], ["#f0a3c4", "#3a1426"],
  ["#7cc4f2", "#0b2436"], ["#b5d86a", "#1f2a0b"], ["#f2967c", "#3a160b"], ["#9ad7d0", "#0e2a27"],
];
export const AMBER = ["#f2b544", "#2a1d05"];

// toneOf: aceeasi culoare pentru acelasi nume, pe orice pagina
export function toneOf(name, kind) {
  if (kind === "lead") return AMBER;
  if (kind === "user") return ["#3a414d", "#dfe3e8"];
  let h = 0;
  for (const ch of String(name || "?")) h = (h * 31 + ch.codePointAt(0)) >>> 0;
  return TONES[h % TONES.length];
}

// folderOf: folderul in care lucreaza agentul (fara folder ales: primul).
export function folderOf(project, a) {
  const folders = project.folders || [];
  return folders.find((f) => f.id === a?.folderId) || folders[0] || null;
}

// agentBody: corpul complet pentru PUT /api/agents/{id}; patch suprascrie campuri.
export function agentBody(a, patch = {}) {
  return {
    name: a.name, role: a.role, model: a.model, background: a.background,
    instructions: a.instructions, doneDef: a.doneDef, tools: a.tools,
    responsibilities: a.responsibilities, parentId: a.parentId, reviewerId: a.reviewerId,
    detached: !!a.detached,
    folderId: a.folderId ?? null,
    ...patch,
  };
}

// kind: "lead", "worker" sau "user" (vezi kindOf si toneOf)
export function avatar(name, kind, size = 32) {
  const [bg, ink] = toneOf(name, kind);
  return el("span", {
    class: `avatar ${kind === "lead" ? "lead" : "worker"}`,
    style: `--size:${size}px;--tone:${bg};--ink:${ink}`,
    "aria-hidden": "true",
  }, (name || "?").charAt(0).toUpperCase());
}

// switchButton: butonul mic de langa logo care deschide switcher-ul de proiecte (ca Cmd/Ctrl+K; vezi project-switcher.js)
const switchKeys = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘K" : "Ctrl K";
function switchButton() {
  return el("button", { type: "button", class: "ps-hint", title: `Switch project (${switchKeys})`, "aria-label": "Switch project",
    onclick: () => window.dispatchEvent(new Event("hobstack:switch")) }, switchKeys);
}

// topbarExtra: un element pus in fiecare bara de sus, inaintea butoanelor din dreapta (insigna de update, update.js)
let topbarExtra = null;
export function setTopbarExtra(fn) { topbarExtra = fn; }

// topbar: bara de sus a fiecarei pagini: logo, firimituri (crumbs: [text, href?]),
// apoi ce primeste in dreapta (butoane, tab-uri).
// crumbs poate fi si "projects" / "library" / "settings": atunci in loc de firimituri apare navigatia principala.
export function topbar(crumbs, ...right) {
  if (typeof crumbs === "string") {
    return el("header", { class: "topbar" },
      el("a", { class: "logo", href: "#/", title: "Projects", "aria-label": "Projects" }, "H"),
      switchButton(),
      el("nav", { class: "mainnav", "aria-label": "Main" },
        [["projects", "Projects", "#/"], ["library", "Library", "#/library/tools"], ["settings", "Settings", "#/settings"]].map(([id, label, href]) =>
          el("a", { class: "navlink" + (id === crumbs ? " active" : ""), href, "aria-current": id === crumbs ? "page" : null }, label))),
      el("div", { class: "topbar-right" }, right, topbarExtra && topbarExtra()));
  }
  const parts = [];
  crumbs.forEach(([text, href], i) => {
    if (i) parts.push(el("span", { class: "crumb-sep", "aria-hidden": "true" }, "/"));
    parts.push(href ? el("a", { class: "crumb", href }, text) : el("span", { class: "crumb current" }, text));
  });
  return el("header", { class: "topbar" },
    el("a", { class: "logo", href: "#/", title: "Projects", "aria-label": "Projects" }, "H"),
    switchButton(),
    el("nav", { class: "crumbs", "aria-label": "Breadcrumb" }, parts),
    el("div", { class: "topbar-right" }, right, topbarExtra && topbarExtra()));
}

export const STATUS_LABELS = {
  Running: "Running", Idle: "Idle", Paused: "Paused",
  Working: "Working", Queued: "Queued",
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
// kind: "error" (implicit) sau "ok" pentru confirmari scurte.
// action: { label, onClick } pune un buton in toast (ex. Undo); toast-ul sta atunci mai mult.
export function toast(message, kind = "error", action = null) {
  const box = document.getElementById("toast");
  box.replaceChildren(document.createTextNode(message));
  if (action) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "toast-action";
    b.textContent = action.label;
    b.addEventListener("click", () => { box.hidden = true; action.onClick(); });
    box.append(" ", b);
  }
  box.classList.toggle("ok", kind === "ok");
  box.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { box.hidden = true; }, action ? 8000 : 5000);
}

export function clock(iso) {
  return new Date(iso).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
}


export const TOOLS = [
  { id: "write", label: "Write files" },
  { id: "shell", label: "Shell commands" },
  { id: "packages", label: "Install packages (npm, composer, pip, go get)" },
  { id: "git", label: "Git: status, diff, commit, branch" },
  { id: "web", label: "Web search" },
];
