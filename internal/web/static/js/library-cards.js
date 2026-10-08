import { api } from "./api.js";
import { el, toast } from "./ui.js";
import { run, chips } from "./library.js";

// Cardurile Library din afara paginilor Library: propunerile din "Needs you", cardul
// "Share to ... library?" din chat si sectiunea Instructions > Library a proiectului.
// Textul vine de la agenti: doar prin el() / textContent.

const KIND = { tool: "Tool", skill: "Skill", mcp: "MCP" };
const kindLabel = (it) => KIND[it.kind] || it.kind;

// publish: POST publish cu starea "ocupat"; 422/409 apar ca toast
function publish(it, btn, onDone) {
  const slow = it.kind === "mcp" && it.transport === "local";
  return run(btn, slow ? "Testing…" : "Publishing…", () => api(`/api/library/${it.id}/publish`, { method: "POST", body: {} }))
    .then((ok) => { if (ok) { toast(`${it.name} is in the library`, "ok"); onDone?.(); } });
}

// needsProposalBox: cardul unei propuneri in banda "Needs you"
export function libraryProposalBox(it, { onDone }) {
  const pub = el("button", { type: "button", class: "btn small primary" }, "Publish");
  pub.addEventListener("click", () => publish(it, pub, onDone));
  return el("div", { class: "question library-prop" },
    el("div", { class: "question-head" }, el("strong", {}, `Library · ${kindLabel(it).toUpperCase()}`),
      it.madeBy ? el("span", {}, `by ${it.madeBy}`) : null),
    el("p", { class: "question-text" }, el("code", { class: "lib-name" }, it.name)),
    it.description ? el("p", { class: "question-text hint" }, it.description) : null,
    el("div", { class: "actions" },
      el("a", { class: "btn small", href: `#/library/item/${it.id}?tab=code` }, "Review"),
      el("span", { class: "grow" }), pub));
}

// librarySharePrompt: cardul din chat "Share to Tool library?" (mockup 13)
export function librarySharePrompt(it, { onDone }) {
  const stats = el("span", { class: "share-stats" });
  // fisierele propunerii nu au numar de linii in API: le numaram din continut (max 20 de fisiere)
  api(`/api/library/${it.id}`).then(async (d) => {
    const files = d.files || [];
    stats.textContent = `${files.length} ${files.length === 1 ? "file" : "files"}`;
    let lines = 0;
    for (const f of files.slice(0, 20)) {
      const text = await (await fetch(`/api/library/${it.id}/files/${f.name.split("/").map(encodeURIComponent).join("/")}`)).text();
      lines += text ? text.replace(/\n$/, "").split("\n").length : 0;
    }
    if (lines) stats.textContent += ` \u00b7 ${lines} lines`;
  }).catch(() => {});
  const pub = el("button", { type: "button", class: "btn primary" }, "Publish");
  pub.addEventListener("click", () => publish(it, pub, onDone));
  const keep = el("button", { type: "button", class: "btn ghost keep" }, "Keep in this project");
  keep.addEventListener("click", async () => {
    if (await run(keep, "Keeping…", () => api(`/api/library/${it.id}/decline`, { method: "POST", body: { keep: true } }))) onDone?.();
  });
  const hasSecrets = (it.secrets || []).length > 0;
  return el("div", { class: "share-card" },
    el("div", { class: "share-title" }, el("span", { class: "dot-amber" }), `Share to ${kindLabel(it)} library?`),
    el("div", { class: "share-main" },
      el("code", { class: "lib-name" }, it.name),
      el("span", { class: "label-tag" }, it.kind === "tool" ? "script" : it.kind),
      el("span", { class: "grow" }), stats),
    (it.needs || []).length || hasSecrets ? el("div", { class: "chips" }, chips(it)) : null,
    hasSecrets ? el("p", { class: "share-note" }, "Secrets stay in each project. Each project provides its own value when it installs it.") : null,
    el("div", { class: "share-actions" }, pub,
      el("a", { class: "btn ghost", href: `#/library/item/${it.id}?tab=code` }, "Review code"),
      el("span", { class: "grow" }), keep));
}

// libraryInstalls: sectiunea "Library" din Instructions. Se salveaza imediat la fiecare schimbare;
// valorile secretelor nu se afiseaza niciodata (API-ul nu le intoarce, doar "set").
export function libraryInstalls(projectId) {
  const list = el("div", { class: "lib-installs" });
  let rows = [];

  async function put(it, body) {
    try {
      await api(`/api/projects/${projectId}/library/${it.item.id}`, { method: "PUT", body });
    } catch (err) { toast(err.message); }
    await load();
  }

  function row(r) {
    const it = r.item;
    const on = el("input", { type: "checkbox", checked: r.installed });
    on.addEventListener("change", () => put(r, { installed: on.checked }));
    const head = el("label", { class: "check toggle-row" }, on,
      el("span", { class: "grow" },
        el("strong", { class: "lib-name" }, it.name), " ", el("span", { class: "label-tag" }, kindLabel(it)),
        el("span", { class: "hint" }, it.description || "")));
    const meta = el("div", { class: "lib-install-meta" },
      r.installed && r.version ? el("span", { class: "hint" }, `Pinned ${r.version}`) : null,
      r.updateAvailable ? el("button", { type: "button", class: "btn small",
        onclick: () => put(r, { installed: true, upgrade: true }) }, `Update to ${it.version?.version || "latest"}`) : null);
    const secrets = r.installed ? (r.secrets || []).map((s) => {
      const input = el("input", { type: "password", autocomplete: "new-password",
        placeholder: s.set ? "set · type to replace" : "not set", "aria-label": s.name });
      const save = () => { if (input.value) put(r, { installed: true, secrets: { [s.name]: input.value } }); };
      input.addEventListener("change", save);
      return el("div", { class: "lib-secret" },
        el("span", { class: "lib-secret-name" }, s.name),
        el("span", { class: "lib-secret-state" + (s.set ? " set" : "") }, s.set ? "set" : "missing"),
        input,
        s.set ? el("a", { href: "#", class: "hint", onclick: (e) => { e.preventDefault(); put(r, { installed: true, clear: [s.name] }); } }, "Clear") : null);
    }) : [];
    return el("div", { class: "lib-install" }, head, meta, ...secrets);
  }

  async function load() {
    try { rows = await api(`/api/projects/${projectId}/library`); }
    catch (err) { list.replaceChildren(el("p", { class: "hint" }, err.message)); return; }
    list.replaceChildren(...(rows.length ? rows.map(row) : [el("p", { class: "hint" }, "Nothing published yet. Published tools, skills and MCP servers show up here.")]));
  }
  load();
  return el("section", { class: "section" },
    el("h2", {}, "Library", el("span", { class: "hint" }, " · tools, skills and servers for this project")),
    el("p", { class: "hint" }, "Changes save immediately and apply on each agent's next run. Secrets are stored on this machine and never shown again."),
    list);
}
