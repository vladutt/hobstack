import { api } from "./api.js";

// Lista proiectelor tinuta in memorie (pentru switcher-ul Cmd/Ctrl+K): se arata imediat si se cere din nou doar daca s-a
// schimbat ceva la proiecte (orice cerere care nu e GET pe /api/projects... sau /api/setup..., vezi api.js) sau daca are
// peste un minut (numarul "need you" si echipa se schimba si fara noi). Pagina Proiecte o actualizeaza cand se incarca.

const MAX_AGE = 60_000;

let list = null;
let at = 0;
let changed = false;
let inflight = null;

// cachedProjects: lista din memorie (null = inca nu s-a incarcat niciodata)
export function cachedProjects() { return list; }

// rememberProjects: o lista proaspata, adusa de altcineva (pagina Proiecte)
export function rememberProjects(projects) {
  list = projects || [];
  at = Date.now();
  changed = false;
}

// projectsChanged: un proiect a fost creat / sters / schimbat; urmatoarea deschidere aduce lista din nou
export function projectsChanged() { changed = true; }

export function projectsStale() { return !list || changed || Date.now() - at > MAX_AGE; }

// loadProjects: aduce lista de la server (o singura cerere odata)
export function loadProjects() {
  if (!inflight) {
    changed = false; // o schimbare in timpul cererii o marcheaza din nou
    inflight = api("/api/projects")
      .then((res) => { list = res || []; at = Date.now(); return list; })
      .finally(() => { inflight = null; });
  }
  return inflight;
}
