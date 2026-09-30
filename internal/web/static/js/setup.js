import { api } from "./api.js";
import { el, field, toast, TOOLS } from "./ui.js";
import { liveRenderer } from "./live.js";

// Wizard-ul "Proiect nou cu AI": 1) folder + descriere, 2) analiza live,
// 3) propunerea editabila, apoi "Creeaza proiectul".
// Analiza ruleaza pe server: pagina se poate inchide, iar #/setup/<id> o
// redeschide la pasul la care a ajuns (cardul din Proiecte duce aici).
//
// Modul folder: target = { projectId, folderId } (ruta #/setup/folder/<p>/<f>,
// sau o sesiune care are projectId). AI-ul citeste un folder dintr-un proiect
// existent si propune agenti noi; echipa existenta nu se schimba.
export async function setupView(root, id, target = null) {
  const info = await api("/api/info");
  const form = { workdirs: [""], description: "", model: "sonnet" };
  let session = null;   // {id, runId}
  let stream = null;    // EventSource-ul analizei
  let closed = false;
  const cleanup = () => { closed = true; closeStream(); };

  // sesiunea se incarca inainte de header: din ea aflam daca e modul folder
  let loadErr = null;
  if (id) {
    try { session = await api(`/api/setup/${id}`); } catch (err) { loadErr = err; }
    if (session?.projectId) target = { projectId: session.projectId, folderId: session.folderId };
  }

  // modul folder: proiectul (pentru echipa existenta) si folderul analizat
  let project = null, folder = null;
  if (target) {
    try { project = await api(`/api/projects/${target.projectId}`); } catch { /* tratat mai jos */ }
    folder = (project?.folders || []).find((f) => f.id === target.folderId) || null;
    if (!folder) {
      toast("The folder no longer exists.");
      location.hash = "#/";
      return cleanup;
    }
  }

  const body = el("div", { class: "wizard" });
  const title = target ? `Set up agents for ${folder.name}` : "New project with AI";
  root.append(
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" }, el("a", { href: "#/" }, "Projects"), " / ",
          target ? [el("a", { href: `#/p/${project.id}/edit` }, project.name), " / "] : null, title),
        el("h1", {}, title),
        el("p", { class: "muted" }, target
          ? "An AI reads this folder and proposes agents for it. Your existing team is not changed."
          : "Pick a folder (or describe the project); an AI reads it and proposes the project and the team. You decide what to keep."))),
    body);

  // modelSelect: modelele recomandate pentru analiza
  const modelSelect = () => el("select", { value: form.model },
    (info.models || []).filter((m) => m.group === "latest").map((m) => el("option", { value: m.id }, m.label)));

  // ---------- pasul 1 ----------
  function stepChoose() {
    if (target) { stepChooseFolder(); return; }
    // lista de foldere: un rand cu input, "Browse..." si buton de stergere
    const folderBox = el("div", { class: "folder-rows" });
    const inputs = [];
    const addFolder = (value = "") => {
      const input = el("input", { type: "text", value, placeholder: inputs.length ? "e.g. ~/code/probe-landing" : "e.g. ~/code/shop (optional)" });
      const pick = info.canPickFolder && el("button", { type: "button", class: "btn", onclick: async () => {
        try { const r = await api("/api/pick-folder", { method: "POST" }); if (r?.path) input.value = r.path; } catch (err) { toast(err.message); }
      } }, "Browse...");
      const row = el("div", { class: "input-row" }, input, pick);
      const del = el("button", { type: "button", class: "btn", "aria-label": "Remove folder", onclick: () => {
        inputs.splice(inputs.indexOf(input), 1); row.remove(); syncDel();
      } }, "\u00d7");
      row.append(del);
      input.del = del;
      inputs.push(input);
      folderBox.append(row);
      syncDel();
    };
    const syncDel = () => inputs.forEach((i) => { i.del.hidden = inputs.length === 1; });
    (form.workdirs.length ? form.workdirs : [""]).forEach((v) => addFolder(v));
    const description = el("textarea", { value: form.description, rows: 5, maxlength: 4000,
      placeholder: "What do you want to do? e.g. refactor checkout and add payment tests. Required if you do not pick a folder." });
    const model = modelSelect();
    const go = el("button", { type: "submit", class: "btn primary" }, "Analyze");
    body.replaceChildren(el("form", { class: "section wizard-step", onsubmit: start },
      el("h2", {}, "1. What to analyze"),
      el("div", { class: "field" },
        el("span", { class: "label" }, "Project folders"),
        folderBox,
        el("div", { class: "actions" }, el("button", { type: "button", class: "btn small", onclick: () => addFolder() }, "+ Add folder")),
        el("span", { class: "hint" }, "The folders are not modified: the AI only reads. Several folders = one project with several parts (e.g. client + landing page). Empty = new project; the folder is created at the end.")),
      field("What do you want to do?", description),
      field("Model for the analysis", model, "Sonnet is a good balance; Haiku is faster."),
      el("div", { class: "actions" }, go)));

    async function start(e) {
      e.preventDefault();
      Object.assign(form, { workdirs: inputs.map((i) => i.value.trim()).filter(Boolean), description: description.value.trim(), model: model.value });
      go.disabled = true;
      try {
        await begin();
      } catch (err) {
        toast(err.message);
        go.disabled = false;
      }
    }
  }

  // pasul 1 in modul folder: folderul e deja ales, raman descrierea si modelul
  function stepChooseFolder() {
    const description = el("textarea", { value: form.description, rows: 5, maxlength: 4000,
      placeholder: "e.g. build and maintain the landing page. Optional." });
    const model = modelSelect();
    const go = el("button", { type: "submit", class: "btn primary" }, "Analyze");
    body.replaceChildren(el("form", { class: "section wizard-step", onsubmit: start },
      el("h2", {}, "1. What to analyze"),
      field("What should these agents do?", description),
      field("Model for the analysis", model, "Sonnet is a good balance; Haiku is faster."),
      el("div", { class: "actions" }, go)));

    async function start(e) {
      e.preventDefault();
      Object.assign(form, { description: description.value.trim(), model: model.value });
      go.disabled = true;
      try {
        await begin();
      } catch (err) {
        toast(err.message);
        go.disabled = false;
      }
    }
  }

  // back: inapoi la pasul 1; sesiunea ramane in Proiecte pana o ascunzi
  function back() {
    history.replaceState(null, "", target ? `#/setup/folder/${target.projectId}/${target.folderId}` : "#/setup");
    stepChoose();
  }

  // begin porneste o analiza noua cu datele din form. Adresa devine
  // #/setup/<id> (fara hashchange), ca un refresh sa nu piarda analiza.
  async function begin() {
    session = target
      ? await api(`/api/projects/${target.projectId}/folders/${target.folderId}/setup`, { method: "POST", body: { description: form.description, model: form.model } })
      : await api("/api/setup", { method: "POST", body: form });
    history.replaceState(null, "", `#/setup/${session.id}`);
    stepAnalyze();
  }

  // ---------- pasul 2 ----------
  function stepAnalyze() {
    const box = el("div", { class: "live wizard-live" }, el("div", { class: "live-head" }, "The AI is reading the project..."));
    const stopBtn = el("button", { type: "button", class: "btn", onclick: () => api(`/api/setup/${session.id}/stop`, { method: "POST" }).catch((e) => toast(e.message)) }, "Stop");
    body.replaceChildren(el("section", { class: "section wizard-step" },
      el("h2", {}, "2. Analysis"),
      el("p", { class: "hint" }, "You can leave this page: the analysis keeps running and shows up in Projects when it is done."),
      box, el("div", { class: "actions" }, stopBtn)));
    const render = liveRenderer(box);
    stream = new EventSource(`/api/runs/${session.runId}/events`);
    stream.onmessage = (msg) => {
      let ev;
      try { ev = JSON.parse(msg.data); } catch { return; }
      if (ev.type === "app_done") { closeStream(); checkResult(); return; }
      render(ev);
      box.scrollTop = box.scrollHeight;
    };
    stream.onerror = () => { if (stream?.readyState === EventSource.CLOSED) { closeStream(); checkResult(); } };
  }

  function closeStream() { stream?.close(); stream = null; }

  // checkResult: finish()-ul din server poate veni imediat dupa app_done
  async function checkResult(tries = 0) {
    if (closed) return;
    let s;
    try { s = await api(`/api/setup/${session.id}`); } catch (err) { toast(err.message); stepChoose(); return; }
    if (s.status === "running" && tries < 20) { setTimeout(() => checkResult(tries + 1), 250); return; }
    if (s.status === "ready") { stepReview(s.proposal); return; }
    stepFailed(s);
  }

  function stepFailed(s) {
    body.replaceChildren(el("section", { class: "section wizard-step" },
      el("h2", {}, s.status === "stopped" ? "Analysis stopped" : "Analysis failed"),
      s.error && el("p", { class: "err" }, s.error),
      el("div", { class: "actions" },
        el("button", { type: "button", class: "btn", onclick: back }, "Back"),
        el("button", { type: "button", class: "btn primary", onclick: async () => {
          const old = session.id;
          try {
            await begin();
            api(`/api/setup/${old}`, { method: "DELETE" }).catch(() => {}); // cardul vechi nu mai trebuie
          } catch (err) { toast(err.message); }
        } }, "Retry"))));
  }

  // ---------- pasul 3 ----------
  function stepReview(p) {
    const name = el("input", { type: "text", value: p.name, maxlength: 80 });
    const goal = el("textarea", { value: p.goal, rows: 3 });
    const context = el("textarea", { value: p.context, rows: 8 });
    const tree = el("pre", { class: "wizard-tree" });
    // modul folder: sectiunile proprii (folderul, contextul lui) si echipa existenta
    const fm = target ? reviewFolderSection(p) : null;
    // numele agentilor existenti (lowercase -> numele scris), sefi/revieweri posibili
    const existing = new Map(target ? (project.agents || []).map((a) => [a.name.toLowerCase(), a.name]) : []);
    // foldere: campurile editabile ale fiecarui folder propus (in modul folder nu
    // exista: agentii noi lucreaza toti in folderul analizat)
    const folders = target ? [] : (p.folders || []).map((f) => ({
      path: f.path,
      name: el("input", { type: "text", value: f.name, maxlength: 40 }),
      context: el("textarea", { value: f.context || "", rows: 3 }),
    }));
    const many = folders.length > 1;
    const folderIndex = (name) => Math.max(0, (p.folders || []).findIndex((f) => f.name.toLowerCase() === (name || "").toLowerCase()));
    // un rand per agent propus, cu referinte la input-uri
    const rows = p.agents.map((a) => {
      const r = {
        include: el("input", { type: "checkbox", checked: true }),
        name: el("input", { type: "text", value: a.name, maxlength: 40 }),
        role: el("input", { type: "text", value: a.role, maxlength: 80 }),
        model: el("select", { value: a.model }, (info.models || []).map((m) => el("option", { value: m.id }, m.label))),
        resp: el("textarea", { value: a.responsibilities, rows: 2 }),
        // null (nu false) cu un singur folder: r.folder?.x protejeaza doar null/undefined
        folder: many ? el("select", { value: String(folderIndex(a.folder)) },
          folders.map((f, i) => el("option", { value: i }, f.name.value))) : null,
        parent: el("select", {}), reviewer: el("select", {}),
        parentName: a.reportsTo || "", reviewerName: a.reviewedBy || "",
        oldName: a.name, // numele de dinainte de o redenumire (pentru legaturile celorlalti)
        background: el("textarea", { value: a.background || "", rows: 4 }),
        instructions: el("textarea", { value: a.instructions || "", rows: 4 }),
        doneDef: el("textarea", { value: a.doneDef || "", rows: 3 }),
        tools: TOOLS.map((t) => ({ id: t.id, box: el("input", { type: "checkbox", checked: (a.tools || []).includes(t.id) }), label: t.label })),
      };
      r.el = el("div", { class: "wizard-agent" },
        el("label", { class: "check wizard-include" }, r.include, el("strong", {}, "Include")),
        el("div", { class: "row" }, field("Name", r.name), field("Role", r.role)),
        el("div", { class: "row" }, field("Model", r.model), field("Takes", r.resp)),
        el("div", { class: "row" }, field("Reports to", r.parent), field("Reviewed by", r.reviewer)),
        r.folder && el("div", { class: "row" }, field("Folder", r.folder)),
        el("div", { class: "field" }, el("span", { class: "label" }, "Tools"),
          el("div", { class: "wizard-tools" }, r.tools.map((t) => el("label", { class: "check" }, t.box, t.label)))),
        el("details", {}, el("summary", {}, "Prompt"),
          field("Background", r.background), field("Instructions", r.instructions), field("Definition of done", r.doneDef)));
      return r;
    });

    // selectele de sef/reviewer contin doar agentii inclusi; un agent scos
    // trimite copiii la seful lui (ca la stergere), iar reviewer-ul devine nimeni
    // parentName/reviewerName: legaturile dorite (din propunere, selecte, redenumiri).
    // effParent/effReviewer: legaturile efective, calculate aici din cele dorite si din
    // bife. Un agent debifat isi trimite copiii la seful lui, iar cei verificati de el
    // raman fara reviewer; la re-bifare revine totul, fiindca cele dorite nu se schimba.
    function sync() {
      const included = rows.filter((r) => r.include.checked);
      const byName = new Map(rows.map((r) => [r.name.value.trim().toLowerCase(), r]));
      for (const r of rows) {
        r.el.classList.toggle("excluded", !r.include.checked);
        // urcam peste agentii noi debifati; want = numele la care am ajuns. Daca nu e
        // un agent nou, poate fi unul din echipa existenta (modul folder).
        let want = r.parentName;
        let boss = byName.get(want.toLowerCase());
        for (let i = 0; boss && !boss.include.checked && i < rows.length; i++) {
          want = boss.parentName;
          boss = byName.get(want.toLowerCase());
        }
        const rev = byName.get(r.reviewerName.toLowerCase());
        r.effParent = boss ? (boss !== r && boss.include.checked ? boss.name.value.trim() : "")
          : existing.get(want.toLowerCase()) || "";
        r.effReviewer = rev ? (rev !== r && rev.include.checked ? rev.name.value.trim() : "")
          : existing.get(r.reviewerName.toLowerCase()) || "";
        // optiunile: agentii noi inclusi, apoi (modul folder) grupul "Existing team"
        const opts = (first) => [el("option", { value: "" }, first),
          ...included.filter((o) => o !== r).map((o) => el("option", { value: o.name.value.trim() }, o.name.value.trim())),
          ...(existing.size ? [el("optgroup", { label: "Existing team" },
            [...existing.values()].map((n) => el("option", { value: n }, n)))] : [])];
        r.parent.replaceChildren(...opts("You"));
        r.parent.value = r.effParent;
        r.reviewer.replaceChildren(...opts("nobody"));
        r.reviewer.value = r.effReviewer;
      }
      drawTree(included);
    }
    function drawTree(included) {
      const lines = ["You"];
      const seen = new Set(); // protectie: doi agenti cu acelasi nume ar face bucla
      // copiii lui parent: agentii inclusi cu acel sef efectiv ("" = radacinile, sub Tu)
      const walk = (parent, depth) => {
        for (const r of included.filter((x) => x.effParent.toLowerCase() === parent.toLowerCase())) {
          if (seen.has(r)) continue;
          seen.add(r);
          lines.push("  ".repeat(depth) + "\u2514 " + r.name.value.trim() + (r.role.value ? " \u00b7 " + r.role.value : "") +
            (many ? "  [" + folders[Number(r.folder.value)].name.value.trim() + "]" : "") +
            (r.effReviewer ? "  (reviewed by " + r.effReviewer + ")" : ""));
          walk(r.name.value.trim(), depth + 1);
        }
      };
      walk("", 1);
      // modul folder: agentii noi care raporteaza unui agent existent, sub el
      const bosses = new Set(included.map((r) => r.effParent).filter((n) => existing.has(n.toLowerCase())));
      for (const b of bosses) {
        lines.push("  \u2514 " + b + "  (existing)");
        walk(b, 2);
      }
      tree.textContent = lines.join("\n");
    }
    for (const r of rows) {
      r.include.addEventListener("change", sync);
      r.parent.addEventListener("change", () => { r.parentName = r.parent.value; sync(); });
      r.reviewer.addEventListener("change", () => { r.reviewerName = r.reviewer.value; sync(); });
      r.name.addEventListener("change", () => {
        // redenumirea pastreaza legaturile celorlalti spre acest agent. Un nume gol
        // nu e o redenumire: altfel "" ar potrivi toate radacinile si toti fara reviewer.
        const old = r.oldName, now = r.name.value.trim();
        if (!now) { sync(); return; }
        if (old && old !== now) {
          for (const o of rows) {
            if (o.parentName.toLowerCase() === old.toLowerCase()) o.parentName = now;
            if (o.reviewerName.toLowerCase() === old.toLowerCase()) o.reviewerName = now;
          }
        }
        r.oldName = now;
        sync();
      });
      r.role.addEventListener("input", () => drawTree(rows.filter((x) => x.include.checked)));
      r.folder?.addEventListener("change", () => drawTree(rows.filter((x) => x.include.checked)));
    }
    // numele unui folder se schimba: etichetele din selectele agentilor le urmeaza
    folders.forEach((f, i) => f.name.addEventListener("input", () => {
      for (const r of rows) if (r.folder) r.folder.options[i].textContent = f.name.value;
      drawTree(rows.filter((x) => x.include.checked));
    }));

    const createBtn = el("button", { type: "button", class: "btn primary", onclick: create }, target ? "Add agents" : "Create project");
    body.replaceChildren(el("div", { class: "wizard-review" },
      fm ? fm.sections : null,
      fm ? null : el("section", { class: "section" },
        el("h2", {}, "3. Proposal"),
        field("Name", name), field("Goal", goal), field("Shared context", context),
        p.rationale && el("div", { class: "field" }, el("span", { class: "label" }, "Why this team"), el("p", { class: "muted" }, p.rationale))),
      folders.length > 0 && el("section", { class: "section" },
        el("h2", {}, many ? "Folders" : "Folder"),
        folders.map((f) => el("div", { class: "folder-row" },
          el("div", { class: "row" }, field("Name", f.name), el("div", { class: "field" }, el("span", { class: "label" }, "Path"), el("code", {}, f.path || "new folder"))),
          field("Context for this folder", f.context)))),
      el("section", { class: "section" },
        el("div", { class: "section-head" }, el("h2", {}, "Team"), el("span", { class: "hint" }, "Uncheck what you do not want; you can also adjust the links on the canvas after creation.")),
        tree, rows.map((r) => r.el)),
      el("div", { class: "actions wizard-actions" },
        el("button", { type: "button", class: "btn", onclick: back }, "Back"),
        el("span", { class: "grow" }), createBtn)));
    sync();

    // numele efectiv al folderului, ca in backend: numele scris, altfel ultimul
    // segment al caii, altfel "Main". Acelasi nume merge la folder si la agenti.
    function folderName(f) {
      const typed = f.name.value.trim();
      if (typed) return typed;
      const last = (f.path || "").split(/[\\/]/).filter(Boolean).pop();
      return last || "Main";
    }

    async function create() {
      const agents = rows.filter((r) => r.include.checked).map((r) => ({
        name: r.name.value.trim(), role: r.role.value.trim(), model: r.model.value,
        responsibilities: r.resp.value, reportsTo: r.effParent, reviewedBy: r.effReviewer,
        background: r.background.value, instructions: r.instructions.value, doneDef: r.doneDef.value,
        tools: r.tools.filter((t) => t.box.checked).map((t) => t.id),
        folder: r.folder ? folderName(folders[Number(r.folder.value)]) : "",
      }));
      createBtn.disabled = true;
      try {
        if (fm) { await createInFolder(p, agents, fm); return; }
        const proj = await api("/api/projects/setup", { method: "POST", body: {
          setupId: session?.id, workdir: form.workdirs[0] || "",
          proposal: { name: name.value, goal: goal.value, context: context.value, rationale: p.rationale || "",
            folders: folders.map((f) => ({ path: f.path, name: folderName(f), context: f.context.value })), agents },
        } });
        location.hash = `#/p/${proj.id}/team`;
      } catch (err) {
        toast(err.message);
        createBtn.disabled = false;
      }
    }
  }

  // reviewFolderSection: in modul folder, sectiunile de deasupra echipei:
  // motivatia (daca exista) si folderul analizat, cu contextul propus.
  function reviewFolderSection(p) {
    const ctxArea = el("textarea", { value: p.folders?.[0]?.context || "", rows: 6 });
    const hasCtx = Boolean((folder.context || "").trim());
    // un folder fara context il primeste implicit; unul cu context il pastreaza
    const updateCtx = el("input", { type: "checkbox", checked: !hasCtx });
    // editarea contextului bifeaza singura optiunea, altfel modificarile s-ar pierde
    ctxArea.addEventListener("input", () => { updateCtx.checked = true; }, { once: true });
    const sections = [
      p.rationale ? el("section", { class: "section" },
        el("h2", {}, "3. Proposal"),
        el("div", { class: "field" }, el("span", { class: "label" }, "Why these agents"), el("p", { class: "muted" }, p.rationale))) : null,
      el("section", { class: "section" },
        el("h2", {}, "Folder"),
        el("div", { class: "folder-row" },
          el("div", { class: "row" },
            el("div", { class: "field" }, el("span", { class: "label" }, "Name"), el("strong", {}, folder.name)),
            el("div", { class: "field" }, el("span", { class: "label" }, "Path"), el("code", {}, folder.path))),
          field("Context for this folder", ctxArea),
          el("label", { class: "check" }, updateCtx, "Update folder context"),
          hasCtx ? el("span", { class: "hint" }, "Current context is kept unless you tick this.") : null)),
    ];
    return { sections, ctxArea, updateCtx };
  }

  // createInFolder: adauga agentii noi in proiectul existent, in folderul analizat
  async function createInFolder(p, agents, fm) {
    await api(`/api/projects/${project.id}/agents/setup`, { method: "POST", body: {
      setupId: session?.id, folderId: folder.id, updateContext: fm.updateCtx.checked,
      proposal: { name: project.name, goal: "", context: "", rationale: p.rationale || "",
        folders: [{ path: folder.path, name: folder.name, context: fm.ctxArea.value }], agents },
    } });
    location.hash = `#/p/${project.id}/team`;
  }

  if (id) {
    if (loadErr) {
      toast(loadErr.message);
      history.replaceState(null, "", "#/setup");
      stepChoose();
      return cleanup;
    }
    Object.assign(form, { workdirs: session.workdirs || [], description: session.description, model: session.model || form.model });
    if (session.status === "running") stepAnalyze();
    else if (session.status === "ready") stepReview(session.proposal);
    else stepFailed(session);
  } else {
    stepChoose();
  }
  return cleanup;
}
