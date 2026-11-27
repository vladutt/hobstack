import { api } from "./api.js";
import { el, toast } from "./ui.js";
import { run, chips, cmdBox, publishItem, keepItem } from "./library.js";

// Cardurile Library din afara paginilor Library: propunerile din "Needs you", cardul
// "Share to ... library?" din chat si sectiunea Instructions > Library a proiectului.
// Textul vine de la agenti: doar prin el() / textContent.

const KIND = { tool: "Tool", skill: "Skill", mcp: "MCP" };
const statsCache = new Map(); // id propunere -> { files, lines }, ca sa nu recitim fisierele la fiecare redesenare
const kindLabel = (it) => KIND[it.kind] || it.kind;

// publish: confirmare cu comanda (server local), erori si "Publish without test" sunt in publishItem
const publish = publishItem;

// needsProposalBox: cardul unei propuneri in banda "Needs you"
export function libraryProposalBox(it, { onDone }) {
  const pub = el("button", { type: "button", class: "btn small primary" }, "Publish");
  pub.addEventListener("click", () => publish(it, pub, onDone));
  return el("div", { class: "question library-prop" },
    el("div", { class: "question-head" }, el("strong", {}, `Library · ${kindLabel(it).toUpperCase()}`),
      it.madeBy ? el("span", {}, `by ${it.madeBy}`) : null),
    el("p", { class: "question-text" }, el("code", { class: "lib-name" }, it.name)),
    it.description ? el("p", { class: "question-text hint" }, it.description) : null,
    cmdBox(it),
    el("div", { class: "actions" },
      el("a", { class: "btn small", href: `#/library/item/${it.id}?tab=code` }, "Review"),
      el("span", { class: "grow" }), pub));
}

// librarySharePrompt: cardul din chat "Share to Tool library?" (mockup 13)
export function librarySharePrompt(it, { onDone }) {
  const stats = el("span", { class: "share-stats" });
  // fisierele propunerii nu au numar de linii in API: le numaram din continut (max 20 de fisiere);
  // daca vreunul nu se poate citi, aratam doar numarul de fisiere (nu subestimam in tacere)
  const showStats = (st) => {
    stats.textContent = `${st.files} ${st.files === 1 ? "file" : "files"}` + (st.lines != null ? ` \u00b7 ${st.lines} lines` : "");
  };
  if (statsCache.has(it.id)) showStats(statsCache.get(it.id));
  else api(`/api/library/${it.id}`).then(async (d) => {
    const files = d.files || [];
    const st = { files: files.length, lines: null };
    showStats(st);
    if (files.length > 20) return;
    const texts = await Promise.all(files.map(async (f) => {
      const res = await fetch(`/api/library/${it.id}/files/${f.name.split("/").map(encodeURIComponent).join("/")}`);
      return res.ok ? res.text() : null;
    }));
    if (texts.some((t) => t === null)) return;
    st.lines = texts.reduce((n, t) => n + (t ? t.replace(/\n$/, "").split("\n").length : 0), 0);
    statsCache.set(it.id, st);
    showStats(st);
  }).catch(() => {});
  const pub = el("button", { type: "button", class: "btn primary" }, "Publish");
  pub.addEventListener("click", () => publish(it, pub, onDone));
  const keep = el("button", { type: "button", class: "btn ghost keep" }, "Keep in this project");
  keep.addEventListener("click", () => keepItem(it, keep, onDone));
  const hasSecrets = (it.secrets || []).length > 0;
  return el("div", { class: "share-card" },
    el("div", { class: "share-title" }, el("span", { class: "dot-amber" }), `Share to ${kindLabel(it)} library?`),
    el("div", { class: "share-main" },
      el("code", { class: "lib-name" }, it.name),
      el("span", { class: "label-tag" }, it.kind === "tool" ? "script" : it.kind),
      el("span", { class: "grow" }), stats),
    cmdBox(it),
    (it.needs || []).length || hasSecrets ? el("div", { class: "chips" }, chips(it)) : null,
    hasSecrets ? el("p", { class: "share-note" }, "Secrets stay in each project. Each project provides its own value when it installs it.") : null,
    el("div", { class: "share-actions" }, pub,
      el("a", { class: "btn ghost", href: `#/library/item/${it.id}?tab=code` }, "Review code"),
      el("span", { class: "grow" }), keep));
}

