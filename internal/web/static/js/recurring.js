import { api } from "./api.js";
import { el, field, toast, clock } from "./ui.js";

// Etichetele starilor unui task (pentru link-ul spre ultimul task creat).
const STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
};

// Zilele saptamanii in ordinea din formular (Mon..Sun) si bitul fiecareia (duminica = 0).
const DAYS = [
  { label: "Mon", bit: 1 << 1 }, { label: "Tue", bit: 1 << 2 }, { label: "Wed", bit: 1 << 3 },
  { label: "Thu", bit: 1 << 4 }, { label: "Fri", bit: 1 << 5 }, { label: "Sat", bit: 1 << 6 },
  { label: "Sun", bit: 1 << 0 },
];
const WEEKDAYS = 62; // Mon..Fri

let formSeq = 0; // nume unic pentru grupul de radio-uri al fiecarui formular

// when: "Today 14:00", "Tomorrow 09:00" sau "Oct 6 09:00" (ora locala)
function when(iso) {
  const d = new Date(iso);
  const day = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const today = day(new Date());
  const diff = Math.round((day(d) - today) / 86400000);
  if (diff === 0) return "Today " + clock(iso);
  if (diff === 1) return "Tomorrow " + clock(iso);
  return d.toLocaleDateString("en-US", { month: "short", day: "numeric" }) + " " + clock(iso);
}

