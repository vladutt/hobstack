import { api } from "./api.js";
import { el, toast } from "./ui.js";

// ago: "5 min ago" / "2h ago" / "3d ago", pentru ultima verificare
function ago(iso) {
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)}h ago`;
  return `${Math.round(s / 86400)}d ago`;
}

// accountsSettings: conturile Claude din Settings. "Default" e CLI-ul logat (doar informativ);
// celelalte au token din `claude setup-token` si se aleg pe proiect.
export function accountsSettings() {
  const box = el("section", { class: "section accounts" });
  let list = [];
  let def = null;      // claude auth status; null = inca nu stim
  let busy = 0;        // id-ul contului verificat/salvat acum (-1 = Add)
  let replacing = 0;   // contul cu formularul "Replace token" deschis
  let adding = false;

  async function load() {
    try { list = await api("/api/accounts"); } catch (err) { toast(err.message); }
    draw();
  }
  async function loadDefault() {
    try { def = await api("/api/accounts/default"); } catch { def = { loggedIn: false }; }
    draw();
  }
  // run: o actiune pe un cont; butoanele stau oprite pana termina (Check poate dura cateva secunde)
  async function run(id, fn) {
    busy = id; draw();
    try { await fn(); } catch (err) { toast(err.message); }
    busy = 0;
    await load();
  }

  function state(a) {
    if (busy === a.id) return el("span", { class: "acct-state" }, "Checking…");
    if (a.lastError) return el("span", { class: "acct-state bad", title: a.lastError }, a.lastError);
    if (a.checkedAt) return el("span", { class: "acct-state ok" }, `Works · checked ${ago(a.checkedAt)}`);
    return el("span", { class: "acct-state" }, "Not checked yet");
  }

  function tokenInput() {
    return el("input", { type: "password", autocomplete: "off", spellcheck: "false", placeholder: "sk-ant-oat01-…", "aria-label": "Token" });
  }

  function replaceForm(a) {
    const token = tokenInput();
    const save = () => run(a.id, async () => {
      await api(`/api/accounts/${a.id}`, { method: "PUT", body: { token: token.value.trim() } });
      replacing = 0;
    });
    token.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); save(); } });
    queueMicrotask(() => token.focus());
    return el("div", { class: "acct-form" }, token,
      el("button", { type: "button", class: "btn small primary", disabled: !!busy, onclick: save }, "Save and check"),
      el("button", { type: "button", class: "btn small", onclick: () => { replacing = 0; draw(); } }, "Cancel"));
  }

  function row(a) {
    const used = a.projects ? `Used by ${a.projects} ${a.projects === 1 ? "project" : "projects"}` : "Not used";
    return el("div", { class: "acct-row" },
      el("div", { class: "acct-main" },
        el("strong", {}, a.name),
        el("code", { class: "acct-hint" }, a.tokenHint ? `••••${a.tokenHint}` : "••••"),
        state(a)),
      el("div", { class: "acct-actions" },
        el("span", { class: "hint" }, used),
        el("button", { type: "button", class: "btn small", disabled: !!busy,
          onclick: () => run(a.id, () => api(`/api/accounts/${a.id}/check`, { method: "POST" })) }, "Check"),
        el("button", { type: "button", class: "btn small", disabled: !!busy,
          onclick: () => { replacing = replacing === a.id ? 0 : a.id; draw(); } }, "Replace token"),
        el("button", { type: "button", class: "link-btn danger", disabled: !!busy || a.projects > 0,
          title: a.projects ? "Move its projects to another account first" : null,
          onclick: () => { if (confirm(`Delete the Claude account ${a.name}?`)) run(a.id, () => api(`/api/accounts/${a.id}`, { method: "DELETE" })); } }, "Delete")),
      replacing === a.id ? replaceForm(a) : null);
  }

  function defaultRow() {
    let text = "…";
    let cls = "acct-state";
    if (def && def.loggedIn) {
      text = [def.email || def.authMethod, def.subscriptionType].filter(Boolean).join(" · ");
      cls += " ok";
    } else if (def) {
      text = "Not logged in: run claude in a terminal and log in";
      cls += " bad";
    }
    return el("div", { class: "acct-row" },
      el("div", { class: "acct-main" }, el("strong", {}, "Default"), el("span", { class: "hint" }, "the logged-in Claude CLI"),
        el("span", { class: cls }, text)));
  }

  function addForm() {
    const name = el("input", { type: "text", maxlength: 40, placeholder: "e.g. Work", "aria-label": "Account name" });
    const token = tokenInput();
    const save = () => run(-1, async () => {
      await api("/api/accounts", { method: "POST", body: { name: name.value.trim(), token: token.value.trim() } });
      adding = false;
    });
    queueMicrotask(() => name.focus());
    return el("div", { class: "acct-add" },
      el("p", { class: "hint" }, "In a terminal, run ", el("code", {}, "claude setup-token"),
        " and sign in with the other account in the browser. Paste the token it prints. It is stored in Foreman's database on this machine and never shown again in the UI. Agents of projects on this account run with it in their environment, so an agent with Shell (or a Library tool) could read it."),
      el("div", { class: "acct-form" }, name, token,
        el("button", { type: "button", class: "btn small primary", disabled: !!busy, onclick: save }, busy === -1 ? "Checking…" : "Add and check"),
        el("button", { type: "button", class: "btn small", onclick: () => { adding = false; draw(); } }, "Cancel")));
  }

  function draw() {
    box.replaceChildren(
      el("h2", {}, "Claude accounts"),
      el("p", { class: "hint" }, "Each project runs on Default or on one of these accounts (pick it in the project's Instructions). Use this to keep work projects on the company subscription."),
      el("div", { class: "acct-table" }, defaultRow(), list.map(row)),
      adding ? addForm() : el("div", { class: "cmd-add" },
        el("button", { type: "button", class: "btn small", disabled: !!busy, onclick: () => { adding = true; draw(); } }, "+ Add account")));
  }

  draw();
  load();
  loadDefault();
  return box;
}
