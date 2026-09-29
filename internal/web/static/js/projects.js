import { api } from "./api.js";
import { el, avatar, pill, field, toast, kindOf } from "./ui.js";

export async function projectsView(root) {
  const projects = await api("/api/projects");
  root.append(
    el("header", { class: "page-head" },
      el("div", {},
        el("h1", {}, "Proiecte"),
        el("p", { class: "muted" }, "Fiecare proiect are echipa lui. Lead-ul planifica, delegheaza si verifica.")),
      el("a", { class: "btn primary", href: "#/new" }, "+ Proiect nou")),
    el("div", { class: "grid" },
      projects.map(projectCard),
      el("a", { class: "card card-new", href: "#/new" },
        el("span", { class: "plus" }, "+"),
        el("strong", {}, "Proiect nou"),
        el("span", { class: "hint" }, "Incepi cu un lead, adaugi echipa dupa"))));
}

function projectCard(p) {
  const pct = p.total ? Math.round((p.done / p.total) * 100) : 0;
  return el("a", { class: "card", href: `#/p/${p.id}` },
    el("div", { class: "card-top" }, el("span", { class: "card-title" }, p.name), pill(p.status)),
    el("p", { class: "muted clamp" }, p.goal || "Fara obiectiv inca."),
    el("div", { class: "avatars" },
      el("span", { class: "stack" }, p.team.map((m) => avatar(m.name, kindOf(m), 28))),
      el("span", { class: "hint" }, p.team.length === 1 ? "1 agent" : `${p.team.length} agenti`)),
    el("div", { class: "progress-row" },
      el("span", { class: "hint" }, p.total ? `${p.done} din ${p.total} task-uri gata` : "Niciun task"),
      el("div", { class: "bar" },
        el("div", { class: "bar-fill" + (p.status === "Running" ? " live" : ""), style: `width:${pct}%` }))));
}

export async function projectFormView(root, id) {
  const p = id
    ? await api(`/api/projects/${id}`)
    : { name: "", goal: "", context: "", workdir: "", maxParallel: 0, budgetUsd: 0 };

  const name = el("input", { type: "text", value: p.name, placeholder: "ex. HoReCa QR", required: true, maxlength: 80 });
  const goal = el("textarea", { value: p.goal, placeholder: "Un MVP si un prototip clickable pentru inscrieri prin QR." });
  const context = el("textarea", { value: p.context, rows: 7, placeholder: "Stack, conventii, ce e deja decis, ce e interzis." });
  const workdir = el("input", { id: "f-workdir", type: "text", value: p.workdir, placeholder: "gol = ~/Foreman/<nume proiect>" });
  // butonul "Alege..." apare doar daca serverul poate deschide dialogul nativ
  let canPick = false;
  try { canPick = (await api("/api/info")).canPickFolder; } catch { /* fara buton */ }
  const pickBtn = canPick && el("button", { type: "button", class: "btn", onclick: pickFolder }, "Alege...");
  // 0 = fara limita; in formular apare ca un camp gol
  const parallel = el("input", { type: "number", min: 1, max: 100, value: p.maxParallel || "", placeholder: "fara limita" });
  const budget = el("input", { type: "number", min: 0.05, max: 100, step: 0.05, value: p.budgetUsd || "", placeholder: "fara limita" });

  // eticheta trebuie sa arate spre input, nu spre containerul cu buton
  const folderField = field("Folder de lucru", el("div", { class: "input-row" }, workdir, pickBtn),
    "Optional. Daca il lasi gol, se creeaza automat. O cale care nu exista se creeaza.");
  folderField.querySelector("label").setAttribute("for", "f-workdir");

  root.append(el("form", { class: "form", onsubmit: save },
    el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" },
          el("a", { href: "#/" }, "Proiecte"), " / ",
          id ? el("a", { href: `#/p/${id}` }, p.name) : "Proiect nou"),
        el("h1", {}, id ? "Instructiuni proiect" : "Proiect nou")),
      el("div", { class: "actions" },
        id && el("button", { type: "button", class: "btn danger", onclick: remove }, "Sterge proiectul"),
        el("a", { class: "btn", href: id ? `#/p/${id}` : "#/" }, "Renunta"),
        el("button", { type: "submit", class: "btn primary" }, id ? "Salveaza" : "Creeaza proiectul"))),
    el("div", { class: "form-cols" },
      el("section", { class: "section" },
        el("h2", {}, "Brief"),
        field("Nume", name),
        field("Obiectiv: cum arata gata", goal),
        field("Context comun: il citeste fiecare agent", context),
        folderField),
      el("section", { class: "section" },
        el("h2", {}, "Cum lucreaza echipa"),
        el("div", { class: "row" },
          field("Workeri in paralel", parallel),
          field("Buget per run (USD)", budget)),
        el("p", { class: "hint" }, "Ambele sunt optionale: lasa-le goale pentru fara limita. Bugetul conteaza doar daca folosesti API key (cu abonament nu platesti per run); cand e setat, un run se opreste la atingerea lui. Agentii care iti raporteaza direct nu intra in limita."),
        !id && el("p", { class: "hint" }, "Primesti un lead implicit. Workerii ii adaugi din pagina proiectului.")))));
  name.focus();

  async function pickFolder() {
    pickBtn.disabled = true;
    pickBtn.textContent = "Se deschide...";
    try {
      const r = await api("/api/pick-folder", { method: "POST" });
      if (r && r.path) workdir.value = r.path; // 204 = a renuntat
    } catch (err) {
      toast(err.message);
    } finally {
      pickBtn.disabled = false;
      pickBtn.textContent = "Alege...";
    }
  }

  async function save(e) {
    e.preventDefault();
    const body = {
      name: name.value, goal: goal.value, context: context.value, workdir: workdir.value,
      maxParallel: Number(parallel.value) || 0, budgetUsd: Number(budget.value) || 0,
    };
    try {
      const saved = id
        ? await api(`/api/projects/${id}`, { method: "PUT", body })
        : await api("/api/projects", { method: "POST", body });
      location.hash = `#/p/${saved.id}`;
    } catch (err) {
      toast(err.message);
    }
  }

  async function remove() {
    if (!confirm(`Stergi proiectul ${p.name}? Task-urile si conversatiile se pierd. Folderul de lucru ramane neatins.`)) return;
    try {
      await api(`/api/projects/${id}`, { method: "DELETE" });
      location.hash = "#/";
    } catch (err) {
      toast(err.message);
    }
  }
}
