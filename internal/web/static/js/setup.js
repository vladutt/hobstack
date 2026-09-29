import { api } from "./api.js";
import { el, field, toast, TOOLS } from "./ui.js";
import { liveRenderer } from "./live.js";

// Wizard-ul "Proiect nou cu AI": 1) folder + descriere, 2) analiza live,
// 3) propunerea editabila, apoi "Creeaza proiectul".
export async function setupView(root) {
  const info = await api("/api/info");
  const form = { workdir: "", description: "", model: "sonnet" };
  let session = null;   // {id, runId}
  let stream = null;    // EventSource-ul analizei
  let closed = false;

  const body = el("div", { class: "wizard" });
  root.append(
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" }, el("a", { href: "#/" }, "Proiecte"), " / Proiect nou cu AI"),
        el("h1", {}, "Proiect nou cu AI"),
        el("p", { class: "muted" }, "Alegi un folder (sau descrii proiectul), un AI il citeste si propune proiectul si echipa. Tu decizi ce pastrezi."))),
    body);

  // ---------- pasul 1 ----------
  function stepChoose() {
    const workdir = el("input", { type: "text", value: form.workdir, placeholder: "ex. ~/code/shop (optional)" });
    const pickBtn = info.canPickFolder && el("button", { type: "button", class: "btn", onclick: pick }, "Alege...");
    const description = el("textarea", { value: form.description, rows: 5, maxlength: 4000,
      placeholder: "Ce vrei sa faci? ex. refactor la checkout si teste pentru plati. Obligatoriu daca nu alegi folder." });
    const model = el("select", { value: form.model },
      (info.models || []).filter((m) => m.group === "latest").map((m) => el("option", { value: m.id }, m.label)));
    const go = el("button", { type: "submit", class: "btn primary" }, "Analizeaza");
    body.replaceChildren(el("form", { class: "section wizard-step", onsubmit: start },
      el("h2", {}, "1. Ce analizam"),
      field("Folder de proiect", el("div", { class: "input-row" }, workdir, pickBtn),
        "Folderul nu se modifica: AI-ul doar citeste. Gol = proiect nou, folderul se creeaza la final."),
      field("Ce vrei sa faci?", description),
      field("Model pentru analiza", model, "Sonnet e un echilibru bun; Haiku e mai rapid."),
      el("div", { class: "actions" }, go)));

    async function pick() {
      try {
        const r = await api("/api/pick-folder", { method: "POST" });
        if (r?.path) workdir.value = r.path;
      } catch (err) { toast(err.message); }
    }
    async function start(e) {
      e.preventDefault();
      Object.assign(form, { workdir: workdir.value.trim(), description: description.value.trim(), model: model.value });
      go.disabled = true;
      try {
        session = await api("/api/setup", { method: "POST", body: form });
        stepAnalyze();
      } catch (err) {
        toast(err.message);
        go.disabled = false;
      }
    }
  }

  // ---------- pasul 2 ----------
  function stepAnalyze() {
    const box = el("div", { class: "live wizard-live" }, el("div", { class: "live-head" }, "AI-ul citeste proiectul..."));
    const stopBtn = el("button", { type: "button", class: "btn", onclick: () => api(`/api/setup/${session.id}/stop`, { method: "POST" }).catch((e) => toast(e.message)) }, "Opreste");
    body.replaceChildren(el("section", { class: "section wizard-step" },
      el("h2", {}, "2. Analiza"), box, el("div", { class: "actions" }, stopBtn)));
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
    body.replaceChildren(el("section", { class: "section wizard-step" },
      el("h2", {}, s.status === "stopped" ? "Analiza oprita" : "Analiza nu a mers"),
      s.error && el("p", { class: "err" }, s.error),
      el("div", { class: "actions" },
        el("button", { type: "button", class: "btn", onclick: stepChoose }, "Inapoi"),
        el("button", { type: "button", class: "btn primary", onclick: async () => {
          try { session = await api("/api/setup", { method: "POST", body: form }); stepAnalyze(); } catch (err) { toast(err.message); }
        } }, "Reincearca"))));
  }

  // ---------- pasul 3 ----------
  function stepReview(p) {
    const name = el("input", { type: "text", value: p.name, maxlength: 80 });
    const goal = el("textarea", { value: p.goal, rows: 3 });
    const context = el("textarea", { value: p.context, rows: 8 });
    const tree = el("pre", { class: "wizard-tree" });
    // un rand per agent propus, cu referinte la input-uri
    const rows = p.agents.map((a) => {
      const r = {
        include: el("input", { type: "checkbox", checked: true }),
        name: el("input", { type: "text", value: a.name, maxlength: 40 }),
        role: el("input", { type: "text", value: a.role, maxlength: 80 }),
        model: el("select", { value: a.model }, (info.models || []).map((m) => el("option", { value: m.id }, m.label))),
        resp: el("textarea", { value: a.responsibilities, rows: 2 }),
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
        el("div", { class: "row" }, field("Nume", r.name), field("Rol", r.role)),
        el("div", { class: "row" }, field("Model", r.model), field("Ce ia", r.resp)),
        el("div", { class: "row" }, field("Raporteaza lui", r.parent), field("Verificat de", r.reviewer)),
        el("div", { class: "field" }, el("span", { class: "label" }, "Tool-uri"),
          el("div", { class: "wizard-tools" }, r.tools.map((t) => el("label", { class: "check" }, t.box, t.label)))),
        el("details", {}, el("summary", {}, "Prompt"),
          field("Background", r.background), field("Instructiuni", r.instructions), field("Definition of done", r.doneDef)));
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
        let boss = byName.get(r.parentName.toLowerCase());
        for (let i = 0; boss && !boss.include.checked && i < rows.length; i++) boss = byName.get(boss.parentName.toLowerCase());
        const rev = byName.get(r.reviewerName.toLowerCase());
        r.effParent = boss && boss !== r && boss.include.checked ? boss.name.value.trim() : "";
        r.effReviewer = rev && rev !== r && rev.include.checked ? rev.name.value.trim() : "";
        const opts = (first) => [el("option", { value: "" }, first),
          ...included.filter((o) => o !== r).map((o) => el("option", { value: o.name.value.trim() }, o.name.value.trim()))];
        r.parent.replaceChildren(...opts("Tu"));
        r.parent.value = r.effParent;
        r.reviewer.replaceChildren(...opts("nimeni"));
        r.reviewer.value = r.effReviewer;
      }
      drawTree(included);
    }
    function drawTree(included) {
      const lines = ["Tu"];
      const seen = new Set(); // protectie: doi agenti cu acelasi nume ar face bucla
      // copiii lui parent: agentii inclusi cu acel sef efectiv ("" = radacinile, sub Tu)
      const walk = (parent, depth) => {
        for (const r of included.filter((x) => x.effParent.toLowerCase() === parent.toLowerCase())) {
          if (seen.has(r)) continue;
          seen.add(r);
          lines.push("  ".repeat(depth) + "\u2514 " + r.name.value.trim() + (r.role.value ? " \u00b7 " + r.role.value : "") +
            (r.effReviewer ? "  (verificat de " + r.effReviewer + ")" : ""));
          walk(r.name.value.trim(), depth + 1);
        }
      };
      walk("", 1);
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
    }

    const createBtn = el("button", { type: "button", class: "btn primary", onclick: create }, "Creeaza proiectul");
    body.replaceChildren(el("div", { class: "wizard-review" },
      el("section", { class: "section" },
        el("h2", {}, "3. Propunerea"),
        field("Nume", name), field("Obiectiv", goal), field("Context comun", context),
        p.rationale && el("div", { class: "field" }, el("span", { class: "label" }, "De ce aceasta echipa"), el("p", { class: "muted" }, p.rationale))),
      el("section", { class: "section" },
        el("div", { class: "section-head" }, el("h2", {}, "Echipa"), el("span", { class: "hint" }, "Debifeaza ce nu vrei; ajustezi legaturile si pe canvas dupa creare.")),
        tree, rows.map((r) => r.el)),
      el("div", { class: "actions wizard-actions" },
        el("button", { type: "button", class: "btn", onclick: stepChoose }, "Inapoi"),
        el("span", { class: "grow" }), createBtn)));
    sync();

    async function create() {
      const agents = rows.filter((r) => r.include.checked).map((r) => ({
        name: r.name.value.trim(), role: r.role.value.trim(), model: r.model.value,
        responsibilities: r.resp.value, reportsTo: r.effParent, reviewedBy: r.effReviewer,
        background: r.background.value, instructions: r.instructions.value, doneDef: r.doneDef.value,
        tools: r.tools.filter((t) => t.box.checked).map((t) => t.id),
      }));
      createBtn.disabled = true;
      try {
        const proj = await api("/api/projects/setup", { method: "POST", body: {
          workdir: form.workdir,
          proposal: { name: name.value, goal: goal.value, context: context.value, rationale: p.rationale || "", agents },
        } });
        location.hash = `#/p/${proj.id}/echipa`;
      } catch (err) {
        toast(err.message);
        createBtn.disabled = false;
      }
    }
  }

  stepChoose();
  return () => { closed = true; closeStream(); };
}
