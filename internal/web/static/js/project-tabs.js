import { el, toneOf } from "./ui.js";
import { needsCount } from "./projects.js";
import { cachedProjects, projectsStale, loadProjects } from "./project-cache.js";
import { modKey } from "./code-explorer.js";

// project-tabs.js: tab-uri cu proiectele, un rand sub bara de sus (ui.topbar), ca in browser. Un proiect deschis
// intra singur la coada (max 8); x / clic cu rotita il scoate; fiecare tab tine minte ultima adresa din proiect
// (Stats, Team...); Cmd/Ctrl+1..9 sare la tab. Totul in localStorage; se poate opri din Settings > App.

const KEY = "hobstack-tabs";             // id-urile, in ordinea tab-urilor
const ROUTES = "hobstack-tab-routes";    // id -> ultima adresa din proiect
const OFF = "hobstack-tabs-off";         // "1" = tab-urile sunt oprite
const MAX = 8;
const REFRESH_MS = 15000;
const KEY_HINT = /Mac|iPhone|iPad/.test(navigator.platform) ? "\u2318" : "Ctrl+";                // starea proiectelor (lucreaza / need you) se reciteste atat de des

const strips = new Set(); // randurile de tab-uri din barele de sus de pe pagina

function load(key, fallback) {
  try { return JSON.parse(localStorage.getItem(key) || "null") ?? fallback; } catch { return fallback; }
}

function save(key, value) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* fara localStorage: doar in pagina asta */ }
}

function readTabs() {
  const ids = load(KEY, []);
  return Array.isArray(ids) ? ids.filter(Number.isInteger) : [];
}

export function tabsEnabled() {
  try { return localStorage.getItem(OFF) !== "1"; } catch { return true; }
}

export function setTabsEnabled(on) {
  try { on ? localStorage.removeItem(OFF) : localStorage.setItem(OFF, "1"); } catch { /* ramane cum era */ }
  refreshTabs();
}

function currentProject() {
  const m = (location.hash || "").match(/^#\/p\/(\d+)/);
  return m ? Number(m[1]) : null;
}

// hrefOf: unde duce tab-ul: ultima adresa vazuta in proiect, altfel board-ul
function hrefOf(id) {
  const r = load(ROUTES, {})[id];
  return typeof r === "string" && r.startsWith(`#/p/${id}`) ? r : `#/p/${id}`;
}

// openTab: la fiecare intrare pe o adresa a proiectului (app.js route). Tine minte adresa si pune tab-ul la coada.
export function openTab(id, hash) {
  let ids = readTabs();
  if (!ids.includes(id)) ids.push(id);
  while (ids.length > MAX) {
    const drop = ids.find((x) => x !== id); // cel mai vechi care nu e curent
    ids = ids.filter((x) => x !== drop);
  }
  save(KEY, ids);
  const routes = load(ROUTES, {});
  for (const k of Object.keys(routes)) if (!ids.includes(Number(k))) delete routes[k];
  routes[id] = hash;
  save(ROUTES, routes);
  refreshTabs();
}

function closeTab(id) {
  const ids = readTabs();
  const i = ids.indexOf(id);
  const rest = ids.filter((x) => x !== id);
  save(KEY, rest);
  if (currentProject() === id) location.hash = rest.length ? hrefOf(rest[Math.min(i, rest.length - 1)]) : "#/";
  refreshTabs();
}

function draw(strip, projects) {
  const byId = new Map((projects || []).map((p) => [p.id, p]));
  const cur = currentProject();
  const tabs = tabsEnabled() ? readTabs().filter((id) => byId.has(id)).map((id) => byId.get(id)) : [];
  strip.hidden = !tabs.length;
  document.body.classList.toggle("has-ptabs", tabs.length > 0);
  strip.replaceChildren(...tabs.map((p, i) => {
    const [bg, ink] = toneOf(p.name);
    const needs = needsCount(p);
    const label = p.name + (needs ? ` (${needs} need you)` : "");
    return el("a", {
      href: hrefOf(p.id), class: "ptab" + (p.id === cur ? " active" : ""), "aria-current": p.id === cur ? "page" : null,
      title: i < 9 ? `${label} \u00b7 ${KEY_HINT}${i + 1}` : label,
      onauxclick: (e) => { if (e.button === 1) { e.preventDefault(); closeTab(p.id); } },
    },
    el("span", { class: "ptab-ini", style: `background:${bg};color:${ink}`, "aria-hidden": "true" }, (p.name || "?").trim().charAt(0).toUpperCase()),
    el("span", { class: "ptab-name" }, p.name),
    p.status === "Running" ? el("span", { class: "dot working", title: "Working" }) : null,
    needs ? el("span", { class: "ptab-needs", title: `${needs} need you` }, String(needs)) : null,
    el("button", { type: "button", class: "ptab-x", title: "Close tab", "aria-label": `Close ${p.name}`,
      onclick: (e) => { e.preventDefault(); e.stopPropagation(); closeTab(p.id); } }, "×"));
  }));
}

// tabStrip: randul de tab-uri pus in fiecare bara de sus (ui.topbar). Desenat imediat din lista din memorie.
export function tabStrip() {
  const strip = el("nav", { class: "ptabs", "aria-label": "Project tabs", hidden: true });
  strips.add(strip);
  const cached = cachedProjects();
  if (cached) draw(strip, cached);
  return strip;
}

// refreshTabs: redeseneaza tab-urile; lista proiectelor din memorie, apoi de la server daca e veche
export function refreshTabs(force) {
  for (const s of strips) if (!s.isConnected) strips.delete(s);
  const paint = (list) => strips.forEach((s) => draw(s, list));
  const cached = cachedProjects();
  if (cached) paint(cached);
  if (!tabsEnabled() || !readTabs().length) return;
  if (force || !cached || projectsStale()) loadProjects().then(paint).catch(() => { /* raman cum erau */ });
}

window.addEventListener("keydown", (e) => {
  if (!modKey(e) || e.shiftKey || e.altKey || !/^[1-9]$/.test(e.key) || !tabsEnabled()) return;
  const id = readTabs()[Number(e.key) - 1];
  if (id == null) return;
  e.preventDefault();
  location.hash = hrefOf(id);
});

setInterval(() => {
  if (document.visibilityState === "visible" && document.body.classList.contains("has-ptabs")) refreshTabs(true);
}, REFRESH_MS);
