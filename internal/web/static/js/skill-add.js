import { api } from "./api.js";
import { el, toast, topbar } from "./ui.js";
import { newForm } from "./library.js";
import { dropToken, githubURL, plural, preparedFiles, sourceNode } from "./skill-source.js";

// Pagina "Add skill" (#/library/skill/add, optional ?project=<id>): cautare pe skills.sh (Official = anthropics/skills),
// lista Official cand campul e gol, skill-urile din Claude ("From your Claude") si fereastra de Install.
// Numele, descrierile si fisierele vin din afara (skills.sh / GitHub / ~/.claude): doar prin el() / textContent.

// aceeasi regula ca safeName din internal/library
const NAME_RE = /^[a-z][a-z0-9-]{1,39}$/;
const NAME_HINT = "2-40 characters, starting with a letter; lowercase letters, digits and dashes.";
const MAX_DESC = 300; // serverul refuza descrierile mai lungi

// installsText: 208236 -> "208k installs", 1540 -> "1.5k installs", 1 -> "1 install"
function installsText(n) {
  n = n || 0;
  const short = n >= 1e6 ? (n / 1e6).toFixed(1).replace(/\.0$/, "") + "M"
    : n >= 1e4 ? Math.round(n / 1e3) + "k"
      : n >= 1e3 ? (n / 1e3).toFixed(1).replace(/\.0$/, "") + "k" : String(n);
  return `${short} ${n === 1 ? "install" : "installs"}`;
}

// truncate: taie la max caractere (pe runes), cu "…" la final
function truncate(text, max) {
  const chars = [...(text || "")];
  return chars.length <= max ? chars.join("") : chars.slice(0, max - 1).join("") + "…";
}

