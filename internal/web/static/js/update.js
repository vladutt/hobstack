import { api } from "./api.js";
import { el, toast, setTopbarExtra } from "./ui.js";
import { renderMarkdown } from "./md.js";

// update.js: versiunile HobStack de pe GitHub Releases. Cand exista o versiune noua, la intrarea in aplicatie
// apare o fereastra cu versiunea si notele ei (Update and restart / Later / Skip this version); insigna din
// bara de sus si Settings > Version raman si dupa. Dupa restart pagina se reincarca singura cand serverul
// raspunde cu alta versiune.

const SKIP_KEY = "hobstack-skip-version";      // localStorage: versiunea sarita
const LATER_KEY = "hobstack-later-version";    // sessionStorage: "Later" pana la urmatoarea sesiune
const UPDATING_KEY = "hobstack-updating-to";   // localStorage: versiunea instalata, pentru mesajul de dupa restart

let status = null;      // ultimul GET /api/update; null = inca nu stim (sau update-ul nu e disponibil)
let timer = 0;
let lost = false;       // serverul nu raspunde (se reporneste)
let startedFrom = "";   // versiunea de dinainte de update, ca sa stim cand ruleaza cea noua
let overlay = null;     // fereastra deschisa
let autoShown = false;  // fereastra s-a deschis singura o data in pagina asta
let checking = false;
const listeners = new Set(); // cine redeseneaza la fiecare stare noua (insignele, Settings, fereastra)

const BUSY = ["updating", "waiting", "restarting"];
function busy(s) { return !!s && BUSY.includes(s.state); }

function store(kind) { try { return kind === "session" ? sessionStorage : localStorage; } catch { return null; } }
function getKey(kind, key) { try { return store(kind)?.getItem(key) || ""; } catch { return ""; } }
function setKey(kind, key, value) { try { value ? store(kind)?.setItem(key, value) : store(kind)?.removeItem(key); } catch { /* fara storage */ } }

