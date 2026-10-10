import { api } from "./api.js";
import { el, toast, clock } from "./ui.js";
import { renderMarkdown } from "./md.js";
import { taskFilesView } from "./task-files.js";

// Etichetele starilor (ca pe board); o rulare arhivata apare ca "Deleted".
const STATUS = {
  queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done",
};
const PAGE = 20;
const EXCERPT = 300;

// day: "Oct 9 14:00" (ora locala)
function day(iso) {
  return new Date(iso).toLocaleDateString("en-US", { month: "short", day: "numeric" }) + " " + clock(iso);
}

// excerpt: primul paragraf din raport, max 300 de caractere
function excerpt(text) {
  const first = text.trim().split(/\n\s*\n/)[0].trim();
  return first.length > EXCERPT ? first.slice(0, EXCERPT).trimEnd() + "…" : first;
}

// recurringHistory: modala Run history a unei sarcini recurente, peste board.
// - onClose(): inchide (prin adresa); onEdit(rec): deschide formularul sarcinii
// Intoarce { recurringId, refresh, destroy }; refresh reincarca fara sa piarda ce e desfasurat.
export function recurringHistory({ projectId, recurringId, onClose, onEdit }) {
  let destroyed = false;
  let rec = null;          // sarcina (recurringView)
  let counts = null;
  let runs = [];           // rularile incarcate
  let more = false;
  let loadingMore = false;
  const expanded = new Set(); // id-urile rularilor cu raportul intreg

  const titleId = "rh-title-" + recurringId;
  const title = el("h2", { class: "modal-title", id: titleId }, "Run history");
  const closeBtn = el("button", { type: "button", class: "modal-close", "aria-label": "Close", onclick: () => onClose() }, "×");
  const files = taskFilesView({ limit: 10 });
  const filesSection = el("section", { class: "modal-section rh-files", hidden: true },
    el("h3", { class: "rh-head" }, "Files"), files.el);
  const runsList = el("ul", { class: "rh-runs" });
  const moreBtn = el("button", { type: "button", class: "btn ghost small", hidden: true, onclick: loadMore }, "Load more");
  const runsSection = el("section", { class: "modal-section" }, el("h3", { class: "rh-head" }, "Runs"), runsList, moreBtn);
  const body = el("div", { class: "modal-body" }, el("p", { class: "hint" }, "Loading..."));
  const side = el("aside", { class: "modal-side", "aria-label": "Recurring task details" });
  const dialog = el("div", { class: "modal", role: "dialog", "aria-modal": "true", "aria-labelledby": titleId, tabindex: "-1" },
    el("div", { class: "modal-main" },
      // header ca la modala de task: crumbs cu X, apoi titlul
      el("div", { class: "modal-head" }, el("div", { class: "modal-head-main" },
        el("div", { class: "modal-crumbs" }, el("span", {}, "Recurring task"), el("span", { class: "grow" }), closeBtn), title)),
      body),
    side);
  const overlay = el("div", { class: "modal-overlay" }, dialog);
  let downOnOverlay = false;
  overlay.addEventListener("mousedown", (e) => { downOnOverlay = e.target === overlay; });
  overlay.addEventListener("click", (e) => { if (downOnOverlay && e.target === overlay) onClose(); });
  const onKey = (e) => { if (e.key === "Escape") { e.preventDefault(); onClose(); } };
  document.addEventListener("keydown", onKey);
  const prevOverflow = document.body.style.overflow;
  document.body.style.overflow = "hidden";
  document.body.append(overlay);
  dialog.focus();
  load();

  // load: prima pagina (sau cate rulari erau incarcate, max 50), totalurile si fisierele
  async function load() {
    const limit = Math.min(Math.max(runs.length, PAGE), 50);
    try {
      const [h, f] = await Promise.all([
        api(`/api/recurring/${recurringId}/history?limit=${limit}`),
        api(`/api/recurring/${recurringId}/files`),
      ]);
      if (destroyed) return;
      rec = h.recurring; counts = h.counts; runs = h.runs; more = h.more;
      files.show(f);
      filesSection.hidden = !f.files.length && !f.user.length;
      render();
    } catch (err) {
      if (destroyed) return;
      const msg = err.status === 404 ? "This recurring task no longer exists." : `Could not load the history: ${err.message}`;
      body.replaceChildren(el("p", { class: "hint" }, msg));
      side.replaceChildren();
    }
  }

  async function loadMore() {
    if (loadingMore || !runs.length) return;
    loadingMore = true;
    moreBtn.disabled = true;
    try {
      const h = await api(`/api/recurring/${recurringId}/history?limit=${PAGE}&before=${runs[runs.length - 1].id}`);
      if (destroyed) return;
      runs = runs.concat(h.runs);
      more = h.more;
      renderRuns();
    } catch (err) {
      toast(err.message);
    } finally {
      loadingMore = false;
      moreBtn.disabled = false;
    }
  }

  async function runNow() {
    try {
      const task = await api(`/api/recurring/${recurringId}/run`, { method: "POST" });
      toast("Created T-" + task.number, "ok");
      await load();
    } catch (err) {
      toast(err.message);
    }
  }

  function render() {
    title.textContent = rec.title;
    body.replaceChildren(filesSection, runsSection);
    renderRuns();
    renderSide();
  }

  function renderSide() {
    const parts = [`${counts.total} ${counts.total === 1 ? "run" : "runs"}`, `${counts.done} done`];
    if (counts.open) parts.push(`${counts.open} open`);
    if (counts.archived) parts.push(`${counts.archived} deleted`);
    const row = (label, value) => el("div", { class: "rh-side-row" }, el("span", { class: "hint" }, label), el("span", {}, value));
    // replaceChildren scrie "null" pentru un copil null: filtram randurile lipsa
    side.replaceChildren(...[
      row("Schedule", rec.scheduleText),
      row("Assignee", rec.assignee || "First stage"),
      row("Next run", rec.enabled ? day(rec.nextRunAt) : "Off"),
      rec.autoAccept ? row("Auto-accept", "On") : null,
      row("Runs", parts.join(" · ")),
      rec.lastNote ? el("p", { class: rec.lastNote.startsWith("Skipped") ? "hint" : "rec-error" }, rec.lastNote) : null,
      el("div", { class: "rh-side-actions" },
        el("button", { type: "button", class: "btn small", onclick: runNow }, "Run now"),
        el("button", { type: "button", class: "btn small", onclick: () => onEdit(rec) }, "Edit")),
    ].filter(Boolean));
  }

  function renderRuns() {
    if (!runs.length) {
      runsList.replaceChildren(el("li", { class: "hint" }, "No runs yet."));
    } else {
      runsList.replaceChildren(...runs.map(runRow));
    }
    moreBtn.hidden = !more;
  }

  function runRow(r) {
    const archived = r.status === "archived";
    const isOpen = expanded.has(r.id);
    const none = r.status === "done" || archived ? "No report." : "No report yet.";
    const report = !r.result ? el("p", { class: "hint rh-excerpt" }, none)
      : isOpen ? renderMarkdown(r.result) : el("p", { class: "rh-excerpt" }, excerpt(r.result));
    return el("li", { class: "rh-run" + (archived ? " archived" : "") },
      el("div", { class: "rh-run-head" },
        el("button", { type: "button", class: "rh-toggle", "aria-expanded": String(isOpen), disabled: !r.result,
          onclick: () => { if (isOpen) expanded.delete(r.id); else expanded.add(r.id); renderRuns(); } },
          el("span", { class: "rh-when" }, day(r.createdAt))),
        el("a", { class: "task-id", href: `#/p/${projectId}/tasks/${r.id}` }, r.ref),
        archived ? el("span", { class: "pill" }, "Deleted")
          : el("span", { class: `pill pill-task-${r.status}` }, STATUS[r.status] || r.status),
        el("span", { class: "hint" }, r.assignee),
        r.files ? el("span", { class: "task-meta", title: `${r.files} ${r.files === 1 ? "file" : "files"}` }, `📎 ${r.files}`) : null),
      report);
  }

  return {
    recurringId,
    refresh() { if (!destroyed && !loadingMore) load(); },
    destroy() {
      destroyed = true;
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prevOverflow;
      overlay.remove();
    },
  };
}
