import { api } from "./api.js";
import { el, toast, topbar } from "./ui.js";
import { newForm } from "./library.js";

// Pagina "Add MCP server" (#/library/mcp/add, optional ?project=<id>): cautare in registrul MCP,
// serverele din configurarile Claude ale utilizatorului si fereastra de Install.
// Numele, descrierile si comenzile vin din afara (registru / fisiere de configurare): doar prin el() / textContent.

// aceeasi regula ca safeName din internal/library, plus "team" rezervat pentru MCP (validate.go)
const NAME_RE = /^[a-z][a-z0-9-]{1,39}$/;
const RESERVED = new Set(["team"]);
const NAME_HINT = "2-40 characters, starting with a letter; lowercase letters, digits and dashes.";
const MAX_DESC = 300; // serverul refuza descrierile mai lungi

// kindText: "local · npx" / "remote"
const kindText = (s) => (s.kind === "remote" ? "remote" : "local" + (s.runtime ? " · " + s.runtime : ""));
// quoteArg: argumentele goale sau cu spatii apar intre ghilimele (JSON), ca sa se vada exact ce se trimite
const quoteArg = (a) => (a === "" || /\s/.test(a) ? JSON.stringify(a) : a);
// commandText: comanda exacta (local) sau URL-ul (remote)
const commandText = (s) => (s.kind === "remote" ? s.url : [s.command, ...(s.args || [])].map(quoteArg).join(" "));

// truncate: taie la max caractere (pe runes, nu pe unitati UTF-16), cu "…" la final
function truncate(text, max) {
  const chars = [...(text || "")];
  return chars.length <= max ? chars.join("") : chars.slice(0, max - 1).join("") + "…";
}

// ago: "just now", "12 min ago", "3 h ago", "2 d ago" (ui.js nu are un helper comun)
function ago(iso) {
  const mins = (Date.now() - new Date(iso).getTime()) / 60000;
  if (!(mins >= 1)) return "just now"; // si pentru un ceas usor decalat (valoare negativa)
  if (mins < 60) return `${Math.floor(mins)} min ago`;
  if (mins < 60 * 24) return `${Math.floor(mins / 60)} h ago`;
  return `${Math.floor(mins / 60 / 24)} d ago`;
}

const num = (n) => (n || 0).toLocaleString("en-US");
const POLL_MS = 3000; // cat timp catalogul se actualizeaza, starea se cere la 3 s
const BUILDING_WAIT = "Searching the registry\u2026 this can take up to a minute.";

// postInstall: ca api(), dar pastreaza testFailed din raspunsul 422 (proba picata)
async function postInstall(body) {
  const res = await fetch("/api/library/install-mcp", {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || res.statusText);
    err.testFailed = !!data.testFailed;
    throw err;
  }
  return data;
}