function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}
function day(iso) { return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" }); }
function vname(v) { return v === "dev" ? "a development build" : `v${v}`; }

// fill: replaceChildren fara golurile din conditii (replaceChildren ar scrie null ca text)
function fill(box, ...children) { box.replaceChildren(...children.filter((c) => c != null && c !== false && c !== "")); }

function changed() { for (const fn of [...listeners]) fn(); }

// poll: des cat ruleaza un update, serverul se reporneste sau prima verificare (la pornire) n-a terminat; altfel la 10 minute
function schedule() {
  clearTimeout(timer);
  const firstCheck = status && !status.checkedAt && !status.checkError;
  timer = setTimeout(load, busy(status) || lost || checking || firstCheck || (status && status.checking) ? 1500 : 10 * 60 * 1000);
}

async function load() {
  try {
    const s = await api("/api/update");
    if (lost || (startedFrom && s.current !== startedFrom)) {
      location.reload(); // serverul a revenit dupa restart: pagina noua vine cu JS-ul versiunii noi
      return;
    }
    status = s;
  } catch (err) {
    if (!err.status && (startedFrom || busy(status))) lost = true; // serverul nu raspunde: se reporneste
    else if (err.status === 503) status = null;
  }
  afterUpdate();
  maybeAutoOpen();
  changed();
  schedule();
}

// mesajul de dupa restart: versiunea pornita e cea instalata
function afterUpdate() {
  const to = getKey("local", UPDATING_KEY);
  if (!to || !status) return;
  setKey("local", UPDATING_KEY, "");
  if (status.current === to) toast(`HobStack updated to v${to}`, "ok");
}

// fereastra se deschide singura o data pe pagina, pentru o versiune noua nesarita si fara "Later" in sesiunea asta
function maybeAutoOpen() {
  const s = status;
  if (autoShown || overlay || !s || !s.available || busy(s)) return;
  if (getKey("local", SKIP_KEY) === s.latest || getKey("session", LATER_KEY) === s.latest) return;
  autoShown = true;
  openUpdate();
}

async function act(path) {
  try {
    const before = status && status.current;
    status = await api(path, { method: "POST" });
    if (path === "/api/update") {
      startedFrom = before || "?";
      setKey("local", UPDATING_KEY, status.target || "");
    }
  } catch (err) {
    toast(err.message);
  }
  changed();
  schedule();
}

async function checkNow() {
  checking = true; changed();
  try { status = await api("/api/update/check", { method: "POST" }); } catch (err) { toast(err.message); }
  checking = false;
  changed();
  schedule();
}

// label: textul insignei din bara de sus; "" = nimic de aratat
function label(s) {
  if (lost) return "Restarting…";
  if (!s) return "";
  switch (s.state) {
    case "updating": return s.step ? `${s.step}…` : "Updating…";
    case "waiting": return `Waiting for ${s.waiting} agent${s.waiting === 1 ? "" : "s"}`;
    case "restarting": return "Restarting…";
    case "failed": return "Update failed";
    case "installed": return "Restart to update";
  }
  return s.available ? `v${s.latest} available` : "";
}

// badge: insigna din bara de sus; se scoate singura din listeners cand bara dispare din pagina
function badge() {
  const b = el("button", { type: "button", class: "upd-badge", hidden: true, onclick: openUpdate });
  const draw = () => {
    if (!b.isConnected && b.dataset.drawn) { listeners.delete(draw); return; }
    b.dataset.drawn = "1";
    const text = label(status);
    b.hidden = !text;
    b.textContent = text;
    b.classList.toggle("bad", !!status && status.state === "failed");
    b.classList.toggle("busy", lost || busy(status));
    b.title = text ? "HobStack update" : "";
  };
  listeners.add(draw);
  draw();
  return b;
}

// progress: starea update-ului in curs sau a celui terminat prost
function progress(s) {
  const parts = [];
  if (lost || s.state === "restarting") parts.push(el("p", { class: "upd-step" }, "Restarting… the page reloads by itself."));
  else if (s.state === "updating") parts.push(el("p", { class: "upd-step" }, `${s.step || "Updating"}…`));
  else if (s.state === "waiting") parts.push(el("p", { class: "upd-step" },
    `v${s.target} is installed. Waiting for ${s.waiting} running agent${s.waiting === 1 ? "" : "s"} to finish; no new runs start meanwhile.`));
  if ((s.state === "failed" || s.state === "installed") && s.error) {
    parts.push(el("pre", { class: s.state === "failed" ? "upd-error" : "upd-note" }, s.error));
  }
  return parts;
}

// actions: butoanele principale (Update / Restart now / Cancel restart)
function mainAction(s) {
  if (s.state === "waiting") {
    return el("button", { type: "button", class: "btn small", onclick: () => act("/api/update/cancel") }, "Cancel restart");
  }
  const offer = s.available || s.state === "installed" || (s.dev && s.latest);
  if (!offer || !s.canUpdate) return null;
  const text = s.state === "installed" ? "Restart now" : s.dev ? `Install v${s.latest}` : "Update and restart";
  return el("button", { type: "button", class: "btn small primary", disabled: busy(s) || lost, onclick: () => act("/api/update") }, text);
}

// openUpdate: fereastra cu versiunea noua si notele ei
export function openUpdate() {
  if (overlay) return;
  const box = el("div", { class: "upd-box", role: "dialog", "aria-modal": "true", "aria-label": "HobStack update" });
  overlay = el("div", { class: "upd-overlay", onclick: (e) => { if (e.target === overlay) later(); } }, box);
  const onKey = (e) => { if (e.key === "Escape") { e.stopPropagation(); later(); } };
  function close() {
    listeners.delete(draw);
    document.removeEventListener("keydown", onKey, true);
    overlay.remove();
    overlay = null;
  }
  // Later: nu mai apare singura in sesiunea asta (insigna ramane)
  function later() {
    if (status && status.available && !busy(status)) setKey("session", LATER_KEY, status.latest);
    close();
  }
  function skip() {
    setKey("local", SKIP_KEY, status.latest);
    close();
  }
  function draw() {
    const s = status;
    if (!s) { box.replaceChildren(el("p", { class: "hint" }, lost ? "HobStack is restarting…" : "Loading…")); return; }
    const title = s.available || (s.dev && s.latest) ? `HobStack v${s.latest} is available` : s.state === "idle" ? "HobStack is up to date" : "HobStack update";
    const meta = [`You have ${vname(s.current)}`];
    if (s.published) meta.push(`released ${day(s.published)}`);
    const showNotes = !!s.latest;
    const working = busy(s) || lost;
    fill(box,
      el("div", { class: "upd-head" },
        el("h2", {}, title),
        el("button", { type: "button", class: "btn small ghost", "aria-label": "Close", onclick: later }, "✕")),
      el("p", { class: "hint" }, meta.join(" · "), s.url ? [" · ", el("a", { href: s.url, target: "_blank", rel: "noopener" }, "Release on GitHub")] : null),
      showNotes && s.notes ? el("div", { class: "upd-notes" }, renderMarkdown(s.notes)) : null,
      !s.canUpdate && s.reason ? el("p", { class: "upd-error" }, s.reason) : null,
      ...progress(s),
      el("div", { class: "upd-actions" },
        mainAction(s),
        !working && s.available && s.state === "idle" && el("button", { type: "button", class: "btn small", onclick: later }, "Later"),
        !working && s.available && s.state === "idle" && el("button", { type: "button", class: "btn small ghost", onclick: skip }, "Skip this version")),
      !working && s.canUpdate && (s.available || s.dev) && el("p", { class: "hint" },
        "HobStack downloads the new version, checks it and keeps the old binary as hobstack.prev. Then it waits for running agents " +
        "to finish, backs up the database to ~/.hobstack/backups and restarts."));
  }
  listeners.add(draw);
  document.addEventListener("keydown", onKey, true);
  draw();
  document.body.append(overlay);
  box.querySelector(".btn.primary, button")?.focus();
}

// versionSettings: sectiunea Version din Settings
export function versionSettings() {
  const box = el("section", { class: "section upd-settings" });
  const draw = () => {
    if (!box.isConnected && box.dataset.drawn) { listeners.delete(draw); return; }
    box.dataset.drawn = "1";
    const s = status;
    if (!s) { box.replaceChildren(el("h2", {}, "Version"), el("p", { class: "hint" }, lost ? "HobStack is restarting…" : "Loading…")); return; }
    const running = s.dev
      ? ["Development build", s.revision ? [" ", el("code", {}, s.revision)] : null, el("span", { class: "hint" }, " · built without a version, so it does not offer updates by itself")]
      : ["Running ", el("code", {}, `v${s.current}`)];
    const line = [];
    if (s.latest) line.push(`Latest release: v${s.latest}${s.published ? ` (${day(s.published)})` : ""}`);
    else if (s.checkedAt) line.push("No releases published yet");
    if (s.checkedAt) line.push(`checked ${ago(s.checkedAt)}`);
    let verdict = null;
    if (s.available) verdict = el("p", { class: "upd-new" }, `v${s.latest} is available.`);
    else if (s.latest && !s.dev && s.state === "idle") verdict = el("p", { class: "upd-ok" }, "You have the latest version.");
    fill(box,
      el("h2", {}, "Version"),
      el("p", { class: "upd-version" }, running),
      line.length ? el("p", { class: "hint" }, line.join(" · ")) : null,
      s.checkError ? el("p", { class: "upd-error" }, `Check failed: ${s.checkError}`) : null,
      verdict,
      s.latest && !s.canUpdate && s.reason ? el("p", { class: "hint" }, s.reason) : null,
      ...progress(s),
      el("div", { class: "upd-actions" },
        mainAction(s),
        el("button", { type: "button", class: "btn small", disabled: checking || busy(s) || lost, onclick: checkNow }, checking ? "Checking…" : "Check now"),
        s.latest && s.notes && el("button", { type: "button", class: "btn small ghost", onclick: openUpdate }, "Release notes")));
  };
  listeners.add(draw);
  draw();
  return box;
}

// pornit o data din app.js
export function initUpdates() {
  setTopbarExtra(badge);
  load();
}
