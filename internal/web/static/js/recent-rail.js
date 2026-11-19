import { el, toneOf } from "./ui.js";
import { readRecent } from "./project-switcher.js";
import { needsCount } from "./projects.js";
import { cachedProjects, projectsStale, loadProjects } from "./project-cache.js";

// Bara cu proiectele recente: doar in fereastra proprie (window.hobstackApp), in stanga pe mijloc, cel mult 5 proiecte
// deschise recent (ordinea din switcher-ul Cmd/Ctrl+K), fara cel curent. Clic = proiectul. Se poate opri din
// Settings > App (alegerea in localStorage, ca sertarul de chat).

const KEY = "hobstack-recent-rail";
const MAX = 5;

let nav = null;

export function railEnabled() {
  try { return localStorage.getItem(KEY) !== "off"; } catch { return true; }
}

export function setRailEnabled(on) {
  try { localStorage.setItem(KEY, on ? "on" : "off"); } catch { /* fara localStorage: ramane pornita */ }
  updateRail();
}

function currentProject() {
  const m = (location.hash || "").match(/^#\/p\/(\d+)/);
  return m ? Number(m[1]) : null;
}

function hide() {
  nav?.remove();
  nav = null;
  document.body.classList.remove("has-rail");
}

function draw(projects) {
  if (!window.hobstackApp || !railEnabled()) { hide(); return; }
  const byId = new Map((projects || []).map((p) => [p.id, p]));
  const cur = currentProject();
  const items = readRecent().filter((id) => id !== cur && byId.has(id)).slice(0, MAX).map((id) => byId.get(id));
  if (!items.length) { hide(); return; }
  if (!nav) {
    nav = el("nav", { class: "recent-rail", "aria-label": "Recent projects" });
    document.body.append(nav);
    document.body.classList.add("has-rail");
  }
  nav.replaceChildren(...items.map((p) => {
    const [bg, ink] = toneOf(p.name);
    const waiting = needsCount(p) > 0;
    return el("a", {
      href: `#/p/${p.id}`, class: "rr-item", style: `--tone:${bg};--ink:${ink}`,
      "aria-label": waiting ? `${p.name} (needs you)` : p.name,
    },
    (p.name || "?").trim().charAt(0).toUpperCase(),
    waiting ? el("span", { class: "rr-dot", "aria-hidden": "true" }) : null,
    el("span", { class: "rr-name", "aria-hidden": "true" }, p.name));
  }));
}

// updateRail: la fiecare schimbare de pagina (app.js). Lista din memorie imediat, apoi de la server daca e veche.
export function updateRail() {
  if (!window.hobstackApp || !railEnabled()) { hide(); return; }
  const cached = cachedProjects();
  if (cached) draw(cached);
  if (!cached || projectsStale()) loadProjects().then(draw).catch(() => { /* ramane ce era */ });
}
