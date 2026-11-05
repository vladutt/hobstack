import { api } from "./api.js";
import { toast } from "./ui.js";
import { projectsView, projectFormView } from "./projects.js";
import { workspaceView, parseWorkspaceRoute } from "./workspace.js";
import { setupView } from "./setup.js";
import { newProjectView } from "./new-project.js";
import { newAgentView } from "./new-agent.js";
import { settingsView } from "./settings.js";
import { libraryView, libraryItemView } from "./library.js";
import { mcpAddView } from "./mcp-add.js";
import { skillAddView } from "./skill-add.js";
import { toggleSwitcher, touchRecent } from "./project-switcher.js";
import { modKey } from "./code-explorer.js";
import { initUpdates } from "./update.js";

// Aplicatia se numea foreman: preferintele salvate ("foreman-drawer", "foreman-category"...) trec o data
// pe cheile noi "hobstack-...", ca sa nu se piarda starea panourilor si filtrele.
try {
  for (const key of Object.keys(localStorage)) {
    if (!key.startsWith("foreman-")) continue;
    const next = "hobstack-" + key.slice("foreman-".length);
    if (localStorage.getItem(next) === null) localStorage.setItem(next, localStorage.getItem(key));
    localStorage.removeItem(key);
  }
} catch { /* fara localStorage: nimic de mutat */ }

