import { api } from "./api.js";
import { toast } from "./ui.js";
import { projectsView, projectFormView } from "./projects.js";
import { agentView } from "./agent.js";
import { workspaceView } from "./workspace.js";

// Rute in hash: #/, #/new, #/p/3/edit, #/p/3/agent/new, #/p/3/agent/7.
// Fiecare view primeste elementul in care deseneaza si poate intoarce o
// functie de curatare (inchide stream-uri, opreste timere).
const routes = [
  [/^#\/?$/, (root) => projectsView(root)],
  [/^#\/new$/, (root) => projectFormView(root, null)],
  [/^#\/p\/(\d+)\/edit$/, (root, m) => projectFormView(root, Number(m[1]))],
  [/^#\/p\/(\d+)$/, (root, m) => workspaceView(root, Number(m[1]))],
  [/^#\/p\/(\d+)\/agent\/new$/, (root, m) => agentView(root, Number(m[1]), null)],
  [/^#\/p\/(\d+)\/agent\/(\d+)$/, (root, m) => agentView(root, Number(m[1]), Number(m[2]))],
];

let cleanup = null;
let seq = 0; // numarul rutei curente: un view lent nu are voie sa strice ruta noua

async function route() {
  if (cleanup) {
    cleanup();
    cleanup = null;
  }
  const root = document.getElementById("view");
  const my = ++seq;
  const box = document.createElement("div"); // container nou pentru fiecare ruta
  root.replaceChildren(box);
  const hash = location.hash || "#/";
  for (const [pattern, view] of routes) {
    const m = hash.match(pattern);
    if (!m) continue;
    try {
      const c = await view(box, m);
      if (my !== seq) {
        if (c) c(); // ruta s-a schimbat intre timp: curatam imediat
        return;
      }
      cleanup = c || null;
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
