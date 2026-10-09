import { api } from "./api.js";
import { el } from "./ui.js";

// Categoriile proiectelor: filtrul tinut minte pe pagina Proiecte, lista categoriilor
// folosite si campul Category cu lista vizibila (formularul de proiect, wizard-ul).

// ALL = toate proiectele; nu poate fi o categorie reala
export const ALL = "\u0000all";
const KEY = "foreman-category";

export function savedCategory() {
  try { return localStorage.getItem(KEY) ?? ALL; } catch { return ALL; }
}

export function saveCategory(value) {
  try { localStorage.setItem(KEY, value); } catch { /* fara memorie: ramane doar pe pagina */ }
}

// loadCategories: categoriile folosite de proiecte, cu numarul lor, sortate dupa nume
export async function loadCategories() {
  try {
    const counts = new Map();
    for (const p of await api("/api/projects")) {
      if (p.category) counts.set(p.category, (counts.get(p.category) || 0) + 1);
    }
    return [...counts].map(([name, count]) => ({ name, count }))
      .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
  } catch {
    return []; // fara sugestii; campul merge si asa
  }
}

// categoryInput: campul Category cu lista categoriilor existente, deschisa la focus si
// filtrata cand scrii; "Create "X"" pentru una noua. onChange(valoare) e optional.
export function categoryInput(value, categories, onChange) {
  const input = el("input", {
    type: "text", value, placeholder: "e.g. Work", maxlength: 40, autocomplete: "off",
    role: "combobox", "aria-autocomplete": "list", "aria-expanded": "false",
  });
  const list = el("ul", { class: "combo-list", role: "listbox", hidden: true });
  let items = [];
  let active = -1;

  function open() {
    const q = input.value.trim();
    const ql = q.toLowerCase();
    items = categories.filter((c) => !ql || c.name.toLowerCase().includes(ql))
      .map((c) => ({ value: c.name, label: c.name, count: c.count }));
    if (q && !categories.some((c) => c.name.toLowerCase() === ql)) items.push({ value: q, label: `Create "${q}"`, create: true });
    active = -1;
    draw();
  }
  function draw() {
    list.replaceChildren(...items.map((it, i) => el("li", {
      role: "option", "aria-selected": String(i === active),
      class: "combo-opt" + (i === active ? " active" : "") + (it.create ? " create" : ""),
      // mousedown, nu click: inainte de blur, care inchide lista
      onmousedown: (e) => { e.preventDefault(); pick(it); },
    }, el("span", {}, it.label), it.count != null ? el("span", { class: "cat-count" }, it.count) : null)));
    list.hidden = !items.length;
    input.setAttribute("aria-expanded", String(!list.hidden));
  }
  function close() {
    list.hidden = true;
    input.setAttribute("aria-expanded", "false");
  }
  function pick(it) {
    input.value = it.value;
    close();
    onChange?.(it.value);
  }

  input.addEventListener("focus", open);
  input.addEventListener("input", () => { open(); onChange?.(input.value); });
  input.addEventListener("blur", close);
  input.addEventListener("keydown", (e) => {
    if (list.hidden) {
      if (e.key === "ArrowDown") { e.preventDefault(); open(); }
      return;
    }
    if (e.key === "ArrowDown") { e.preventDefault(); active = Math.min(items.length - 1, active + 1); draw(); }
    else if (e.key === "ArrowUp") { e.preventDefault(); active = Math.max(0, active - 1); draw(); }
    else if (e.key === "Enter" && active >= 0) { e.preventDefault(); pick(items[active]); }
    else if (e.key === "Escape") { e.preventDefault(); close(); }
  });
  return { el: el("div", { class: "combo" }, input, list), input };
}
