// Tab-ul Memory al unui proiect: deciziile si faptele pe care agentii le vad in fiecare tura.
// Agentii le scriu prin MCP (remember / forget); aici utilizatorul le adauga, editeaza, blocheaza si sterge.
// Spec: docs/superpowers/specs/2026-11-27-hobstack-project-memory-design.md
import { api } from "./api.js";
import { el, avatar, toast } from "./ui.js";
import { renderMarkdown } from "./md.js";

const fold = (s) => String(s || "").normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();

function ago(iso) {
  if (!iso) return "";
  const d = new Date(iso);
  const mins = Math.max(0, (Date.now() - d.getTime()) / 60000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${Math.floor(mins)} min ago`;
  if (mins < 60 * 24) return `${Math.floor(mins / 60)} h ago`;
  if (mins < 60 * 48) return "yesterday";
  return d.toLocaleDateString("en-GB", { month: "short", day: "numeric" });
}

const ACTIONS = { set: "saved", forget: "deleted", restore: "restored", lock: "locked", unlock: "unlocked" };

// memoryView(projectId): { el, enter(id), refresh, destroy }
export function memoryView(projectId) {
  const state = { list: [], deleted: [], loaded: false, query: "", showDeleted: false, openId: null, modal: null };

  const search = el("input", {
    type: "search", class: "mem-search", placeholder: "Search memory", "aria-label": "Search memory",
    oninput: () => { state.query = search.value; paintList(); },
    onkeydown: (e) => { if (e.key === "Escape" && search.value) { search.value = ""; state.query = ""; paintList(); e.stopPropagation(); } },
  });
  const deletedBtn = el("button", { type: "button", class: "btn small ghost", onclick: () => { state.showDeleted = !state.showDeleted; paintList(); } });
  const addBtn = el("button", { type: "button", class: "btn small primary", onclick: () => openAdd() }, "+ Add");
  const listBox = el("div", { class: "mem-list" });
  const intro = el("p", { class: "hint mem-intro" }, "Decisions and facts every agent of this project sees in each turn. Agents add them as they work; you can add, edit, lock or delete them here.");
  const root = el("section", { class: "tab-body memory-body" },
    el("div", { class: "mem-toolbar" }, search, el("span", { class: "grow" }), deletedBtn, addBtn),
    intro, listBox);

  async function load() {
    try {
      const [list, deleted] = await Promise.all([
        api(`/api/projects/${projectId}/memory`),
        api(`/api/projects/${projectId}/memory?deleted=1`),
      ]);
      state.list = list || [];
      state.deleted = deleted || [];
      state.loaded = true;
    } catch (err) {
      toast(err.message);
      return;
    }
    paintList();
    // in timpul editarii nu se redeseneaza formularul (s-ar pierde textul scris)
    if (state.modal && !state.modal.editing) paintModal();
  }

  function find(id) {
    return state.list.find((m) => m.id === id) || state.deleted.find((m) => m.id === id);
  }

  function meta(m) {
    const out = [];
    out.push(m.authorAgentId ? avatar(m.authorName, "worker", 18) : el("span", { class: "mem-you" }, "You"));
    if (m.authorAgentId) out.push(el("span", {}, m.authorName));
    out.push(el("span", { class: "hint" }, m.deletedAt ? `deleted ${ago(m.deletedAt)}` : ago(m.updatedAt)));
    if (m.taskRef) {
      out.push(el("a", {
        class: "mem-task", href: `#/p/${projectId}/tasks/${m.taskId}`, title: "Open the task",
        onclick: (e) => e.stopPropagation(),
      }, m.taskRef));
    }
    return el("span", { class: "mem-meta" }, out);
  }

  function row(m) {
    const restore = m.deletedAt ? el("button", {
      type: "button", class: "btn small ghost", onclick: (e) => { e.stopPropagation(); restoreEntry(m); },
    }, "Restore") : null;
    return el("div", {
      class: "mem-row" + (m.deletedAt ? " deleted" : ""), role: "button", tabindex: "0",
      onclick: () => open(m.id),
      onkeydown: (e) => { if (e.key === "Enter") open(m.id); },
    },
    el("div", { class: "mem-row-head" },
      el("code", { class: "mem-key" }, m.key),
      m.locked ? el("span", { class: "mem-lock", title: "Locked: agents cannot change it" }, "🔒 Locked") : null,
      el("span", { class: "grow" }),
      meta(m), restore),
    el("div", { class: "mem-summary" }, m.summary));
  }

  function paintList() {
    deletedBtn.textContent = state.showDeleted ? "Back to memory" : `Deleted (${state.deleted.length})`;
    deletedBtn.hidden = !state.showDeleted && state.deleted.length === 0;
    addBtn.hidden = state.showDeleted;
    intro.hidden = !state.list.length || state.showDeleted; // starea goala are textul ei
    if (!state.loaded) { listBox.replaceChildren(el("p", { class: "empty" }, "Loading…")); return; }
    const src = state.showDeleted ? state.deleted : state.list;
    const q = fold(state.query).trim();
    const words = q ? q.split(/\s+/) : [];
    const shown = src.filter((m) => {
      const hay = fold(`${m.key} ${m.summary} ${m.body}`);
      return words.every((w) => hay.includes(w));
    });
    if (!src.length) {
      listBox.replaceChildren(state.showDeleted
        ? el("p", { class: "empty" }, "No deleted entries.")
        : el("div", { class: "empty-state mem-empty" },
          el("h2", {}, "No memory yet"),
          el("p", {}, "When an agent learns a rule (from a send-back, your decision or an architecture choice) it saves it here, and every agent sees it from then on."),
          el("button", { type: "button", class: "btn primary", onclick: () => openAdd() }, "+ Add the first entry")));
      return;
    }
    listBox.replaceChildren(...(shown.length ? shown.map(row) : [el("p", { class: "empty" }, "Nothing matches your search.")]));
  }

  // ---------- modala ----------
  function closeModal() {
    if (!state.modal) return;
    document.removeEventListener("keydown", state.modal.onKey, true);
    state.modal.overlay.remove();
    state.modal = null;
    state.openId = null;
    const want = `#/p/${projectId}/memory`;
    if (location.hash.startsWith(want + "/")) history.replaceState(null, "", want);
  }

  function showModal(label) {
    closeModal();
    const box = el("div", { class: "mem-modal", tabindex: "-1" });
    const overlay = el("div", {
      class: "modal-overlay", role: "dialog", "aria-modal": "true", "aria-label": label,
      onclick: (e) => { if (e.target === overlay) closeModal(); },
    }, box);
    const onKey = (e) => {
      if (e.key !== "Escape" || e.target.closest?.(".ps-overlay")) return; // switcher-ul (Cmd+K) are Esc-ul lui
      e.preventDefault();
      e.stopPropagation();
      if (state.modal?.editing) { state.modal.editing = false; paintModal(); } else closeModal();
    };
    document.addEventListener("keydown", onKey, true);
    document.body.append(overlay);
    state.modal = { overlay, box, onKey, editing: false, history: null };
    return box;
  }

  function open(id) {
    if (!state.loaded) { state.openId = id; load().then(() => { if (state.openId === id) open(id); }); return; }
    const m = find(id);
    if (!m) {
      history.replaceState(null, "", `#/p/${projectId}/memory`);
      toast("Memory entry not found");
      return;
    }
    showModal("Memory entry").focus();
    state.openId = id;
    const want = `#/p/${projectId}/memory/${id}`;
    if (location.hash !== want) history.replaceState(null, "", want);
    paintModal();
    loadHistory(id);
  }

  async function loadHistory(id) {
    try {
      const h = await api(`/api/memory/${id}/history`);
      if (state.modal && state.openId === id) { state.modal.history = h; paintModal(); }
    } catch {}
  }

  function paintModal() {
    const md = state.modal;
    if (!md || state.openId == null) return;
    const m = find(state.openId);
    if (!m) { closeModal(); return; }
    const head = el("div", { class: "mem-modal-head" },
      el("code", { class: "mem-key" }, m.key),
      m.locked ? el("span", { class: "mem-lock" }, "🔒 Locked") : null,
      m.deletedAt ? el("span", { class: "mem-deleted-tag" }, "Deleted") : null,
      el("span", { class: "grow" }),
      el("button", { type: "button", class: "drawer-close", "aria-label": "Close", onclick: closeModal }, "×"));
    let main;
    if (md.editing) {
      const summary = el("input", { type: "text", maxlength: "140", value: m.summary, "aria-label": "Summary" });
      const body = el("textarea", { rows: "8", maxlength: "4000", "aria-label": "Details" }, m.body);
      main = el("form", {
        class: "mem-form", onsubmit: async (e) => {
          e.preventDefault();
          try {
            await api(`/api/memory/${m.id}`, { method: "PUT", body: { summary: summary.value, body: body.value } });
            md.editing = false;
            md.history = null;
            await load();
            loadHistory(m.id);
          } catch (err) { toast(err.message); }
        },
      },
      el("label", {}, "Summary", summary, el("span", { class: "hint" }, "One line, the rule or fact itself (max 140 characters).")),
      el("label", {}, "Details", body),
      el("div", { class: "mem-actions" },
        el("button", { type: "submit", class: "btn primary small" }, "Save"),
        el("button", { type: "button", class: "btn ghost small", onclick: () => { md.editing = false; paintModal(); } }, "Cancel")));
      setTimeout(() => summary.focus(), 0);
    } else {
      const actions = m.deletedAt
        ? [el("button", { type: "button", class: "btn small", onclick: () => restoreEntry(m) }, "Restore")]
        : [
          el("button", { type: "button", class: "btn small", onclick: () => { md.editing = true; paintModal(); } }, "Edit"),
          el("button", { type: "button", class: "btn small", onclick: () => lockEntry(m, !m.locked) }, m.locked ? "Unlock" : "Lock"),
          el("button", { type: "button", class: "btn small danger", onclick: () => deleteEntry(m) }, "Delete"),
        ];
      main = el("div", {},
        el("h3", { class: "mem-modal-summary" }, m.summary),
        m.body ? el("div", { class: "md mem-body" }, renderMarkdown(m.body)) : el("p", { class: "hint" }, "No details."),
        el("div", { class: "mem-modal-meta" }, meta(m)),
        m.locked ? el("p", { class: "hint" }, "Locked: agents cannot change or delete it; they tell you if they think it is wrong.") : null,
        el("div", { class: "mem-actions" }, actions));
    }
    const hist = md.history;
    const history = el("div", { class: "mem-history" },
      el("h4", {}, "History"),
      hist == null ? el("p", { class: "hint" }, "Loading…") : el("ol", {}, hist.map((v) => el("li", {},
        el("span", { class: "mem-h-who" }, `${v.authorName} ${ACTIONS[v.action] || v.action}`),
        el("span", { class: "hint" }, ` · ${new Date(v.at).toLocaleString("en-GB", { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}`),
        v.reason ? el("div", { class: "mem-h-reason" }, `Reason: ${v.reason}`) : null,
        v.action === "set" ? el("div", { class: "mem-h-text" }, v.summary) : null))));
    md.box.replaceChildren(head, main, history);
  }

  // ---------- actiuni ----------
  async function lockEntry(m, locked) {
    try {
      await api(`/api/memory/${m.id}/lock`, { method: "PUT", body: { locked } });
      await load();
      loadHistory(m.id);
    } catch (err) { toast(err.message); }
  }

  async function deleteEntry(m) {
    try {
      await api(`/api/memory/${m.id}`, { method: "DELETE" });
      closeModal();
      await load();
      toast(`Deleted ${m.key}`, "ok", { label: "Undo", onClick: () => restoreEntry(m) });
    } catch (err) { toast(err.message); }
  }

  async function restoreEntry(m) {
    try {
      await api(`/api/memory/${m.id}/restore`, { method: "POST" });
      await load();
      if (state.openId === m.id) loadHistory(m.id);
    } catch (err) { toast(err.message); }
  }

  function openAdd() {
    const box = showModal("Add a memory entry");
    const key = el("input", { type: "text", maxlength: "60", placeholder: "area.topic, for example ui.dates", "aria-label": "Key" });
    const summary = el("input", { type: "text", maxlength: "140", placeholder: "Show dates as DD.MM.YYYY everywhere", "aria-label": "Summary" });
    const body = el("textarea", { rows: "6", maxlength: "4000", placeholder: "Why, examples, exceptions (optional)", "aria-label": "Details" });
    const locked = el("input", { type: "checkbox" });
    box.append(
      el("div", { class: "mem-modal-head" }, el("h3", {}, "Add to memory"), el("span", { class: "grow" }),
        el("button", { type: "button", class: "drawer-close", "aria-label": "Close", onclick: closeModal }, "×")),
      el("form", {
        class: "mem-form", onsubmit: async (e) => {
          e.preventDefault();
          try {
            const m = await api(`/api/projects/${projectId}/memory`, {
              method: "POST", body: { key: key.value, summary: summary.value, body: body.value, locked: locked.checked },
            });
            closeModal();
            await load();
            toast(`Saved ${m.key}`, "ok");
          } catch (err) { toast(err.message); }
        },
      },
      el("label", {}, "Key", key, el("span", { class: "hint" }, "Short and stable; agents update an entry by its key.")),
      el("label", {}, "Summary", summary, el("span", { class: "hint" }, "One line, shown to every agent in each turn.")),
      el("label", {}, "Details", body),
      el("label", { class: "mem-check" }, locked, "Lock it (agents cannot change or delete it)"),
      el("div", { class: "mem-actions" },
        el("button", { type: "submit", class: "btn primary small" }, "Save"),
        el("button", { type: "button", class: "btn ghost small", onclick: closeModal }, "Cancel"))));
    setTimeout(() => key.focus(), 0);
  }

  paintList();
  return {
    el: root,
    // enter: intrarea pe tab (prima data incarca lista); id = intrarea din adresa
    enter: (id) => {
      if (id) { if (state.openId !== id) open(id); return; }
      closeModal(); // inapoi la lista (Back din browser)
      if (!state.loaded) load();
    },
    refresh: () => { if (state.loaded) load(); },
    destroy: closeModal,
  };
}