export async function skillAddView(root, projectId) {
  let project = null;
  if (projectId) {
    project = await api(`/api/projects/${projectId}`).catch(() => null);
  }
  let closed = false;
  let timer = null;
  let seq = 0; // cautarea curenta: un raspuns vechi nu suprascrie unul nou
  let query = "";
  let ctrl = null; // AbortController-ul cautarii in curs
  const dialogs = new Set(); // functiile de inchidere ale ferestrelor deschise

  const search = el("input", {
    type: "search", class: "mcp-search", placeholder: "Search skills.sh: pdf, frontend, testing…",
    "aria-label": "Search skills.sh", autocomplete: "off",
  });
  const listTitle = el("h2", { class: "mcp-h2" }, "Official (Anthropic)");
  const listHint = el("p", { class: "hint" }, "Skills published by Anthropic in anthropics/skills.");
  const status = el("p", { class: "hint mcp-status", role: "status" });
  const list = el("div", { class: "mcp-list" });
  const localList = el("div", { class: "mcp-list" }, el("p", { class: "hint" }, "Reading your Claude skills…"));

  search.addEventListener("input", () => {
    clearTimeout(timer);
    const q = search.value.trim();
    if (q.length === 1) {
      // 1 caracter nu cauta nimic (prea larg): rezultatele vechi dispar, cererea in curs se anuleaza
      ++seq;
      ctrl?.abort();
      query = q;
      listTitle.textContent = "Results";
      listHint.hidden = true;
      list.replaceChildren();
      status.textContent = "Type at least 2 characters to search.";
      return;
    }
    timer = setTimeout(() => { query = q; load(); }, 300);
  });

  // load: cautarea pe skills.sh, sau lista Official cand campul e gol
  async function load() {
    const my = ++seq;
    ctrl?.abort();
    ctrl = new AbortController();
    const official = !query;
    listTitle.textContent = official ? "Official (Anthropic)" : "Results";
    listHint.hidden = !official;
    list.replaceChildren();
    status.textContent = official ? "Loading the Official skills…" : "Searching…";
    let data;
    try {
      data = await api(`/api/library/skills/search?q=${encodeURIComponent(query)}`, { signal: ctrl.signal });
    } catch (err) {
      if (err.name === "AbortError" || closed || my !== seq) return;
      status.textContent = "";
      const retry = el("button", { type: "button", class: "btn small" }, "Retry");
      retry.addEventListener("click", () => load());
      list.replaceChildren(el("div", { class: "mcp-error", role: "alert" }, el("span", {}, err.message), retry));
      return;
    }
    if (closed || my !== seq) return;
    const rows = official ? (data.official || []) : (data.skills || []);
    status.textContent = rows.length ? "" : (official ? "No Official skills found." : `No skills match “${query}”.`);
    list.replaceChildren(...rows.map((s) => official ? officialRow(s) : searchRow(s)));
  }

  // action: Install (pregateste skill-ul, apoi fereastra) sau "In library"
  function action(s, body) {
    if (s.inLibrary) return el("span", { class: "label-tag mcp-in" }, "In library");
    const btn = el("button", { type: "button", class: "btn small primary" }, "Install");
    btn.addEventListener("click", () => prepare(btn, body, s));
    return btn;
  }

  // repoLink: owner/repo cu link catre GitHub; un repo cu alta forma ramane text
  const repoLink = (repo) => {
    const href = githubURL(repo);
    return href ? el("a", { class: "mono skill-repo", href, target: "_blank", rel: "noopener noreferrer" }, repo)
      : el("span", { class: "mono skill-repo" }, repo);
  };

  function searchRow(s) {
    return el("article", { class: "mcp-row" },
      el("div", { class: "mcp-row-main" },
        el("div", { class: "mcp-row-top" },
          el("span", { class: "mcp-title mono" }, s.name),
          s.official ? el("span", { class: "label-tag skill-official" }, "Official") : null),
        el("div", { class: "skill-meta" }, repoLink(s.repo || s.source),
          el("span", { class: "muted" }, installsText(s.installs)))),
      el("div", { class: "mcp-row-act" }, action(s, { kind: "github", repo: s.repo || s.source, skillId: s.skillId })));
  }

  function officialRow(s) {
    return el("article", { class: "mcp-row" },
      el("div", { class: "mcp-row-main" },
        el("div", { class: "mcp-row-top" },
          el("span", { class: "mcp-title mono" }, s.name),
          el("span", { class: "label-tag skill-official" }, "Official")),
        s.description ? el("p", { class: "mcp-desc", title: s.description }, s.description) : null),
      el("div", { class: "mcp-row-act" }, action(s, { kind: "github", repo: s.repo, skillId: s.skillId })));
  }

  function localRow(s) {
    return el("article", { class: "mcp-row" },
      el("div", { class: "mcp-row-main" },
        el("div", { class: "mcp-row-top" },
          el("span", { class: "mcp-title mono" }, s.name),
          el("span", { class: "label-tag" }, s.label || s.source)),
        s.description ? el("p", { class: "mcp-desc", title: s.description }, s.description) : null),
      el("div", { class: "mcp-row-act" }, action(s, { kind: "local", path: s.path })));
  }

  async function loadLocal() {
    try {
      const data = await api("/api/library/skills/local");
      if (closed) return;
      const skills = data.skills || [];
      localList.replaceChildren(...(skills.length ? skills.map(localRow)
        : [el("p", { class: "hint" }, "No skills in ~/.claude/skills, claude.ai or your Claude Code plugins.")]));
    } catch (err) {
      if (!closed) localList.replaceChildren(el("p", { class: "hint" }, err.message));
    }
  }

  // prepare: serverul aduce skill-ul intr-un folder de lucru; raspunsul deschide fereastra
  async function prepare(btn, body, s) {
    const text = btn.textContent;
    btn.disabled = true;
    btn.classList.add("busy");
    btn.textContent = "Preparing…";
    let prep;
    try {
      prep = await api("/api/library/skills/prepare", { method: "POST", body });
    } catch (err) {
      if (!closed) toast(err.message);
      return;
    } finally {
      btn.disabled = false;
      btn.classList.remove("busy");
      btn.textContent = text;
    }
    if (closed) { dropToken(prep.token); return; }
    installDialog(prep, s.name);
  }

  // installDialog: nume, descriere, fisierele (cu previzualizare), sursa, "Enable in <proiect>", Install.
  // Cu scripturi, Install cere intai o confirmare (al doilea pas al ferestrei).
  function installDialog(prep, title) {
    let token = prep.token; // "" dupa ce s-a folosit sau a expirat (410)
    let busy = false; // cat se instaleaza fereastra nu se inchide
    const files = prep.files || [];
    const scripts = files.filter((f) => f.script);
    const sensitive = files.filter((f) => f.sensitive);
    const name = el("input", { type: "text", id: "skill-name", value: prep.name || "", autocomplete: "off", spellcheck: "false" });
    const nameHint = el("span", { class: "hint" }, NAME_HINT);
    const desc = el("textarea", { id: "skill-desc", rows: "3", maxlength: String(MAX_DESC) });
    desc.value = truncate((prep.description || "").replace(/\s+/g, " ").trim(), MAX_DESC);
    const enable = project ? el("input", { type: "checkbox", checked: true }) : null;
    const fileBox = preparedFiles(prep.token, files);
    const body = el("div", { class: "lib-form-body" });
    const foot = el("div", { class: "lib-form-foot" });
    const btn = (label, cls, onclick) => el("button", { type: "button", class: "btn " + cls, onclick }, label);

    const onKey = (e) => { if (e.key === "Escape") close(); };
    function close() {
      if (busy) return;
      overlay.remove();
      document.removeEventListener("keydown", onKey);
      dialogs.delete(close);
      dropToken(token);
      token = "";
    }

    function validName() {
      const v = name.value.trim();
      const ok = NAME_RE.test(v);
      nameHint.classList.toggle("bad", !ok && !!v);
      return ok;
    }
    name.addEventListener("input", validName);

    function form(error) {
      body.replaceChildren(...[
        el("h2", {}, `Install ${title}`),
        error ? el("p", { class: "mcp-dlg-error", role: "alert" }, error) : null,
        el("p", { class: "skill-src-line" }, el("span", { class: "muted" }, "Source "), sourceNode(prep.source)),
        el("div", { class: "field" }, el("label", { for: "skill-name" }, "Name in Library"), name, nameHint),
        el("div", { class: "field" }, el("label", { for: "skill-desc" }, "Description"), desc,
          el("span", { class: "hint" }, "Agents load the skill when a task matches this description.")),
        el("div", { class: "field" },
          el("span", { class: "label" }, `Files · ${plural(files.length, "file", "files")}`
            + (scripts.length ? ` · ${plural(scripts.length, "script", "scripts")}` : "")),
          fileBox,
          el("span", { class: "hint" }, "Click a file to preview it.")),
        sensitive.length ? el("p", { class: "skill-warn", role: "note" },
          "This skill includes files that may hold secrets (.env, keys). Check them before installing.") : null,
        project ? el("label", { class: "check toggle-row" }, enable,
          el("span", { class: "grow" }, el("strong", {}, `Enable in ${project.name}`),
            el("span", { class: "hint" }, "Installed and turned on for this project's agents."))) : null,
      ].filter(Boolean));
      const install = btn("Install", "primary", () => {
        if (!validName()) { name.focus(); return; }
        if (scripts.length) confirmStep(); else send(install);
      });
      install.disabled = !token;
      foot.replaceChildren(btn("Cancel", "ghost", close), el("span", { class: "grow" }), install);
    }

    // confirmStep: skill-urile cu scripturi cer confirmarea, cu lista scripturilor
    function confirmStep() {
      body.replaceChildren(...[
        el("h2", {}, `Install ${title}`),
        el("p", { class: "mcp-confirm" }, `This skill has ${plural(scripts.length, "script", "scripts")} that agents can run. Install it?`),
        el("ul", { class: "skill-script-list" }, scripts.map((f) => el("li", { class: "mono" }, f.path))),
        sensitive.length ? el("p", { class: "skill-warn", role: "note" },
          "This skill includes files that may hold secrets (.env, keys). Check them before installing.") : null,
      ].filter(Boolean));
      const go = btn("Install", "primary", () => send(go));
      foot.replaceChildren(btn("Back", "ghost", () => form()), el("span", { class: "grow" }), go);
      go.focus();
    }

    async function send(button) {
      const withProject = !!enable?.checked;
      const payload = { token, name: name.value.trim() };
      const d = desc.value.trim();
      if (d) payload.description = d;
      if (withProject) payload.projectId = project.id;
      busy = true;
      foot.querySelectorAll("button").forEach((b) => { b.disabled = true; });
      button.classList.add("busy");
      button.textContent = "Installing…";
      try {
        const it = await api("/api/library/skills/install", { method: "POST", body: payload });
        token = ""; // folosit: nu mai e nimic de sters
        busy = false;
        toast(withProject ? `${it.name} installed in ${project.name}` : `${it.name} is in the library`, "ok");
        close();
        if (!closed) location.hash = `#/library/item/${it.id}`;
      } catch (err) {
        busy = false;
        // pagina parasita cat se instala: doar toast-ul, fereastra (si pregatirea) dispar
        if (closed) { toast(err.message); close(); return; }
        if (err.status === 410) token = ""; // pregatirea a expirat: doar Cancel (si Install din nou din lista)
        form(err.message);
        if (err.status === 409) { name.focus(); name.select(); }
      }
    }

    const overlay = el("div", { class: "modal-overlay", onclick: (e) => { if (e.target === overlay) close(); } },
      el("div", { class: "modal lib-form mcp-dialog skill-dialog", role: "dialog", "aria-modal": "true", "aria-label": `Install ${title}` }, body, foot));
    form();
    document.addEventListener("keydown", onKey); // Esc inchide fereastra oriunde ar fi focusul
    validName();
    document.body.append(overlay);
    name.focus();
    dialogs.add(close);
  }

  const manual = el("a", { href: "#", class: "mcp-manual" }, "Add manually");
  manual.addEventListener("click", (e) => { e.preventDefault(); newForm("skill", () => { location.hash = "#/library/skills"; }); });

  root.append(
    topbar("library"),
    el("div", { class: "page lib-page mcp-add" },
      el("div", { class: "mcp-head" },
        el("nav", { class: "lib-crumbs", "aria-label": "Breadcrumb" },
          el("a", { href: "#/library/skills" }, "Skills"), el("span", { class: "crumb-sep", "aria-hidden": "true" }, "/"),
          el("span", { class: "current" }, "Add skill")),
        el("div", { class: "mcp-head-row" },
          el("h1", { class: "mcp-h1" }, "Add skill"),
          manual),
        el("p", { class: "lib-sub" }, project
          ? `Search skills.sh, pick an Official skill from Anthropic or install one you already use in Claude. Installs into the Library and ${project.name}.`
          : "Search skills.sh, pick an Official skill from Anthropic or install one you already use in Claude.")),
      el("section", { class: "mcp-section" }, search, listTitle, listHint, status, list),
      el("section", { class: "mcp-section" },
        el("h2", { class: "mcp-h2" }, "From your Claude"),
        el("p", { class: "hint" }, "Skills in ~/.claude/skills, synced from claude.ai and from your Claude Code plugins."),
        localList)));
  search.focus();
  load();
  loadLocal();

  return () => {
    closed = true;
    clearTimeout(timer);
    ctrl?.abort();
    // ferestrele deschise se inchid si isi sterg pregatirea (busy = instalarea continua, fereastra ramane pana se termina)
    [...dialogs].forEach((c) => c());
  };
}
