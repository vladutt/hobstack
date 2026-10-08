import { api } from "./api.js";
import { el, toast } from "./ui.js";

const MODES = [["allow", "Allow"], ["ask", "Ask"], ["block", "Block"]];

// modeSwitch: comutatorul Allow / Ask / Block; onChange(mode)
function modeSwitch(current, onChange, { inherit = null, disabled = false } = {}) {
  const opts = inherit ? [["", `Inherit (${inherit})`], ...MODES] : MODES;
  return el("div", { class: "cmd-seg", role: "group" }, opts.map(([v, label]) =>
    el("button", { type: "button", class: "seg-btn" + (v === current ? " active" : ""), "aria-pressed": String(v === current), disabled,
      onclick: () => onChange(v) }, label)));
}

// globalCommands: lista din Settings (salvare imediata a intregii liste).
export function globalCommands() {
  const box = el("section", { class: "section cmd-rules" });
  let rules = [];
  let busy = false; // salvare in curs: butoanele sunt oprite ca sa nu se suprapuna doua salvari
  async function load() {
    try { rules = await api("/api/settings/commands"); draw(); } catch (err) { toast(err.message); }
  }
  async function save(next) {
    if (busy) return;
    busy = true; draw();
    try {
      await api("/api/settings/commands", { method: "PUT", body: next });
      busy = false;
      await load(); // lista normalizata de server
    } catch (err) { busy = false; toast(err.message); draw(); }
  }
  function draw() {
    const input = el("input", { type: "text", maxlength: 80, placeholder: "e.g. docker", "aria-label": "Command" });
    const add = () => { const v = input.value.trim(); if (v) save([...rules, { prefix: v, mode: "ask" }]); };
    input.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
    box.replaceChildren(
      el("h2", {}, "Commands"),
      el("p", { class: "hint" }, "Rules for terminal commands of agents with Shell. Allow runs it, Ask refuses it and asks you in Needs you, Block refuses it. Rules match how a command starts, so an agent with Shell can get around them (for example with bash -c): they are a safety net, not a sandbox."),
      el("div", { class: "cmd-table" }, rules.map((r) => el("div", { class: "cmd-row" },
        el("code", { class: "cmd-prefix" }, r.prefix),
        modeSwitch(r.mode, (m) => save(rules.map((x) => x.prefix === r.prefix ? { ...x, mode: m } : x)), { disabled: busy }),
        el("button", { type: "button", class: "link-btn danger", disabled: busy, "aria-label": `Remove ${r.prefix}`,
          onclick: () => save(rules.filter((x) => x.prefix !== r.prefix)) }, "Remove")))),
      el("div", { class: "cmd-add" }, input, el("button", { type: "button", class: "btn small", disabled: busy, onclick: add }, "+ Add command"),
        el("span", { class: "grow" }),
        el("button", { type: "button", class: "link-btn", onclick: async () => {
          if (!confirm("Reset the command rules to the defaults?")) return;
          try { await api("/api/settings/commands/reset", { method: "POST" }); load(); } catch (err) { toast(err.message); }
        } }, "Reset to defaults")));
  }
  load();
  return box;
}

// projectCommands: regulile globale mostenite (Inherit / Allow / Ask / Block) + comenzile doar ale proiectului.
export function projectCommands(projectId) {
  const box = el("section", { class: "section cmd-rules" });
  let data = { global: [], project: [], warnings: [] };
  let busy = false; // salvare in curs: butoanele sunt oprite ca sa nu se suprapuna doua salvari
  async function load() {
    try { data = await api(`/api/projects/${projectId}/commands`); draw(); } catch (err) { toast(err.message); }
  }
  async function save(next) {
    if (busy) return;
    busy = true; draw();
    try {
      await api(`/api/projects/${projectId}/commands`, { method: "PUT", body: next });
      busy = false;
      await load();
    } catch (err) { busy = false; toast(err.message); draw(); }
  }
  const setMode = (prefix, mode) => {
    const rest = data.project.filter((x) => x.prefix !== prefix);
    save(mode ? [...rest, { prefix, mode }] : rest);
  };
  function draw() {
    const own = data.project.filter((r) => !data.global.some((g) => g.prefix === r.prefix));
    const input = el("input", { type: "text", maxlength: 80, placeholder: "e.g. docker", "aria-label": "Command" });
    const add = () => { const v = input.value.trim(); if (v) setMode(v, "ask"); };
    input.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); add(); } });
    box.replaceChildren(
      el("h2", {}, "Commands"),
      el("p", { class: "hint" }, "Defaults come from Settings. Override them here for this project, or add commands only this project has. Changes save immediately."),
      el("div", { class: "cmd-table" },
        data.global.map((g) => {
          const o = data.project.find((x) => x.prefix === g.prefix);
          return el("div", { class: "cmd-row" }, el("code", { class: "cmd-prefix" }, g.prefix),
            modeSwitch(o ? o.mode : "", (m) => setMode(g.prefix, m), { inherit: g.mode, disabled: busy }));
        }),
        own.map((r) => el("div", { class: "cmd-row" }, el("code", { class: "cmd-prefix" }, r.prefix),
          modeSwitch(r.mode, (m) => setMode(r.prefix, m), { disabled: busy }),
          el("button", { type: "button", class: "link-btn danger", disabled: busy, onclick: () => setMode(r.prefix, "") }, "Remove")))),
      // replaceChildren nativ nu filtreaza null: lista de avertismente intra doar cand are continut
      ...(data.warnings.length ? [el("ul", { class: "cmd-warnings" }, data.warnings.map((w) => el("li", {}, w)))] : []),
      el("div", { class: "cmd-add" }, input, el("button", { type: "button", class: "btn small", disabled: busy, onclick: add }, "+ Add command")));
  }
  load();
  return box;
}
