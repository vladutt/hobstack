import { api } from "./api.js";

// viewing.js: spune serverului ce proiect are deschis pagina si daca are focus. Cat te uiti la un proiect,
// HobStack nu trimite notificari de sistem pentru el. Serverul uita raportul dupa 45 s, deci il repetam la 20 s.
const client = Math.random().toString(36).slice(2) + Date.now().toString(36);
let last = "";
let lastAt = 0;

function projectOf(hash) {
  const m = /^#\/p\/(\d+)/.exec(hash);
  return m ? Number(m[1]) : 0;
}

function report(force) {
  const project = projectOf(location.hash);
  const focused = document.visibilityState === "visible" && document.hasFocus();
  const key = `${project}:${focused}`;
  // fara schimbare trimitem doar ca sa nu expire raportul (si doar cat are focus)
  if (!force && key === last && (!focused || Date.now() - lastAt < 20000)) return;
  last = key;
  lastAt = Date.now();
  api("/api/app/viewing", { method: "POST", body: { client, project, focused } }).catch(() => { /* nu conteaza */ });
}

export function initViewing() {
  for (const ev of ["focus", "blur", "hashchange", "pageshow"]) window.addEventListener(ev, () => report(false));
  document.addEventListener("visibilitychange", () => report(false));
  // pagina inchisa: ultimul raport spune "fara focus"
  window.addEventListener("pagehide", () => {
    try {
      navigator.sendBeacon?.("/api/app/viewing", new Blob([JSON.stringify({ client, project: 0, focused: false })], { type: "application/json" }));
    } catch { /* expira singur */ }
  });
  setInterval(() => report(false), 5000);
  report(true);
}
