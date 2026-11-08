import { el, toast, avatar, kindOf } from "./ui.js";
import { api } from "./api.js";

// mcpRequestBox: cererea install_mcp a unui agent, in "Needs you". Utilizatorul vede comanda exacta, pune
// valorile cheilor (sau mai tarziu, in Instructions > Library) si instaleaza; proba picata ofera "Install without test".
export function mcpRequestBox(r, { agent, onDone }) {
  const local = r.transport !== "remote";
  const inputs = (r.secrets || []).map((name) => ({
    name,
    input: el("input", { type: "password", autocomplete: "off", spellcheck: "false", placeholder: "Value (optional)", "aria-label": name }),
  }));
  const status = el("p", { class: "question-text hint", hidden: true });
  const buttons = [];
  const lock = (on) => { for (const b of buttons) b.disabled = on; };
  const values = () => Object.fromEntries(inputs.map((i) => [i.name, i.input.value.trim()]).filter(([, v]) => v));

  async function install(skipTest) {
    lock(true);
    status.hidden = false;
    status.textContent = local && !skipTest ? "Starting the server to check it answers…" : "Installing…";
    try {
      const res = await api(`/api/mcp-requests/${r.id}/install`, { method: "POST", body: { secretValues: values(), skipTest } });
      toast(`Installed ${r.name}${res.tools?.length ? ` (${res.tools.length} tools)` : ""}.`, "ok");
      await onDone?.();
    } catch (err) {
      lock(false);
      if (err.data?.testFailed) {
        status.replaceChildren(err.message, " ", el("button", { type: "button", class: "link-btn", onclick: () => {
          if (confirm(`Install ${r.name} without checking that it starts?`)) install(true);
        } }, "Install without test"));
      } else {
        status.textContent = err.message;
      }
    }
  }
  async function decline() {
    lock(true);
    try { await api(`/api/mcp-requests/${r.id}/decline`, { method: "POST" }); await onDone?.(); }
    catch (err) { lock(false); toast(err.message); }
  }
  const btn = (label, cls, fn) => { const b = el("button", { type: "button", class: cls, onclick: fn }, label); buttons.push(b); return b; };

  const what = local ? [r.command, ...(r.args || [])].join(" ") : r.url;
  return el("div", { class: "question mcp-request" },
    el("div", { class: "question-head" }, agent ? avatar(agent.name, kindOf(agent), 20) : null,
      el("strong", {}, `${r.agentName || "An agent"} wants the MCP server ${r.name}`),
      r.ref ? ` · ${r.ref}` : null),
    r.description ? el("p", { class: "question-text" }, r.description) : null,
    el("p", { class: "question-text" }, r.reason),
    el("div", { class: "mcp-request-cmd" }, el("span", { class: "label" }, local ? "COMMAND" : "URL"), el("code", {}, what)),
    r.existing ? el("p", { class: "question-text hint" }, "Already in your library: installing turns it on in this project.") : null,
    inputs.length ? el("div", { class: "mcp-request-secrets" }, inputs.map((i) =>
      el("label", { class: "field" }, el("span", { class: "label" }, i.name), i.input)),
    el("span", { class: "hint" }, "Leave a key empty to add it later in Instructions > Library.")) : null,
    el("p", { class: "question-text hint" }, local
      ? "Installing runs this command on your computer for this project's agents. HobStack first checks that the server starts."
      : "This project's agents will connect to this address."),
    status,
    el("div", { class: "actions" },
      btn("Install", "btn small primary", () => install(false)),
      btn("Decline", "btn small", decline)));
}
