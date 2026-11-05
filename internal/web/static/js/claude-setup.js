import { api } from "./api.js";
import { el, toast } from "./ui.js";

// claude-setup.js: pagina de start cand Claude Code lipseste sau nu e logat. Trei pasi: Install (installerul
// oficial, cu output-ul live), PATH (ca sa mearga claude si in terminal; HobStack nu are nevoie) si Log in
// (deschide un terminal cu `claude auth login`). Se inchide singura cand claude e instalat si logat.

const SKIP_LOGIN_KEY = "hobstack-skip-claude-login"; // sessionStorage: "Continue without logging in"

let overlay = null;
let status = null;
let timer = 0;
let busy = "";      // actiunea in curs: "install" | "path" | "login" | "check"
let pathMsg = "";

function skippedLogin() { try { return sessionStorage.getItem(SKIP_LOGIN_KEY) === "1"; } catch { return false; } }
function skipLogin() { try { sessionStorage.setItem(SKIP_LOGIN_KEY, "1"); } catch { /* fara storage */ } }

function needed(s) { return !!s && (!s.found || (!s.loggedIn && !skippedLogin())); }

async function load() {
  try { status = await api("/api/claude"); } catch (err) { if (err.status === 503) return; status = null; }
  if (!overlay && needed(status)) open();
  if (overlay) {
    draw();
    clearTimeout(timer);
    // installerul: des; login-ul facut in terminal: la cateva secunde
    timer = setTimeout(load, status && status.install.running ? 1000 : 3000);
  }
}

async function act(kind, path) {
  busy = kind; draw();
  try {
    const res = await api(path, { method: "POST" });
    if (kind === "path") { pathMsg = res.message; status = res.status; }
    else if (res) status = res;
    if (kind === "login") toast("A terminal window opened. Log in there, then come back here.", "ok");
  } catch (err) {
    toast(err.message);
  }
  busy = "";
  load();
}

function step(n, title, done, body) {
  return el("li", { class: "cs-step" + (done ? " done" : "") },
    el("span", { class: "cs-num", "aria-hidden": "true" }, done ? "✓" : String(n)),
    el("div", { class: "cs-body" }, el("h3", {}, title), body));
}

function installStep(s) {
  if (s.found) {
    return step(1, "Claude Code is installed", true,
      el("p", { class: "hint" }, `${s.version || "claude"} · ${s.path}`));
  }
  const inst = s.install;
  const log = inst.log.length ? el("pre", { class: "cs-log" }, inst.log.join("\n")) : null;
  if (log) queueMicrotask(() => { log.scrollTop = log.scrollHeight; });
  return step(1, "Install Claude Code", false, [
    el("p", {}, "HobStack runs your agents with Claude Code (the claude command), and it is not on this computer yet."),
    el("p", { class: "hint" }, "The official installer runs:"),
    el("code", { class: "cs-cmd" }, s.installCommand.join(" ")),
    log,
    inst.error ? el("p", { class: "upd-error" }, inst.error) : null,
    inst.done && !inst.error && !s.found ? el("p", { class: "upd-error" }, "The installer finished, but HobStack cannot find claude. Check the output above.") : null,
    el("div", { class: "upd-actions" },
      el("button", { type: "button", class: "btn small primary", disabled: inst.running || !!busy, onclick: () => act("install", "/api/claude/install") },
        inst.running ? "Installing…" : inst.done ? "Try again" : "Install Claude Code"),
      el("button", { type: "button", class: "btn small", disabled: inst.running || !!busy, onclick: () => act("check", "/api/claude/check") },
        busy === "check" ? "Checking…" : "I installed it, check again")),
  ]);
}

function pathStep(s) {
  if (!s.found) return step(2, "Use claude in a terminal (optional)", false, el("p", { class: "hint" }, "After installing."));
  if (s.inPath) {
    return step(2, "claude works in a terminal", true, pathMsg ? el("p", { class: "hint" }, pathMsg) : el("p", { class: "hint" }, `${s.dir} is in your PATH.`));
  }
  return step(2, "Use claude in a terminal (optional)", false, [
    el("p", {}, `HobStack finds claude by itself, but ${s.dir} is not in your PATH, so typing claude in a terminal does not work yet.`),
    el("p", { class: "hint" }, s.os === "windows"
      ? "Adds the folder at the end of your user PATH; nothing else changes."
      : "Adds one line to your shell startup file (~/.zshrc or ~/.bash_profile)."),
    el("div", { class: "upd-actions" },
      el("button", { type: "button", class: "btn small", disabled: !!busy, onclick: () => act("path", "/api/claude/path") },
        busy === "path" ? "Adding…" : "Add to PATH")),
  ]);
}

function loginStep(s) {
  if (!s.found) return step(3, "Log in", false, el("p", { class: "hint" }, "After installing."));
  if (s.loggedIn) return step(3, "Logged in", true, el("p", { class: "hint" }, s.email || "Claude Code is logged in."));
  return step(3, "Log in", false, [
    el("p", {}, "Log in to Claude Code with your Claude account. A terminal window opens and the browser asks you to sign in; this page notices when you are done."),
    el("div", { class: "upd-actions" },
      el("button", { type: "button", class: "btn small primary", disabled: !!busy, onclick: () => act("login", "/api/claude/login") },
        busy === "login" ? "Opening…" : "Open login"),
      el("button", { type: "button", class: "btn small", disabled: !!busy, onclick: () => act("check", "/api/claude/check") },
        busy === "check" ? "Checking…" : "Check again")),
    el("p", { class: "hint" }, "Or run claude auth login in a terminal yourself."),
  ]);
}

function draw() {
  if (!overlay) return;
  const box = overlay.firstChild;
  const s = status;
  if (!s) { box.replaceChildren(el("p", { class: "hint" }, "Loading…")); return; }
  const ready = s.found && s.loggedIn;
  const parts = [
    el("div", { class: "upd-head" }, el("h2", {}, ready ? "Claude Code is ready" : "Set up Claude Code")),
    el("ol", { class: "cs-steps" }, installStep(s), pathStep(s), loginStep(s)),
    el("div", { class: "upd-actions cs-foot" },
      ready
        ? el("button", { type: "button", class: "btn small primary", onclick: close }, "Continue")
        : s.found && el("button", { type: "button", class: "btn small ghost", onclick: () => { skipLogin(); close(); } },
          "Continue without logging in")),
  ];
  box.replaceChildren(...parts.filter(Boolean));
}

function open() {
  overlay = el("div", { class: "upd-overlay cs-overlay" },
    el("div", { class: "upd-box cs-box", role: "dialog", "aria-modal": "true", "aria-label": "Set up Claude Code" }));
  document.body.append(overlay);
}

function close() {
  clearTimeout(timer);
  overlay?.remove();
  overlay = null;
}

// pornit o data din app.js
export function initClaudeSetup() { load(); }