// Tab-urile Skills / MCP servers / Tools din Instructions: titlul, butonul Add si textul cand lista e goala
const INSTALL_TABS = {
  skill: { title: "Skills", hint: " \u00b7 skills the agents can use in this project", add: ["Add skill", "skill"],
    empty: "No skills yet. Add one, or publish one from the Library." },
  mcp: { title: "MCP servers", hint: " \u00b7 servers the agents can call in this project", add: ["Add MCP server", "mcp"],
    empty: "No MCP servers yet. Add one from the registry or from your Claude." },
  tool: { title: "Tools", hint: " \u00b7 scripts the agents run in this project", add: null,
    empty: "No tools yet. Agents propose tools; published ones show up here." },
};

// libraryInstalls: un tab din Instructions (Skills, MCP servers sau Tools), doar elementele de tipul kind.
// Se salveaza imediat la fiecare schimbare; valorile secretelor nu se afiseaza niciodata (API-ul nu le
// intoarce, doar "set"). onRows(randuri) primeste lista intreaga dupa fiecare citire (numerele de pe tab-uri).
export function libraryInstalls(projectId, kind, onRows) {
  const meta = INSTALL_TABS[kind];
  const list = el("div", { class: "lib-installs" });
  const nodes = new Map(); // id element -> nodul randului
  const queues = new Map(); // id element -> coada de PUT-uri (un singur PUT pe rand odata)
  let windows = false; // tool-urile cu secrete nu merg pe Windows (wrapper sh)

  // put: salveaza un rand si redeseneaza DOAR acel rand; celelalte randuri (cu secrete pe jumatate scrise) raman neatinse
  // sub = "" (instalare, secrete) sau "/always" (Always load in proiect)
  function put(r, body, okMsg, sub = "") {
    const id = r.item.id;
    const next = (queues.get(id) || Promise.resolve()).then(async () => {
      try {
        await api(`/api/projects/${projectId}/library/${id}${sub}`, { method: "PUT", body });
        if (okMsg) toast(okMsg, "ok");
      } catch (err) { toast(err.message); }
      try {
        const all = await api(`/api/projects/${projectId}/library`);
        onRows?.(all);
        const fresh = all.find((x) => x.item.id === id);
        const old = nodes.get(id);
        if (fresh && old) { const n = row(fresh); nodes.set(id, n); old.replaceWith(n); }
      } catch (err) { toast(err.message); }
    });
    queues.set(id, next);
    return next;
  }

  function row(r) {
    const it = r.item;
    const on = el("input", { type: "checkbox", checked: r.installed });
    on.addEventListener("change", () => put(r, { installed: on.checked }));
    const head = el("label", { class: "check toggle-row" }, on,
      el("span", { class: "grow" },
        el("strong", { class: "lib-name" }, it.name),
        it.isDefault ? [" ", el("span", { class: "label-tag default", title: "On in every project unless turned off here" }, "Default")] : null,
        el("span", { class: "hint" }, it.description || "")));
    const meta = el("div", { class: "lib-install-meta" },
      r.installed && r.version ? el("span", { class: "hint" }, r.fromDefault ? `Latest ${r.version}` : `Pinned ${r.version}`) : null,
      r.installed && it.kind === "skill" ? alwaysToggle(r) : null,
      r.installed && r.updateAvailable ? el("button", { type: "button", class: "btn small",
        onclick: () => put(r, { installed: true, upgrade: true }) }, `Update to ${it.version?.version || "latest"}`) : null);
    const secrets = r.installed ? (r.secrets || []).map((s) => {
      const input = el("input", { type: "password", autocomplete: "new-password",
        placeholder: s.set ? "set \u00b7 type to replace" : s.global ? "type to override" : "not set",
        "aria-label": s.name });
      const save = () => { if (input.value) put(r, { installed: true, secrets: { [s.name]: input.value } }, `${s.name} saved`); };
      input.addEventListener("change", save);
      // Enter nu trebuie sa trimita formularul proiectului: salveaza doar secretul
      input.addEventListener("keydown", (e) => {
        if (e.key !== "Enter") return;
        e.preventDefault();
        save();
      });
      return el("div", { class: "lib-secret" },
        el("span", { class: "lib-secret-name" }, s.name),
        el("span", { class: "lib-secret-state" + (s.set || s.global ? " set" : "") }, s.set ? "set" : s.global ? "global" : "missing"),
        input,
        s.set ? el("a", { href: "#", class: "hint", onclick: (e) => {
          e.preventDefault();
          if (confirm(`Remove the saved value for ${s.name}?`)) put(r, { installed: true, clear: [s.name] });
        } }, "Clear") : null);
    }) : [];
    const winNote = windows && it.kind === "tool" && (it.secrets || []).length
      ? el("p", { class: "hint" }, "Tools with secrets are off on Windows.") : null;
    return el("div", { class: "lib-install" }, head, meta, winNote, ...secrets);
  }

  // alwaysToggle: "Always load" pe un skill pornit; fara alegere proprie proiectul urmeaza flag-ul global al skill-ului
  function alwaysToggle(r) {
    const box = el("input", { type: "checkbox", checked: !!r.alwaysLoad });
    box.addEventListener("change", () => put(r, { on: box.checked },
      box.checked ? `${r.item.name} loads on every run` : `${r.item.name} loads only when it fits`, "/always"));
    return el("span", { class: "lib-always" },
      el("label", { class: "check", title: "Agents load this skill at the start of every run, not only when its description fits" },
        box, el("span", {}, "Always load")),
      !r.alwaysOwn && r.item.alwaysLoad ? el("span", { class: "label-tag", title: "Set on the skill for every project" }, "global") : null,
      r.alwaysOwn ? el("a", { href: "#", class: "hint", title: "Follow the skill's global setting",
        onclick: (e) => { e.preventDefault(); put(r, { on: null }, null, "/always"); } }, "Reset") : null);
  }

  async function load() {
    let rows;
    try { rows = await api(`/api/projects/${projectId}/library`); }
    catch (err) { list.replaceChildren(el("p", { class: "hint" }, err.message)); return; }
    onRows?.(rows);
    // cele pornite primele (sort stabil: in rest ordinea serverului); la comutare randul ramane pe loc,
    // ordinea se reface doar la urmatoarea incarcare, ca lista sa nu sara sub cursor
    rows = rows.filter((r) => r.item.kind === kind).sort((a, b) => Number(b.installed) - Number(a.installed));
    nodes.clear();
    list.replaceChildren(...(rows.length ? rows.map((r) => { const n = row(r); nodes.set(r.item.id, n); return n; })
      : [el("p", { class: "hint" }, meta.empty)]));
  }
  api("/api/info").catch(() => ({})).then((i) => { windows = i.os === "windows"; load(); });
  return el("section", { class: "section" },
    el("div", { class: "section-head" },
      el("h2", {}, meta.title, el("span", { class: "hint" }, meta.hint)),
      // Install direct in proiect (paginile Add skill / Add MCP server primesc proiectul)
      meta.add ? el("a", { class: "btn small", href: `#/library/${meta.add[1]}/add?project=${projectId}` }, meta.add[0]) : null),
    el("p", { class: "hint" }, "Changes save immediately and apply on each agent's next run. Secrets are stored on this machine and never shown again."),
    list);
}
