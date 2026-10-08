import { api } from "./api.js";
import { el, toast, clock, avatar, kindOf as kindOf_ } from "./ui.js";

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

  // openForm: formularul pentru o sarcina noua (r = null) sau pentru editare, intr-o
  // fereastra peste board: in stanga formularul, in dreapta cum apare cardul, urmatoarele
  // rulari si sarcinile recurente existente.
  function openForm(r) {
    editing = true;
    const first = firstStage();
    const choices = assignable(first?.id);
    const s = r ? r.schedule : { kind: "weekly", everyHours: 1, at: "09:00", weekdays: WEEKDAYS };

    const title = el("input", { type: "text", maxlength: 200, required: true, value: r ? r.title : "", placeholder: "e.g. Check dependencies for updates", "aria-label": "Title" });
    const description = el("textarea", { rows: 3, value: r ? r.description : "", placeholder: "e.g. Run composer outdated and npm outdated. Open one task per package with a major bump; apply patch bumps, run tests, report.", "aria-label": "What the agent should do each time" });
    const assigned = r?.assigneeId || 0;
    // fara etape nu exista optiunea "" (prima etapa): implicit primul agent
    const who = el("select", { value: assigned ? String(assigned) : first ? "" : String(choices[0]?.id ?? ""), "aria-label": "Assignee" },
      first ? el("option", { value: "" }, `First free agent in ${first.name}`) : null,
      choices.map((a) => el("option", { value: a.id }, a.name + (a.role ? " · " + a.role : ""))),
      // asignatul nu mai e printre optiuni: il pastram selectat
      assigned && !choices.some((a) => a.id === assigned) ? el("option", { value: assigned }, r.assignee || "?") : null);

    // programul: "Every [day | weekday | selected days | N hours] at [ora]" + zilele
    let mask = s.kind === "weekly" ? s.weekdays : WEEKDAYS;
    const every = el("select", { class: "rec-every", "aria-label": "Repeat" },
      el("option", { value: "daily" }, "day"),
      el("option", { value: "weekday" }, "weekday"),
      el("option", { value: "weekly" }, "selected days"),
      el("option", { value: "every_hours" }, "few hours"));
    every.value = s.kind === "weekly" ? (s.weekdays === WEEKDAYS ? "weekday" : "weekly") : s.kind;
    const hours = el("input", { type: "number", min: 1, max: 24, value: s.everyHours || 1, class: "rec-hours", "aria-label": "Hours" });
    const at = el("input", { type: "time", value: s.at || "09:00", "aria-label": "Time" });
    const dayBtns = DAYS.map((d) => {
      const b = el("button", { type: "button", class: "rec-day", title: d.label, "aria-label": d.label, onclick: () => {
        mask ^= d.bit;
        if (every.value === "weekday" || every.value === "daily") every.value = "weekly";
        sync();
      } }, d.label.charAt(0));
      b.bit = d.bit;
      return b;
    });
    const hoursBox = el("span", { class: "rec-inline" }, hours, "hours");
    const atBox = el("span", { class: "rec-inline" }, "at", at);
    const days = el("div", { class: "rec-days", role: "group", "aria-label": "Days" }, dayBtns);
    const summary = el("span", { class: "rec-summary" });
    const kindOf = () => (every.value === "every_hours" ? "every_hours" : every.value === "daily" ? "daily" : "weekly");
    const maskOf = () => (every.value === "weekday" ? WEEKDAYS : every.value === "daily" ? 127 : mask);
    function sync() {
      const k = kindOf();
      hoursBox.hidden = k !== "every_hours";
      atBox.hidden = k === "every_hours";
      days.hidden = k === "every_hours";
      for (const b of dayBtns) b.setAttribute("aria-pressed", String(!!(maskOf() & b.bit)));
      summary.textContent = scheduleText(k, maskOf(), at.value, Number(hours.value) || 1);
      drawSide();
    }
    every.addEventListener("change", () => { if (every.value === "weekly" && !mask) mask = WEEKDAYS; sync(); });
    for (const n of [hours, at]) n.addEventListener("input", sync);

    const auto = el("input", { type: "checkbox", checked: r ? r.autoAccept : false });
    const saveBtn = el("button", { type: "submit", class: "btn primary" }, r ? "Save changes" : "Create recurring task");
    const runNow = r ? el("button", { type: "button", class: "btn ghost", onclick: () => run(async () => {
      const task = await api(`/api/recurring/${r.id}/run`, { method: "POST" });
      toast("Created T-" + task.number, "ok");
    }) }, "Run once now") : null;

    const card = el("div", { class: "rec-preview" });
    const nextList = el("div", { class: "rec-next" });
    const others = el("div", { class: "rec-others" });
    for (const n of [title, who]) n.addEventListener(n.tagName === "SELECT" ? "change" : "input", drawSide);

    const form = el("form", { class: "rec-form", onsubmit: submit },
      el("div", { class: "rec-form-head" }, el("h3", {}, r ? "Edit recurring task" : "New recurring task"),
        el("span", { class: "np-dim" }, `· creates a card in ${first ? first.name : "Queued"} on a schedule`)),
      el("div", { class: "field" }, el("label", { class: "label" }, "Title"), title),
      el("div", { class: "field" }, el("label", { class: "label" }, "What the agent should do each time"), description),
      el("div", { class: "field" }, el("label", { class: "label" }, "Assignee"), who,
        el("span", { class: "hint" }, first ? "Or leave it to the first free agent in the stage." : "The agent picks it up when the card appears.")),
      el("div", { class: "field" },
        el("span", { class: "label" }, "When"),
        el("div", { class: "rec-when" }, "Every", every, hoursBox, atBox),
        days,
        el("div", { class: "rec-sum-row" }, summary, el("span", { class: "np-dim" }, " · " + (Intl.DateTimeFormat().resolvedOptions().timeZone || "local time")))),
      el("label", { class: "check" }, auto, el("span", {}, "Accept automatically when done ",
        el("span", { class: "np-dim" }, "· skips your Accept / Send back; questions and blocked tasks still reach you"))),
      el("p", { class: "hint" }, "A run is skipped while the card from the previous one is still open."),
      el("div", { class: "actions" }, saveBtn, runNow, el("span", { class: "grow" }),
        el("button", { type: "button", class: "btn ghost", onclick: close }, "Cancel")));
    const dialog = el("div", { class: "modal rec-modal", role: "dialog", "aria-modal": "true", "aria-label": r ? "Edit recurring task" : "New recurring task", tabindex: "-1" },
      el("div", { class: "rec-modal-main" }, form),
      el("aside", { class: "rec-modal-side" },
        el("span", { class: "na-cap" }, `Appears in ${first ? first.name : "Queued"} as`), card,
        el("span", { class: "na-cap" }, "Next runs"), nextList,
        el("div", { class: "rec-others-box" }, others)));
    const overlay = el("div", { class: "modal-overlay" }, dialog);
    let downOnOverlay = false;
    overlay.addEventListener("mousedown", (e) => { downOnOverlay = e.target === overlay; });
    overlay.addEventListener("click", (e) => { if (downOnOverlay && e.target === overlay) close(); });
    const onKey = (e) => { if (e.key === "Escape") { e.preventDefault(); close(); } };
    document.addEventListener("keydown", onKey);
    document.body.append(overlay);
    sync();
    title.focus();

    // drawSide: cardul de pe board, urmatoarele 3 rulari si sarcinile existente
    function drawSide() {
      const agent = current.agents.find((a) => String(a.id) === who.value);
      card.replaceChildren(
        el("span", { class: "label-tag recurring" }, "↻ recurring"),
        el("strong", {}, title.value.trim() || "Untitled"),
        el("span", { class: "rec-preview-foot" }, el("span", { class: "task-id" }, r ? `R-${r.id}` : "R-new"),
          el("span", { class: "grow" }), agent ? avatar(agent.name, kindOf_(agent), 24) : el("span", { class: "avatar empty", style: "--size:24px" })));
      const runs = nextRuns(kindOf(), maskOf(), at.value, Number(hours.value) || 1, 3);
      nextList.replaceChildren(...(runs.length ? runs.map((d) => el("div", { class: "rec-next-row" },
        el("span", {}, d.toLocaleDateString("en-US", { weekday: "short", month: "short", day: "numeric" })),
        el("span", { class: "task-id" }, clock(d.toISOString()))))
        : [el("p", { class: "hint" }, "Pick at least one day.")]));
      const list = (current.recurring || []).filter((x) => x.id !== r?.id);
      others.replaceChildren(...(list.length ? [
        el("span", { class: "na-cap" }, `Existing · ${list.length}`),
        ...list.map((x) => el("div", { class: "rec-other" },
          el("span", { class: "dot" + (x.enabled ? " on" : "") }),
          el("strong", {}, x.title),
          el("span", { class: "grow" }),
          el("span", { class: "hint" }, [x.scheduleText, x.assignee, x.enabled ? "" : "paused"].filter(Boolean).join(" · "))))] : []));
    }

    // close: inchide fereastra si arata lista proiectului curent
    function close() {
      document.removeEventListener("keydown", onKey);
      overlay.remove();
      editing = false;
      renderList(current);
    }

    async function submit(e) {
      e.preventDefault();
      if (!title.value.trim()) { toast("Title is required."); return; }
      const kind = kindOf();
      const m = maskOf();
      if (kind === "weekly" && !m) { toast("Choose at least one day."); return; }
      // "in fiecare zi" ramane daily pe server; zilele alese sunt weekly
      const payload = {
        title: title.value.trim(), description: description.value.trim(),
        assigneeId: Number(who.value) || 0, autoAccept: auto.checked, enabled: r ? r.enabled : true,
        schedule: { kind, everyHours: kind === "every_hours" ? Number(hours.value) || 0 : 0,
          at: kind === "every_hours" ? "" : at.value, weekdays: kind === "weekly" ? m : 0 },
      };
      saveBtn.disabled = true;
      try {
        if (r) await api(`/api/recurring/${r.id}`, { method: "PUT", body: payload });
        else await api(`/api/projects/${projectId}/recurring`, { method: "POST", body: payload });
        close();
        await onChanged();
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


// scheduleText: "Monday to Friday at 09:00", "Every day at 09:00", "Every 3 hours"
function scheduleText(kind, mask, at, hours) {
  if (kind === "every_hours") return hours === 1 ? "Every hour" : `Every ${hours} hours`;
  if (kind === "daily" || mask === 127) return `Every day at ${at}`;
  if (mask === WEEKDAYS) return `Monday to Friday at ${at}`;
  const names = DAYS.filter((d) => mask & d.bit).map((d) => d.label);
  return names.length ? `${names.join(", ")} at ${at}` : "No days selected";
}

// nextRuns: urmatoarele n rulari dupa acum, in ora locala (doar pentru previzualizare;
// serverul isi calculeaza singur programul)
function nextRuns(kind, mask, at, hours, n) {
  const out = [];
  const now = new Date();
  if (kind === "every_hours") {
    const d = new Date(now);
    d.setMinutes(0, 0, 0);
    while (out.length < n) { d.setHours(d.getHours() + hours); out.push(new Date(d)); }
    return out;
  }
  const [h, m] = (at || "09:00").split(":").map(Number);
  const d = new Date(now);
  d.setHours(h, m, 0, 0);
  if (d <= now) d.setDate(d.getDate() + 1);
  for (let i = 0; i < 14 && out.length < n; i++) {
    if (kind === "daily" || mask & (1 << d.getDay())) out.push(new Date(d));
    d.setDate(d.getDate() + 1);
  }
  return out;
}
