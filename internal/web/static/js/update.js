import { api } from "./api.js";
import { el, toast, setTopbarExtra } from "./ui.js";

// update.js: versiunea noua de pe git. Insigna din bara de sus (cand exista commit-uri noi sau un update e
// in curs), fereastra cu lista commit-urilor si "Update and restart", sectiunea Version din Settings.
// Dupa restart serverul raspunde cu alta revizie si pagina se reincarca singura.

let status = null;      // ultimul GET /api/update; null = inca nu stim (sau update-ul nu e disponibil)
let timer = 0;
let lost = false;       // serverul nu raspunde (se reporneste)
let startedFrom = "";   // revizia de dinainte de update, ca sa stim cand ruleaza cea noua
let overlay = null;     // fereastra deschisa
const listeners = new Set(); // cine redeseneaza la fiecare stare noua (insignele, Settings, fereastra)

const BUSY = ["updating", "waiting", "restarting"];

function busy(s) { return !!s && BUSY.includes(s.state); }

function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

function changed() { for (const fn of listeners) fn(); }

// poll: des cat ruleaza un update sau serverul se reporneste, altfel la 10 minute
function schedule() {
  clearTimeout(timer);
  timer = setTimeout(load, busy(status) || lost ? 1500 : 10 * 60 * 1000);
}

async function load() {
  try {
    const s = await api("/api/update");
    if (lost || (startedFrom && s.current !== startedFrom)) {
      // serverul a revenit dupa restart: pagina noua vine cu JS-ul versiunii noi
      location.reload();
      return;
    }
    status = s;
  } catch (err) {
    if (!err.status && (startedFrom || busy(status))) {
      lost = true; // serverul nu raspunde: se reporneste
    } else if (err.status === 503) {
      status = null; // aplicatia ruleaza fara update
    }
  }
  changed();
  schedule();
}

async function act(path) {
  try {
    status = await api(path, { method: "POST" });
    if (path === "/api/update") startedFrom = status.current || "?";
  } catch (err) {
    toast(err.message);
    if (err.data && err.data.state) status = err.data;
  }
  changed();
  schedule();
}

let checking = false;
async function checkNow() {
  checking = true; changed();
  try { status = await api("/api/update/check", { method: "POST" }); } catch (err) { toast(err.message); }
  checking = false; changed();
  schedule();
}

// label: textul scurt pentru insigna; "" = nimic de aratat
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
  return s.behind > 0 ? `Update · ${s.behind}` : "";
}

// badge: insigna din bara de sus; se redeseneaza singura cat e in pagina
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

function commitRow(c) {
  return el("li", { class: "upd-commit" },
    el("code", {}, c.hash),
    el("span", { class: "upd-subject" }, c.subject),
    el("span", { class: "hint" }, `${c.author} · ${ago(c.when)}`));
}

// body: continutul comun al ferestrei si al sectiunii din Settings
function body(s, inModal) {
  if (!s) return [el("p", { class: "hint" }, lost ? "HobStack is restarting…" : "Loading…")];
  const parts = [];
  const built = s.currentTime ? ` · built from a commit of ${new Date(s.currentTime).toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" })}` : "";
  parts.push(el("p", { class: "upd-version" },
    "Running ", el("code", {}, s.current || "unknown"), s.modified ? " (with local changes)" : "", built));
  if (!s.canUpdate) {
    parts.push(el("p", { class: "hint" }, s.reason));
    return parts;
  }
  const where = [`Source: ${s.source}`];
  if (s.upstream) where.push(`tracks ${s.upstream}`);
  if (s.checkedAt) where.push(`checked ${ago(s.checkedAt)}`);
  parts.push(el("p", { class: "hint" }, where.join(" · ")));
  if (s.checkError) parts.push(el("p", { class: "upd-error" }, `Check failed: ${s.checkError}`));

  if (s.behind > 0) {
    parts.push(el("p", { class: "upd-new" }, `${s.behind} new commit${s.behind === 1 ? "" : "s"}`));
    if (inModal) {
      const more = s.behind - s.commits.length;
      parts.push(el("ul", { class: "upd-commits" }, s.commits.map(commitRow),
        more > 0 && el("li", { class: "hint" }, `and ${more} older`)));
    }
  } else if (s.checkedAt && s.state === "idle") {
    parts.push(el("p", { class: "upd-ok" }, "You have the latest version."));
  }

  // starea update-ului in curs
  if (lost || s.state === "restarting") parts.push(el("p", { class: "upd-step" }, "Restarting… the page reloads by itself."));
  else if (s.state === "updating") parts.push(el("p", { class: "upd-step" }, `${s.step || "Updating"}…`));
  else if (s.state === "waiting") parts.push(el("p", { class: "upd-step" },
    `Installed${s.target ? " " + s.target : ""}. Waiting for ${s.waiting} running agent${s.waiting === 1 ? "" : "s"} to finish; no new runs start meanwhile.`));
  if ((s.state === "failed" || s.state === "installed") && s.error) {
    parts.push(el("pre", { class: s.state === "failed" ? "upd-error" : "upd-note" }, s.error));
  }

  const canStart = !busy(s) && !lost && (s.behind > 0 || s.state === "installed" || s.state === "failed");
  parts.push(el("div", { class: "upd-actions" },
    s.state === "waiting"
      ? el("button", { type: "button", class: "btn small", onclick: () => act("/api/update/cancel") }, "Cancel restart")
      : el("button", { type: "button", class: "btn small primary", disabled: !canStart, onclick: () => act("/api/update") },
        s.state === "installed" ? "Restart now" : "Update and restart"),
    el("button", { type: "button", class: "btn small", disabled: checking || busy(s) || lost, onclick: checkNow },
      checking ? "Checking…" : "Check now"),
    !inModal && s.behind > 0 && el("button", { type: "button", class: "btn small ghost", onclick: openUpdate }, "See what's new")));
  if (inModal && canStart) {
    parts.push(el("p", { class: "hint" },
      "HobStack pulls the code, builds it and keeps the old binary as hobstack.prev. Then it waits for running agents to finish, " +
      "backs up the database to ~/.hobstack/backups and restarts."));
  }
  return parts;
}

// openUpdate: fereastra cu commit-urile noi si butoanele
export function openUpdate() {
  if (overlay) return;
  const box = el("div", { class: "upd-box", role: "dialog", "aria-modal": "true", "aria-label": "HobStack update" });
  overlay = el("div", { class: "upd-overlay", onclick: (e) => { if (e.target === overlay) close(); } }, box);
  const onKey = (e) => { if (e.key === "Escape") { e.stopPropagation(); close(); } };
  const draw = () => box.replaceChildren(
    el("div", { class: "upd-head" },
      el("h2", {}, "HobStack update"),
      el("button", { type: "button", class: "btn small ghost", "aria-label": "Close", onclick: close }, "✕")),
    ...body(status, true));
  function close() {
    listeners.delete(draw);
    document.removeEventListener("keydown", onKey, true);
    overlay.remove();
    overlay = null;
  }
  listeners.add(draw);
  document.addEventListener("keydown", onKey, true);
  draw();
  document.body.append(overlay);
  box.querySelector("button")?.focus();
}

// versionSettings: sectiunea Version din Settings
export function versionSettings() {
  const box = el("section", { class: "section upd-settings" });
  const draw = () => {
    if (!box.isConnected && box.dataset.drawn) { listeners.delete(draw); return; }
    box.dataset.drawn = "1";
    box.replaceChildren(el("h2", {}, "Version"), ...body(status, false));
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