// Rute in hash: #/, #/new, #/setup, #/setup/<id>, #/setup/folder/<proiect>/<folder>, #/p/3/edit si pagina proiectului cu tab-uri:
// #/p/3/tasks, #/p/3/team, #/p/3/team/7, #/p/3/files, #/library/tools|skills|mcp, #/library/mcp/add[?project=<id>],
// #/library/skill/add[?project=<id>], #/library/item/<id>.
// Fiecare view primeste elementul in care deseneaza si poate intoarce o
// functie de curatare (inchide stream-uri, opreste timere) sau un obiect
// { cleanup, update }. Cand ruta noua are aceeasi cheie (acelasi proiect),
// apelam update in loc sa redesenam tot: chat-ul si stream-urile raman deschise.
const routes = [
  { pattern: /^#\/?$/, view: (root) => projectsView(root) },
  { pattern: /^#\/setup\/folder\/(\d+)\/(\d+)$/, view: (root, m) => setupView(root, null, { projectId: Number(m[1]), folderId: Number(m[2]) }) },
  { pattern: /^#\/setup(?:\/([0-9a-f]+))?$/, view: (root, m) => setupView(root, m[1] || null) },
  { pattern: /^#\/library\/mcp\/add(?:\?project=(\d+))?$/, view: (root, m) => mcpAddView(root, m[1] ? Number(m[1]) : null) },
  { pattern: /^#\/library\/skill\/add(?:\?project=(\d+))?$/, view: (root, m) => skillAddView(root, m[1] ? Number(m[1]) : null) },
  { pattern: /^#\/library\/(tools|skills|mcp)$/, view: (root, m) => libraryView(root, m[1]) },
  { pattern: /^#\/library\/item\/(\d+)(?:\?tab=(\w+))?$/, view: (root, m) => libraryItemView(root, Number(m[1]), m[2]) },
  { pattern: /^#\/library\/?$/, view: () => { location.hash = "#/library/tools"; } },
  { pattern: /^#\/settings\/?$/, view: (root) => settingsView(root) },
  { pattern: /^#\/new$/, view: (root) => newProjectView(root) },
  { pattern: /^#\/p\/(\d+)\/edit$/, view: (root, m) => projectFormView(root, Number(m[1])) },
  { pattern: /^#\/p\/(\d+)\/agents\/new(?:\?parent=(\d+))?$/, view: (root, m) => newAgentView(root, Number(m[1]), m[2] ? Number(m[2]) : null) },
  {
    pattern: /^#\/p\/(\d+)(\/.*)?$/,
    key: (m) => "p:" + m[1],
    view: (root, m) => workspaceView(root, Number(m[1]), parseWorkspaceRoute(m[2])),
    update: (current, m) => current.update(parseWorkspaceRoute(m[2])),
  },
];

let current = null; // { key, cleanup, update, route }
let seq = 0; // numarul rutei curente: un view lent nu are voie sa strice ruta noua

async function route() {
  const hash = location.hash || "#/";
  const found = routes.map((r) => ({ r, m: hash.match(r.pattern) })).find((x) => x.m);
  if (!found) {
    location.hash = "#/";
    return;
  }
  const { r, m } = found;
  const inProject = hash.match(/^#\/p\/(\d+)/);
  if (inProject) touchRecent(Number(inProject[1])); // ordinea din switcher-ul de proiecte
  updateTitle(hash);
  const key = r.key ? r.key(m) : null;
  if (key && current && current.key === key && current.update) {
    current.update(m);
    return;
  }

  if (current?.cleanup) current.cleanup();
  current = null;
  const root = document.getElementById("view");
  const my = ++seq;
  const box = document.createElement("div"); // container nou pentru fiecare ruta
  root.replaceChildren(box);
  try {
    const res = await r.view(box, m);
    const cleanup = typeof res === "function" ? res : res?.cleanup;
    if (my !== seq) {
      if (cleanup) cleanup(); // ruta s-a schimbat intre timp: curatam imediat
      return;
    }
    current = {
      key,
      cleanup: cleanup || null,
      update: res?.update && r.update ? (mm) => r.update(res, mm) : null,
    };
  } catch (err) {
    toast(err.message);
  }
}

window.addEventListener("hashchange", route);

// Cmd/Ctrl+K pe orice pagina: switcher-ul de proiecte. Pe window, in capture, inregistrat inaintea modalelor:
// merge si peste explorer-ul de cod sau modala de task. Butonul din bara de sus trimite evenimentul hobstack:switch.
window.addEventListener("keydown", (e) => {
  if (modKey(e) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "k") {
    e.preventDefault();
    e.stopImmediatePropagation();
    toggleSwitcher();
  }
}, true);
window.addEventListener("hobstack:switch", toggleSwitcher);

// Titlul tab-ului din browser: cu multe tab-uri deschise, numele proiectului vine primul
// ("Probe · Stats — HobStack"), apoi sectiunea. Numele proiectelor stau intr-un cache mic;
// la fiecare intrare pe proiect se recitesc (poate fi redenumit), dar titlul se pune imediat din cache.
let appTitle = "HobStack";
const projectNames = new Map(); // id -> nume
let titleSeq = 0;

function sectionOf(hash) {
  if (/^#\/?$/.test(hash)) return "Projects";
  if (hash.startsWith("#/setup")) return "Set up with AI";
  if (hash.startsWith("#/library")) return "Library";
  if (hash.startsWith("#/settings")) return "Settings";
  if (hash === "#/new") return "New project";
  const m = hash.match(/^#\/p\/\d+(\/.*)?$/);
  if (!m) return "";
  if (/^\/edit/.test(m[1] || "")) return "Edit";
  if (/^\/agents\/new/.test(m[1] || "")) return "New agent";
  const tab = parseWorkspaceRoute(m[1]).tab;
  return tab.charAt(0).toUpperCase() + tab.slice(1);
}

function setTitle(parts) {
  document.title = [parts.filter(Boolean).join(" · "), appTitle].filter(Boolean).join(" — ");
}

function updateTitle(hash) {
  const my = ++titleSeq;
  const section = sectionOf(hash);
  const p = hash.match(/^#\/p\/(\d+)/);
  if (!p) {
    setTitle([section]);
    return;
  }
  const id = Number(p[1]);
  setTitle([projectNames.get(id), section]);
  api(`/api/projects/${id}`).then((proj) => {
    projectNames.set(id, proj.name);
    if (my === titleSeq) setTitle([proj.name, section]); // doar daca suntem tot pe ruta asta
  }).catch(() => {});
}

api("/api/info").then(({ name }) => {
  appTitle = name.charAt(0).toUpperCase() + name.slice(1);
  updateTitle(location.hash || "#/");
}).catch(() => {});

// insigna de update din bara de sus (inainte de prima desenare, ca s-o puna topbar)
initUpdates();

// prima desenare la final: route() foloseste starea titlului de mai sus (let-urile trebuie deja initializate)
route();