// recurringPanel: panoul Recurring de deasupra board-ului (lista + formular in acelasi loc).
// - assignable(stageId) -> agentii care pot lua un task (din workspace)
// - onChanged(): reincarca proiectul dupa o actiune reusita
// Intoarce { el, update(project) }; cat e deschis formularul, update nu redeseneaza nimic.
export function recurringPanel({ projectId, project, assignable, onClose, onChanged }) {
  let current = project;
  let editing = false; // formularul e deschis
  const body = el("div", { class: "rec-body" });
  const newBtn = el("button", { type: "button", class: "btn small", onclick: () => openForm(null) }, "New recurring task");
  const box = el("section", { class: "section recurring-panel" },
    el("div", { class: "section-head" },
      el("h2", {}, "Recurring tasks"),
      el("div", { class: "actions" },
        newBtn,
        el("button", { type: "button", class: "btn small", onclick: onClose }, "Close"))),
    body);

  const firstStage = () => (current.stages || [])[0] || null;

  // run: executa o actiune, apoi reincarca proiectul; erorile ajung in toast
  async function run(fn) {
    try {
      await fn();
      await onChanged();
    } catch (err) {
      toast(err.message);
      if (!editing) renderList(current); // comutatorul revine la starea reala
    }
  }

  // bodyOf: corpul complet pentru PUT, din view; patch suprascrie campuri
  const bodyOf = (r, patch = {}) => ({
    title: r.title, description: r.description, assigneeId: r.assigneeId || 0,
    schedule: r.schedule, autoAccept: r.autoAccept, enabled: r.enabled, ...patch,
  });

  function renderList(p) {
    current = p;
    newBtn.hidden = false;
    const list = p.recurring || [];
    if (!list.length) {
      body.replaceChildren(el("p", { class: "hint" },
        "No recurring tasks yet. They create a task on the board on a schedule, for example every weekday at 09:00."));
      return;
    }
    const first = firstStage();
    body.replaceChildren(el("div", { class: "rec-list" }, list.map((r) => {
      const who = r.assigneeId ? (r.assignee || "?") : (first ? "First free agent in " + first.name : "?");
      return el("div", { class: "rec-row" + (r.enabled ? "" : " off") },
        el("div", { class: "rec-main" },
          el("strong", { class: "rec-title" }, r.title),
          el("div", { class: "rec-info" },
            el("span", {}, r.scheduleText),
            el("span", {}, who),
            el("span", { class: r.enabled ? "" : "hint" }, r.enabled ? "Next: " + when(r.nextRunAt) : "Off"),
            r.lastTask ? el("a", { href: `#/p/${projectId}/tasks/${r.lastTask.id}` },
              `T-${r.lastTask.number} · ${STATUS[r.lastTask.status] || r.lastTask.status}`) : null,
            r.autoAccept ? el("span", { class: "hint" }, "auto-accept") : null),
          r.lastNote ? el("span", { class: r.lastNote.startsWith("Skipped") ? "hint" : "rec-error" }, r.lastNote) : null),
        el("div", { class: "rec-actions" },
          el("label", { class: "check rec-toggle" },
            el("input", { type: "checkbox", "aria-label": "Enabled", checked: r.enabled,
              onchange: (e) => run(() => api(`/api/recurring/${r.id}`, { method: "PUT", body: bodyOf(r, { enabled: e.target.checked }) })) })),
          el("button", { type: "button", class: "btn small", onclick: () => run(async () => {
            const task = await api(`/api/recurring/${r.id}/run`, { method: "POST" });
            toast("Created T-" + task.number, "ok");
          }) }, "Run now"),
          el("button", { type: "button", class: "btn small", onclick: () => openForm(r) }, "Edit"),
          el("button", { type: "button", class: "btn small danger", onclick: () => {
            if (!confirm(`Delete "${r.title}"? Tasks it already created stay.`)) return;
            run(() => api(`/api/recurring/${r.id}`, { method: "DELETE" }));
          } }, "Delete")));
    })));
  }

  // openForm: formularul pentru o sarcina noua (r = null) sau pentru editare, in locul listei
  function openForm(r) {
    editing = true;
    newBtn.hidden = true;
    const first = firstStage();
    const choices = assignable(first?.id);
    const s = r ? r.schedule : { kind: "daily", everyHours: 1, at: "09:00", weekdays: WEEKDAYS };

    const title = el("input", { type: "text", maxlength: 200, required: true, value: r ? r.title : "", placeholder: "e.g. Check dependencies for updates" });
    const description = el("textarea", { rows: 3, value: r ? r.description : "", placeholder: "What the agent should do each time (optional)" });
    const assigned = r?.assigneeId || 0;
    // fara etape nu exista optiunea "" (prima etapa): implicit primul agent
    const who = el("select", { value: assigned ? String(assigned) : first ? "" : String(choices[0]?.id ?? "") },
      first ? el("option", { value: "" }, `First free agent in ${first.name}`) : null,
      choices.map((a) => el("option", { value: a.id }, a.name + (a.role ? " · " + a.role : ""))),
      // asignatul nu mai e printre optiuni: il pastram selectat
      assigned && !choices.some((a) => a.id === assigned) ? el("option", { value: assigned }, r.assignee || "?") : null);

    // programul: trei variante, doar controalele celei alese sunt active
    const group = `rec-kind-${++formSeq}`;
    const radio = (kind) => el("input", { type: "radio", name: group, value: kind, checked: s.kind === kind, onchange: sync });
    const rHours = radio("every_hours"), rDaily = radio("daily"), rWeekly = radio("weekly");
    const hours = el("input", { type: "number", min: 1, max: 24, value: s.everyHours || 1, "aria-label": "Hours" });
    const dailyAt = el("input", { type: "time", value: s.kind === "daily" ? s.at : "09:00", "aria-label": "Daily time" });
    const weeklyAt = el("input", { type: "time", value: s.kind === "weekly" ? s.at : "09:00", "aria-label": "Weekly time" });
    let mask = s.kind === "weekly" ? s.weekdays : WEEKDAYS;
    const dayBtns = DAYS.map((d) => {
      const b = el("button", { type: "button", class: "btn small", "aria-pressed": String(!!(mask & d.bit)), onclick: () => {
        mask ^= d.bit;
        b.setAttribute("aria-pressed", String(!!(mask & d.bit)));
      } }, d.label);
      return b;
    });
    const kindOf = () => (rHours.checked ? "every_hours" : rWeekly.checked ? "weekly" : "daily");
    function sync() {
      const k = kindOf();
      hours.disabled = k !== "every_hours";
      dailyAt.disabled = k !== "daily";
      weeklyAt.disabled = k !== "weekly";
      for (const b of dayBtns) b.disabled = k !== "weekly";
    }
    sync();

    const auto = el("input", { type: "checkbox", checked: r ? r.autoAccept : false });
    const saveBtn = el("button", { type: "submit", class: "btn primary" }, "Save");
    const form = el("form", { class: "rec-form", onsubmit: submit },
      el("h3", {}, r ? "Edit recurring task" : "New recurring task"),
      field("Title", title), field("Description", description),
      field("Assignee", who),
      el("fieldset", { class: "rec-schedule" },
        el("legend", {}, "Schedule"),
        el("label", { class: "rec-opt" }, rHours, "Every", hours, "hours"),
        el("label", { class: "rec-opt" }, rDaily, "Daily at", dailyAt),
        el("label", { class: "rec-opt" }, rWeekly, "Weekly on",
          el("span", { class: "rec-days", role: "group", "aria-label": "Days" }, dayBtns), weeklyAt)),
      el("div", { class: "field" },
        el("label", { class: "check" }, auto, "Accept automatically when done"),
        el("span", { class: "hint" }, "Skips your Accept / Send back. Questions and blocked tasks still reach you.")),
      el("div", { class: "actions" }, el("span", { class: "grow" }),
        el("button", { type: "button", class: "btn", onclick: back }, "Cancel"), saveBtn));
    body.replaceChildren(form);
    title.focus();

    // back: inchide formularul si arata lista proiectului curent
    function back() {
      editing = false;
      renderList(current);
    }

    async function submit(e) {
      e.preventDefault();
      if (!title.value.trim()) { toast("Title is required."); return; }
      const kind = kindOf();
      if (kind === "weekly" && !mask) { toast("Choose at least one day."); return; }
      const time = kind === "weekly" ? weeklyAt : dailyAt;
      const payload = {
        title: title.value.trim(), description: description.value.trim(),
        assigneeId: Number(who.value) || 0, autoAccept: auto.checked, enabled: r ? r.enabled : true,
        schedule: { kind, everyHours: kind === "every_hours" ? Number(hours.value) || 0 : 0,
          at: kind === "every_hours" ? "" : time.value, weekdays: kind === "weekly" ? mask : 0 },
      };
      saveBtn.disabled = true;
      try {
        if (r) await api(`/api/recurring/${r.id}`, { method: "PUT", body: payload });
        else await api(`/api/projects/${projectId}/recurring`, { method: "POST", body: payload });
        editing = false;
        await onChanged();
        if (!editing) renderList(current);
      } catch (err) {
        toast(err.message);
        saveBtn.disabled = false;
      }
    }
  }

  renderList(current);
  return {
    el: box,
    update(p) {
      current = p;
      if (!editing) renderList(p);
    },
  };
}
