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

// kindOf: culoarea avatarului; amber pentru lideri si radacini, teal in rest.
export const kindOf = (a) => (a && (a.isLead || a.isRoot) ? "lead" : "worker");

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

// kind: "lead" sau "worker" (vezi kindOf)
export function avatar(name, kind, size = 32) {
  return el("span", {
    class: `avatar ${kind === "lead" ? "lead" : "worker"}`,
    style: `--size:${size}px`,
    "aria-hidden": "true",
  }, (name || "?").charAt(0).toUpperCase());
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
// kind: "error" (implicit) sau "ok" pentru confirmari scurte
export function toast(message, kind = "error") {
  const box = document.getElementById("toast");
  box.textContent = message;
  box.classList.toggle("ok", kind === "ok");
  box.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { box.hidden = true; }, 5000);
}

export function clock(iso) {
  return new Date(iso).toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
}


export const TOOLS = [
  { id: "write", label: "Write files" },
  { id: "shell", label: "Shell commands" },
  { id: "git", label: "Git: status, diff, commit, branch" },
  { id: "web", label: "Web search" },
];