export async function mcpAddView(root, projectId) {
  let project = null;
  if (projectId) {
    project = await api(`/api/projects/${projectId}`).catch(() => null);
  }
  let closed = false;
  let timer = null;
  let seq = 0; // cautarea curenta: un raspuns vechi nu suprascrie unul nou
  let query = "";
  let cursor = "";
  let results = [];
  let ctrl = null; // AbortController-ul cautarii in curs (o cautare noua o anuleaza pe cea veche)
  let searching = false; // prima pagina a cautarii curente inca asteapta raspunsul
  let catalog; // starea catalogului local: undefined = nu stim inca, null = fara catalog
  let pollTimer = null;

  const search = el("input", {
    type: "search", class: "mcp-search", placeholder: "Search the MCP registry: postgres, github, notion…",
    "aria-label": "Search the MCP registry", autocomplete: "off",
  });
  const status = el("p", { class: "hint mcp-status", role: "status" });
  // randul de stare al catalogului local, sub cautare (ascuns fara catalog)
  const catalogRow = el("p", { class: "hint mcp-catalog", hidden: true });
  const list = el("div", { class: "mcp-list" });
  const more = el("div", { class: "mcp-more" });
  const localList = el("div", { class: "mcp-list" }, el("p", { class: "hint" }, "Reading your Claude config…"));

  search.addEventListener("input", () => {
    clearTimeout(timer);
    const q = search.value.trim();
    // 1 caracter nu cauta nimic (prea larg); fara text: primele rezultate
    if (q.length === 1) return;
    timer = setTimeout(() => { query = q; load(false); }, 300);
  });

  // waitText: cat asteapta cautarea; in modul "building" ea merge la registru si poate dura
  const waitText = () => (catalog?.state === "building" ? BUILDING_WAIT : "Searching\u2026");

  // updateButton: "Update now" (link-buton) sau "Updating…" cat timp catalogul se actualizeaza
  function updateButton() {
    if (catalog.syncing) return el("span", { class: "mcp-catalog-busy" }, "Updating\u2026");
    const btn = el("button", { type: "button", class: "link-btn mcp-catalog-act" }, "Update now");
    btn.addEventListener("click", () => syncNow(btn));
    return btn;
  }

  // drawCatalog: randul de stare din ultima stare cunoscuta
  function drawCatalog() {
    if (!catalog) { catalogRow.hidden = true; catalogRow.replaceChildren(); return; }
    catalogRow.hidden = false;
    const c = catalog;
    if (c.state === "building" && (c.syncing || !c.lastError)) {
      catalogRow.replaceChildren(`Building local catalog\u2026 ${num(c.count)} servers so far. Search uses the live registry until it's done.`);
    } else if (c.lastError && !c.syncing) {
      catalogRow.replaceChildren(el("span", { class: "mcp-catalog-err" }, `Last update failed: ${c.lastError}`), " \u00b7 ", updateButton());
    } else {
      catalogRow.replaceChildren(`Catalog: ${num(c.count)} servers`,
        c.updatedAt ? ` \u00b7 updated ${ago(c.updatedAt)}` : "", " \u00b7 ", updateButton());
    }
  }

  // setCatalog: starea noua (din orice raspuns). Din building in ready cautarea curenta se reface (acum local);
  // cat timp syncing, starea se cere din nou la 3 s.
  function setCatalog(c) {
    if (closed || c === undefined) return;
    const was = catalog;
    catalog = c;
    drawCatalog();
    if (searching) status.textContent = waitText();
    if (was?.state === "building" && c?.state === "ready") load(false);
    clearTimeout(pollTimer);
    pollTimer = c?.syncing ? setTimeout(poll, POLL_MS) : null;
  }

  async function poll() {
    pollTimer = null;
    try {
      const data = await api("/api/library/registry/status");
      setCatalog(data.catalog);
    } catch {
      // o eroare trecatoare: incercam din nou peste 3 s, cat timp stim ca se actualizeaza
      if (!closed && catalog?.syncing) pollTimer = setTimeout(poll, POLL_MS);
    }
  }

  // syncNow: "Update now". 409 (deja se actualizeaza) = doar starea; alta eroare = toast
  async function syncNow(btn) {
    btn.disabled = true;
    try {
      const data = await api("/api/library/registry/sync", { method: "POST" });
      setCatalog(data?.catalog);
    } catch (err) {
      if (closed) return;
      if (err.status === 409) { poll(); return; }
      btn.disabled = false;
      toast(err.message);
    }
  }

  // load: prima pagina (append=false) sau "Load more" (append=true, cu cursorul)
  async function load(append) {
    const my = ++seq;
    ctrl?.abort(); // cererea precedenta nu mai conteaza
    ctrl = new AbortController();
    const signal = ctrl.signal;
    if (!append) {
      cursor = ""; results = []; list.replaceChildren(); more.replaceChildren();
      searching = true;
      status.textContent = waitText();
    }
    const params = new URLSearchParams();
    if (query) params.set("search", query);
    if (append && cursor) params.set("cursor", cursor);
    let data;
    try {
      data = await api(`/api/library/registry?${params}`, { signal });
    } catch (err) {
      // anulata de o cautare noua / de iesirea din pagina: fara eroare
      if (err.name === "AbortError" || closed || my !== seq) return;
      // cursor invalid (de pe registrul live, catalogul s-a facut gata intre timp): cautare noua, prima pagina
      if (err.status === 400 && append && cursor) {
        if (err.data && "catalog" in err.data) setCatalog(err.data.catalog);
        load(false);
        return;
      }
      searching = false;
      if (err.data && "catalog" in err.data) setCatalog(err.data.catalog);
      status.textContent = "";
      const retry = el("button", { type: "button", class: "btn small" }, "Retry");
      retry.addEventListener("click", () => load(append));
      const box = el("div", { class: "mcp-error", role: "alert" }, el("span", {}, err.message), retry);
      if (append) more.replaceChildren(box); else list.replaceChildren(box);
      return;
    }
    if (closed || my !== seq) return;
    searching = false;
    results.push(...(data.servers || []));
    cursor = data.nextCursor || "";
    status.textContent = results.length ? "" : (query ? `No servers match “${query}”.` : "The registry returned no servers.");
    list.replaceChildren(...results.map(registryRow));
    if (cursor) {
      const btn = el("button", { type: "button", class: "btn ghost" }, "Load more");
      btn.addEventListener("click", () => { btn.disabled = true; btn.textContent = "Loading…"; load(true); });
      more.replaceChildren(btn);
    } else more.replaceChildren();
    // la final: daca starea trece din building in ready, setCatalog reface cautarea
    setCatalog(data.catalog);
  }

  // action: Install / In library / Install dezactivat cu motivul
  function action(s, onInstall) {
    if (s.inLibrary) return el("span", { class: "label-tag mcp-in" }, "In library");
    const btn = el("button", {
      type: "button", class: "btn small" + (s.installable ? " primary" : ""),
      disabled: !s.installable, title: s.installable ? null : s.reason,
    }, "Install");
    if (s.installable) btn.addEventListener("click", onInstall);
    return btn;
  }

  function registryRow(s) {
    const title = s.title || s.name;
    return el("article", { class: "mcp-row" },
      el("div", { class: "mcp-row-main" },
        el("div", { class: "mcp-row-top" },
          el("span", { class: "mcp-title" }, title),
          el("span", { class: "label-tag" }, kindText(s)),
          s.version ? el("span", { class: "mono muted mcp-ver" }, "v" + s.version) : null),
        title !== s.name ? el("span", { class: "mono muted mcp-id" }, s.name) : null,
        s.description ? el("p", { class: "mcp-desc", title: s.description }, s.description) : null,
        !s.installable && s.reason ? el("p", { class: "mcp-reason" }, s.reason) : null),
      el("div", { class: "mcp-row-act" }, action(s, () => installDialog({
        title, name: s.suggestedName, kind: s.kind, command: s.command, args: s.args || [], url: s.url,
        env: s.env || [], description: truncate(s.description || title, MAX_DESC),
      }))));
  }

  function localRow(s) {
    return el("article", { class: "mcp-row" },
      el("div", { class: "mcp-row-main" },
        el("div", { class: "mcp-row-top" },
          el("span", { class: "mcp-title mono" }, s.name),
          el("span", { class: "label-tag" }, s.source),
          el("span", { class: "label-tag" }, s.kind === "remote" ? "remote" : "local")),
        el("code", { class: "mcp-cmd" }, commandText(s)),
        (s.env || []).length ? el("div", { class: "chips" }, s.env.map((v) => el("span", { class: "chip secret" }, v.name))) : null,
        !s.installable && s.reason ? el("p", { class: "mcp-reason" }, s.reason) : null),
      el("div", { class: "mcp-row-act" }, action(s, () => installDialog({
        title: s.name, name: s.suggestedName, kind: s.kind, command: s.command, args: s.args || [], url: s.url,
        env: s.env || [], description: `From ${s.source} config`,
      }))));
  }

  async function loadLocal() {
    try {
      const data = await api("/api/library/local-mcp");
      if (closed) return;
      const servers = data.servers || [];
      localList.replaceChildren(...(servers.length ? servers.map(localRow)
        : [el("p", { class: "hint" }, "No MCP servers in your Claude Code or Claude Desktop config.")]));
    } catch (err) {
      if (!closed) localList.replaceChildren(el("p", { class: "hint" }, err.message));
    }
  }

  // installDialog: fereastra de Install. Pasi: formular -> confirmarea comenzii (doar local) -> proba;
  // proba picata -> eroarea + "Install without test" -> a doua confirmare -> skipTest.
  function installDialog(s) {
    const name = el("input", { type: "text", id: "mcp-name", value: s.name || "", autocomplete: "off", spellcheck: "false" });
    const nameHint = el("span", { class: "hint" }, NAME_HINT);
    const enable = project ? el("input", { type: "checkbox", checked: true }) : null;
    const values = new Map(); // nume variabila -> input (doar cu proiect)
    const missing = new Map(); // nume variabila -> mesajul "Required" de sub input
    let busy = false; // cat ruleaza proba / instalarea fereastra nu se inchide
    const cmd = commandText(s);
    const local = s.kind !== "remote";

    const envBox = el("div", { class: "mcp-env" });
    // stored = devine secret in Library (din registru doar cele obligatorii, din configurarile Claude toate);
    // celelalte sunt doar afisate: un secret fara valoare ar opri serverul pentru agenti
    const stored = s.env.filter((v) => v.stored);
    const optional = s.env.filter((v) => !v.stored);
    function drawEnv() {
      if (!s.env.length) { envBox.replaceChildren(); return; }
      const badges = (v) => [v.required ? el("span", { class: "mcp-badge req" }, "required") : null,
        v.secret ? el("span", { class: "mcp-badge" }, "secret") : null];
      const listItem = (v) => el("li", {},
        el("span", { class: "mcp-env-label" }, el("span", { class: "mono" }, v.name), badges(v)),
        v.description ? el("span", { class: "hint" }, v.description) : null);
      // variabilele optionale: doar informatie, fara camp (nu se salveaza)
      const optionalBlock = () => optional.length ? [
        el("ul", { class: "mcp-env-list" }, optional.map((v) => el("li", {},
          el("span", { class: "mcp-env-label" }, el("span", { class: "mono" }, v.name),
            el("span", { class: "mcp-badge" }, "optional")),
          v.description ? el("span", { class: "hint" }, v.description) : null))),
        el("p", { class: "hint" }, "Optional \u2014 not stored; set it in your environment if needed."),
      ] : [];
      if (enable?.checked) {
        envBox.replaceChildren(el("span", { class: "label" }, "Environment variables"),
          ...stored.map((v) => {
            let input = values.get(v.name);
            if (!input) {
              input = el("input", { type: "password", autocomplete: "new-password", spellcheck: "false" });
              values.set(v.name, input);
            }
            input.id = input.id || `mcp-env-${values.size}-${v.name}`;
            let warn = missing.get(v.name);
            if (!warn) {
              warn = el("span", { class: "hint bad", role: "alert", hidden: true },
                `Required \u2014 fill it in or uncheck Enable in ${project.name}.`);
              missing.set(v.name, warn);
              input.addEventListener("input", () => { if (input.value) warn.hidden = true; });
            }
            return el("div", { class: "field" },
              el("label", { for: input.id, class: "mcp-env-label" }, el("span", { class: "mono" }, v.name), badges(v)),
              input,
              v.required ? warn : null,
              v.description ? el("span", { class: "hint" }, v.description) : null);
          }),
          ...(stored.length ? [el("p", { class: "hint" }, `Saved as ${project.name}'s secrets on this machine and used for the test. Never shown again.`)] : []),
          ...optionalBlock());
      } else {
        envBox.replaceChildren(el("span", { class: "label" }, "Environment variables"),
          ...(stored.length ? [el("ul", { class: "mcp-env-list" }, stored.map(listItem)),
            el("p", { class: "hint" }, "Set the values per project in Instructions > Library after installing.")] : []),
          ...optionalBlock());
      }
    }
    enable?.addEventListener("change", drawEnv);
    drawEnv();

    const body = el("div", { class: "lib-form-body" });
    const foot = el("div", { class: "lib-form-foot" });
    const close = () => { if (!busy) overlay.remove(); };
    const btn = (label, cls, onclick) => el("button", { type: "button", class: "btn " + cls, onclick }, label);
    const cmdBlock = () => el("pre", { class: "md-pre mcp-exact" }, cmd);

    function validName() {
      const v = name.value.trim();
      const reserved = RESERVED.has(v);
      const ok = NAME_RE.test(v) && !reserved;
      nameHint.textContent = reserved ? `"${v}" is reserved for HobStack's own MCP server.` : NAME_HINT;
      nameHint.classList.toggle("bad", !ok && !!v);
      return ok;
    }

    // requiredFilled: cu proiectul bifat, variabilele obligatorii goale sunt marcate sub input (fara proiect nu se cer)
    function requiredFilled() {
      if (!enable?.checked) return true;
      let first = null;
      for (const v of stored) {
        if (!v.required) continue;
        const input = values.get(v.name);
        const empty = !input || !input.value;
        missing.get(v.name).hidden = !empty;
        if (empty && !first) first = input;
      }
      first?.focus();
      return !first;
    }
    name.addEventListener("input", validName);

    // form: primul pas (si inapoi din confirmare)
    function form(error) {
      body.replaceChildren(...[
        el("h2", {}, `Install ${s.title}`),
        error ? el("p", { class: "mcp-dlg-error", role: "alert" }, error) : null,
        el("div", { class: "field" }, el("label", { for: "mcp-name" }, "Name in Library"), name, nameHint),
        el("div", { class: "field" }, el("span", { class: "label" }, local ? "Runs on your machine" : "Connects to"), cmdBlock()),
        project ? el("label", { class: "check toggle-row" }, enable,
          el("span", { class: "grow" }, el("strong", {}, `Enable in ${project.name}`),
            el("span", { class: "hint" }, "Installed and turned on for this project's agents."))) : null,
        envBox,
      ].filter(Boolean));
      const install = btn("Install", "primary", () => {
        if (!validName()) { name.focus(); return; }
        if (!requiredFilled()) return;
        if (local) confirmStep(false); else send(install, false);
      });
      foot.replaceChildren(btn("Cancel", "ghost", close), el("span", { class: "grow" }), install);
    }

    // confirmStep: comanda exacta inainte de rulare; skip=true e a doua confirmare, dupa o proba picata
    function confirmStep(skip, error) {
      body.replaceChildren(...[
        el("h2", {}, skip ? "Install without test?" : `Install ${s.title}`),
        error ? el("div", { class: "mcp-dlg-error", role: "alert" }, el("strong", {}, "The test failed. "), error) : null,
        el("p", { class: "mcp-confirm" }, skip
          ? "HobStack won't check that the server starts. Agents will run this command unverified:"
          : "HobStack will run this command to test it:"),
        cmdBlock(),
        skip ? null : el("p", { class: "hint" }, "It starts the server once, asks for its tools and stops it. It may download the package first (up to 30 s)."),
      ].filter(Boolean));
      if (error && !skip) {
        // dupa proba picata: eroarea + "Install without test" (care cere o a doua confirmare)
        foot.replaceChildren(btn("Cancel", "ghost", close), el("span", { class: "grow" }),
          btn("Install without test", "ghost", () => confirmStep(true, error)),
          btn("Try again", "", (e) => send(e.currentTarget, false)));
        return;
      }
      const go = btn(skip ? "Install without test" : "Run test and install", "primary", () => send(go, skip));
      foot.replaceChildren(btn(skip ? "Back" : "Cancel", "ghost", skip ? () => confirmStep(false, error) : () => form()),
        el("span", { class: "grow" }), go);
    }

    async function send(button, skipTest) {
      const withProject = !!enable?.checked;
      const payload = {
        name: name.value.trim(), description: s.description, kind: local ? "local" : "remote",
        command: local ? s.command : "", args: local ? s.args : [], url: local ? "" : s.url,
        secrets: stored.map((v) => v.name), skipTest,
      };
      if (withProject) {
        payload.projectId = project.id;
        const secretValues = {};
        for (const [k, input] of values) if (input.value) secretValues[k] = input.value;
        if (Object.keys(secretValues).length) payload.secretValues = secretValues;
      }
      const text = button.textContent;
      busy = true;
      foot.querySelectorAll("button").forEach((b) => { b.disabled = true; });
      button.classList.add("busy");
      button.textContent = local && !skipTest ? "Testing…" : "Installing…";
      try {
        const it = await postInstall(payload);
        busy = false;
        toast(withProject ? `${it.name} installed in ${project.name}` : `${it.name} is in the library`, "ok");
        // pagina parasita intre timp (Back in browser): doar toast-ul, fara navigare
        if (closed) return;
        close();
        location.hash = `#/library/item/${it.id}`;
      } catch (err) {
        busy = false;
        if (closed) { toast(err.message); return; }
        button.classList.remove("busy");
        button.textContent = text;
        foot.querySelectorAll("button").forEach((b) => { b.disabled = false; });
        if (err.testFailed && !skipTest) confirmStep(false, err.message);
        else form(err.message);
      }
    }

    const overlay = el("div", { class: "modal-overlay", onclick: (e) => { if (e.target === overlay) close(); } },
      el("div", { class: "modal lib-form mcp-dialog", role: "dialog", "aria-modal": "true", "aria-label": `Install ${s.title}` }, body, foot));
    overlay.addEventListener("keydown", (e) => { if (e.key === "Escape") close(); });
    form();
    document.body.append(overlay);
    name.focus();
    dialogs.add(overlay);
  }
  const dialogs = new Set();

  const manual = el("a", { href: "#", class: "mcp-manual" }, "Add manually");
  manual.addEventListener("click", (e) => { e.preventDefault(); newForm("mcp", () => { location.hash = "#/library/mcp"; }); });

  root.append(
    topbar("library"),
    el("div", { class: "page lib-page mcp-add" },
      el("div", { class: "mcp-head" },
        el("nav", { class: "lib-crumbs", "aria-label": "Breadcrumb" },
          el("a", { href: "#/library/mcp" }, "MCP"), el("span", { class: "crumb-sep", "aria-hidden": "true" }, "/"),
          el("span", { class: "current" }, "Add server")),
        el("div", { class: "mcp-head-row" },
          el("h1", { class: "mcp-h1" }, "Add MCP server"),
          manual),
        el("p", { class: "lib-sub" }, project
          ? `Search the official MCP registry or install a server you already use in Claude. Installs into the Library and ${project.name}.`
          : "Search the official MCP registry or install a server you already use in Claude.")),
      el("section", { class: "mcp-section" }, search, catalogRow, status, list, more),
      el("section", { class: "mcp-section" },
        el("h2", { class: "mcp-h2" }, "From your Claude"),
        el("p", { class: "hint" }, "Servers in ~/.claude.json and Claude Desktop. Only variable names are read, never their values."),
        localList)));
  search.focus();
  poll(); // starea catalogului (randul de sub cautare si textul de asteptare), fara sa astepte cautarea
  load(false);
  loadLocal();

  return () => {
    closed = true;
    clearTimeout(timer);
    clearTimeout(pollTimer);
    ctrl?.abort();
    dialogs.forEach((d) => d.remove());
  };
}
