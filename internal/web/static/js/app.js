import { api } from "./api.js";
import { toast } from "./ui.js";
import { projectsView, projectFormView } from "./projects.js";
import { agentView } from "./agent.js";

// Rute in hash: #/, #/new, #/p/3/edit, #/p/3/agent/new, #/p/3/agent/7.
// Fiecare view primeste elementul in care deseneaza si poate intoarce o
// functie de curatare (inchide stream-uri, opreste timere).
const routes = [
  [/^#\/?$/, (root) => projectsView(root)],
  [/^#\/new$/, (root) => projectFormView(root, null)],
  [/^#\/p\/(\d+)\/edit$/, (root, m) => projectFormView(root, Number(m[1]))],
  [/^#\/p\/(\d+)\/agent\/new$/, (root, m) => agentView(root, Number(m[1]), null)],
  [/^#\/p\/(\d+)\/agent\/(\d+)$/, (root, m) => agentView(root, Number(m[1]), Number(m[2]))],
];

let cleanup = null;

async function route() {
  if (cleanup) {
    cleanup();
    cleanup = null;
  }
  const root = document.getElementById("view");
  root.replaceChildren();
  const hash = location.hash || "#/";
  for (const [pattern, view] of routes) {
    const m = hash.match(pattern);
    if (!m) continue;
    try {
      cleanup = (await view(root, m)) || null;
    } catch (err) {
      toast(err.message);
    }
    return;
  }
  location.hash = "#/";
}

window.addEventListener("hashchange", route);
route();

api("/api/info").then(({ name }) => {
  const title = name.charAt(0).toUpperCase() + name.slice(1);
  document.title = title;
  document.getElementById("app-name").textContent = title;
}).catch(() => {});
