import { api } from "./api.js";
import { el, avatar } from "./ui.js";
import { isMac } from "./code-explorer.js";
import { needsCount } from "./projects.js";

// Switcher-ul de proiecte (Cmd/Ctrl+K pe orice pagina): lista proiectelor, cele deschise recent primele.
// Ordinea "recent" sta in localStorage (doar id-uri, max 20), actualizata la fiecare intrare intr-un proiect.

const RECENT_KEY = "hobstack-recent-projects";
const MAX_RECENT = 20;
const TABS = ["tasks", "team", "files", "stats"]; // tab-urile care se pastreaza la schimbarea proiectului

function readRecent() {
  try {
    const ids = JSON.parse(localStorage.getItem(RECENT_KEY) || "[]");
    return Array.isArray(ids) ? ids.filter((x) => Number.isInteger(x)) : [];
  } catch { return []; }
}

// touchRecent: proiectul id trece primul in lista recentelor
export function touchRecent(id) {
  const ids = [id, ...readRecent().filter((x) => x !== id)].slice(0, MAX_RECENT);
  try { localStorage.setItem(RECENT_KEY, JSON.stringify(ids)); } catch { /* fara localStorage: doar ordinea se pierde */ }
}

// currentRoute: proiectul si tab-ul din hash (#/p/3/stats -> { id: 3, tab: "stats" }); in afara proiectelor id e null
function currentRoute() {
  const m = (location.hash || "").match(/^#\/p\/(\d+)(?:\/([\w-]+))?/);
  if (!m) return { id: null, tab: null };
  return { id: Number(m[1]), tab: TABS.includes(m[2]) ? m[2] : null };
}

// rankProjects: fara text, recentele (in ordinea lor), apoi favoritele, apoi restul; proiectul curent la final.
// Cu text: numele incepe cu textul, numele il contine, categoria il contine; la egalitate ordinea de mai sus.
export function rankProjects(projects, q, recent, currentId) {
  const base = (p) => {
    const r = recent.indexOf(p.id);
    return (p.id === currentId ? 1e6 : 0) + (r >= 0 ? r : (p.favorite ? 1000 : 2000));
  };
  const needle = q.trim().toLowerCase();
  const tier = (p) => {
    if (!needle) return 0;
    const name = p.name.toLowerCase();
    if (name.startsWith(needle)) return 0;
    if (name.includes(needle)) return 1;
    if ((p.category || "").toLowerCase().includes(needle)) return 2;
    return -1;
  };
  return projects
    .map((p, i) => ({ p, t: tier(p), b: base(p), i }))
    .filter((x) => x.t >= 0)
    .sort((a, b) => a.t - b.t || a.b - b.b || a.i - b.i)
    .map((x) => x.p);
}

let openNow = null; // switcher-ul deschis (cel mult unul)

// toggleSwitcher: deschide switcher-ul, sau il inchide daca e deja deschis
export function toggleSwitcher() {
  if (openNow) { openNow.close(); return; }
  openNow = openSwitcher(() => { openNow = null; });
}

function openSwitcher(onClose) {
  const { id: currentId, tab } = currentRoute();
  const recent = readRecent();
  let projects = null; // null = inca se incarca
  let shown = [];
  let active = 0;

  const input = el("input", {
    type: "text", class: "ps-input", placeholder: "Go to project…", autocomplete: "off", spellcheck: "false",
    role: "combobox", "aria-expanded": "true", "aria-controls": "ps-list", "aria-label": "Go to project",
  });
  const list = el("ul", { class: "ps-list", id: "ps-list", role: "listbox", "aria-label": "Projects" });
  const box = el("div", { class: "ps-box", role: "dialog", "aria-modal": "true", "aria-label": "Switch project" },
    el("div", { class: "ps-top" }, input, el("kbd", { class: "ps-kbd" }, "esc")),
    list,
    el("div", { class: "ps-foot" },
      el("span", {}, el("kbd", {}, "↑↓"), " choose"),
      el("span", {}, el("kbd", {}, "↵"), " open"),
      el("span", {}, el("kbd", {}, isMac ? "⌘↵" : "Ctrl+↵"), " new tab")));
  const overlay = el("div", { class: "ps-overlay" }, box);
  let downOnOverlay = false;
  overlay.addEventListener("mousedown", (e) => { downOnOverlay = e.target === overlay; });
  overlay.addEventListener("click", (e) => { if (downOnOverlay && e.target === overlay) close(); });

  // hrefFor: proiectul ales, pe acelasi tab ca acum (Stats -> Stats); altfel pagina implicita a proiectului
  const hrefFor = (p) => `#/p/${p.id}` + (tab ? "/" + tab : "");

  function go(p, newTab) {
    if (!p) return;
    const href = hrefFor(p);
    close();
    if (newTab) window.open(location.pathname + location.search + href, "_blank");
    else location.hash = href;
  }

  function row(p, i) {
    const needs = needsCount(p);
    const state = p.status === "Paused" ? "paused" : p.status === "Running" ? "running" : "idle";
    const cur = p.id === currentId;
    return el("li", {
      id: `ps-opt-${p.id}`, role: "option", "aria-selected": String(i === active),
      class: "ps-row" + (i === active ? " on" : "") + (cur ? " current" : ""),
      onmousemove: () => { if (active !== i) { active = i; paint(); } },
      onclick: (e) => go(p, e.metaKey || e.ctrlKey),
    },
      avatar(p.name, "worker", 26),
      el("span", { class: "ps-name" }, p.name),
      p.favorite ? el("span", { class: "ps-star", title: "Favorite" }, "★") : null,
      p.category ? el("span", { class: "ps-cat" }, p.category) : null,
      el("span", { class: "ps-meta" },
        needs ? el("span", { class: "badge-needs-you" }, `${needs} ${needs === 1 ? "needs" : "need"} you`) : null,
        cur ? el("span", { class: "ps-here" }, "current") : el("span", { class: `p-state ${state}` }, p.status)));
  }

  function paint() {
    if (projects === null) { list.replaceChildren(el("li", { class: "ps-empty" }, "Loading projects…")); return; }
    if (!shown.length) {
      list.replaceChildren(el("li", { class: "ps-empty" }, projects.length ? "No projects match." : "No projects yet."));
      input.removeAttribute("aria-activedescendant");
      return;
    }
    list.replaceChildren(...shown.map(row));
    input.setAttribute("aria-activedescendant", `ps-opt-${shown[active].id}`);
    list.children[active]?.scrollIntoView({ block: "nearest" });
  }

  function refilter() {
    if (projects === null) return paint();
    shown = rankProjects(projects, input.value, recent, currentId);
    active = 0;
    paint();
  }

  input.addEventListener("input", refilter);
  // tastele se trateaza aici, la tinta, si nu mai urca: Esc / Enter / sagetile nu ajung la modalele de dedesubt
  input.addEventListener("keydown", (e) => {
    e.stopPropagation();
    if (e.key === "Escape") { e.preventDefault(); close(); }
    else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      if (!shown.length) return;
      active = (active + (e.key === "ArrowDown" ? 1 : -1) + shown.length) % shown.length;
      paint();
    } else if (e.key === "Enter") {
      e.preventDefault();
      go(shown[active], e.metaKey || e.ctrlKey);
    } else if (e.key === "Tab") e.preventDefault(); // focusul ramane in camp
  });

  const before = document.activeElement;
  let closed = false;
  function close() {
    if (closed) return;
    closed = true;
    overlay.remove();
    if (before?.isConnected) before.focus?.();
    onClose();
  }

  document.body.append(overlay);
  input.focus();
  paint();
  api("/api/projects").then((res) => {
    if (closed) return;
    projects = res || [];
    refilter();
  }).catch((err) => {
    if (closed) return;
    list.replaceChildren(el("li", { class: "ps-empty error" }, `Could not load projects: ${err.message}`));
  });

  return { close };
}
