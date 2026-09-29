import { api } from "./api.js";
import { el, avatar, pill, toast, clock } from "./ui.js";
import { liveRenderer } from "./live.js";

const COLUMNS = [
  { title: "In asteptare", statuses: ["queued"] },
  { title: "In lucru", statuses: ["in_progress"] },
  { title: "Review", statuses: ["review", "blocked"] },
  { title: "Gata", statuses: ["done"] },
];

const ref = (t) => `T-${t.number}`;

export async function workspaceView(root, projectId) {
  const state = {
    project: await api(`/api/projects/${projectId}`),
    selected: null,   // agentul din consola
    seen: new Set(),  // id-urile mesajelor afisate in thread
    open: new Set(),  // task-urile deschise pe board (raman deschise la redesenare)
    liveRunId: null,
    liveStream: null,
  };
  state.selected = state.project.agents[0]?.id ?? null;

  const header = el("div");
  const team = el("div", { class: "team" });
  const board = el("div", { class: "board" });
  const count = el("span", { class: "hint" });
  const consoleHead = el("div", { class: "console-head" });
  const thread = el("div", { class: "thread", "aria-live": "polite" });
  const input = el("textarea", { rows: 1, "aria-label": "Mesaj" });
  const note = el("span", { class: "hint" });
  const composer = el("form", { onsubmit: send },
    input, el("button", { type: "submit", class: "btn primary" }, "Trimite"));
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      composer.requestSubmit();
    }
  });

  root.append(
    header,
    el("div", { class: "ws" },
      el("div", { class: "ws-main" },
        el("section", { class: "section" },
          el("div", { class: "section-head" },
            el("div", {}, el("h2", {}, "Echipa"), el("span", { class: "hint" }, "Selecteaza un agent ca sa vorbesti direct cu el")),
            el("a", { class: "btn small", href: `#/p/${projectId}/agent/new` }, "+ Adauga agent")),
          team),
        el("section", { class: "section" },
          el("div", { class: "section-head" }, el("h2", {}, "Task-uri"), count),
          board)),
      el("aside", { class: "console" },
        consoleHead, thread, el("div", { class: "composer" }, composer, note))));

  // ---------- desenare ----------
  function renderAll() {
    renderHeader();
    renderTeam();
    renderBoard();
    renderConsoleHead();
  }

  function renderHeader() {
    const p = state.project;
    header.replaceChildren(el("header", { class: "page-head" },
      el("div", {},
        el("div", { class: "crumbs" }, el("a", { href: "#/" }, "Proiecte"), " / ", p.name),
        el("div", { class: "actions" }, el("h1", {}, p.name), pill(p.status)),
        p.goal && el("p", { class: "muted" }, "Obiectiv: " + p.goal),
        p.paused && el("p", { class: "hint" }, "Echipa e pe pauza. Mesajele raman in asteptare pana o pornesti.")),
      el("div", { class: "actions" },
        el("a", { class: "btn", href: `#/p/${projectId}/edit` }, "Instructiuni proiect"),
        p.paused
          ? el("button", { class: "btn primary", onclick: () => projectAction("resume") }, "Porneste echipa")
          : el("button", { class: "btn", onclick: () => projectAction("stop") }, "Opreste echipa"))));
  }

  function renderTeam() {
    const [lead, ...workers] = state.project.agents;
    team.replaceChildren(
      memberCard(lead, true),
      workers.length
        ? el("div", { class: "team-workers" }, workers.map((a) => memberCard(a, false)))
        : el("p", { class: "hint" }, "Niciun worker inca. Adauga agenti ca lead-ul sa aiba cui delega."));
  }

  function memberCard(a, isLead) {
    const task = a.currentTask ? `${ref(a.currentTask)} ${a.currentTask.title}` : "";
    const statusText = a.status === "Working" ? (task || "Lucreaza")
      : a.status === "Queued" ? (task ? "Urmeaza: " + task : "Are mesaje in asteptare")
      : "Liber";
    return el("div", { class: "member-cell" + (isLead ? " lead" : "") },
      el("button", {
        type: "button", class: "member",
        "aria-pressed": String(a.id === state.selected),
        onclick: () => select(a.id),
      },
        avatar(a.name, a.position, isLead ? 40 : 32),
        el("span", { class: "member-body" },
          el("span", { class: "member-name" }, a.name, isLead && el("span", { class: "tag" }, "Lead")),
          el("span", { class: "hint" }, [a.role, a.model].filter(Boolean).join(" · ")),
          el("span", { class: "status" }, el("span", { class: "dot " + a.status.toLowerCase() }), el("span", {}, statusText)))),
      el("a", { class: "edit-link", href: `#/p/${projectId}/agent/${a.id}` }, "Editeaza"));
  }

  function renderBoard() {
    const byId = new Map(state.project.agents.map((a) => [a.id, a]));
    count.textContent = `${state.project.done} din ${state.project.total} gata`;
    board.replaceChildren(...COLUMNS.map((c) => {
      const items = state.project.tasks.filter((t) => c.statuses.includes(t.status));
      return el("div", { class: "col" },
        el("div", { class: "col-head" }, el("span", {}, c.title), el("span", {}, items.length)),
        items.map((t) => taskCard(t, byId.get(t.assigneeId))));
    }));
  }

  function taskCard(t, who) {
    const blocked = t.status === "blocked";
    return el("details", {
      class: "task",
      open: state.open.has(t.id),
      ontoggle: (e) => (e.target.open ? state.open.add(t.id) : state.open.delete(t.id)),
    },
      el("summary", {},
        el("span", { class: "task-title" }, el("span", { class: "task-id" }, ref(t)), t.title),
        el("span", { class: "task-who" },
          who && avatar(who.name, who.position, 20), who ? who.name : "?",
          blocked && el("span", { class: "badge-blocked" }, "Blocat"))),
      el("div", { class: "task-detail" },
        t.description && el("div", {}, t.description),
        t.feedback && el("div", {}, el("strong", {}, "Feedback: "), t.feedback),
        t.result && el("div", {}, el("strong", {}, "Rezultat: "), t.result),
        !t.description && !t.feedback && !t.result && el("div", { class: "hint" }, "Fara detalii inca."),
        blocked && el("div", { class: "actions" },
          el("button", { type: "button", class: "btn small primary", onclick: () => taskAction(t, "accept") }, "Accepta"),
          el("button", { type: "button", class: "btn small", onclick: () => taskAction(t, "unblock") }, "Deblocheaza"))));
  }

  function selectedAgent() {
    return state.project.agents.find((a) => a.id === state.selected);
  }

  function renderConsoleHead() {
    const a = selectedAgent();
    if (!a) return;
    const isLead = a.position === "lead";
    consoleHead.replaceChildren(
      avatar(a.name, a.position, 36),
      el("div", {},
        el("div", { class: "member-name" }, a.name, a.role && el("span", { class: "hint" }, "· " + a.role)),
        el("div", { class: "hint" }, isLead
          ? "Thread-ul lead-ului. Ii dai obiective aici, el delegheaza echipei."
          : "Thread direct. Lead-ul primeste in copie ce ii scrii.")));
    input.placeholder = isLead ? `Da-i lui ${a.name} un obiectiv sau cere un status` : `Mesaj pentru ${a.name}`;
    note.textContent = state.project.paused ? "Echipa e pe pauza: mesajul asteapta pana o pornesti." : "";
  }

  // ---------- thread ----------
  async function select(id) {
    state.selected = id;
    renderTeam();
    renderConsoleHead();
    await loadThread();
    syncLive();
  }

  async function loadThread() {
    const id = state.selected;
    if (!id) return;
    const messages = await api(`/api/agents/${id}/messages`);
    if (id !== state.selected) return; // s-a schimbat selectia intre timp
    stopLive();
    state.seen = new Set();
    thread.replaceChildren();
    if (!messages.length) thread.append(el("div", { class: "empty" }, "Niciun mesaj inca."));
    messages.forEach(addMessage);
    scrollDown(true);
  }

  function addMessage(m) {
    if (state.seen.has(m.id)) {
      // acelasi mesaj, stare noua (ex. livrat): il redesenam pe loc
      thread.querySelector(`[data-id="${m.id}"]`)?.replaceWith(messageNode(m));
      return;
    }
    state.seen.add(m.id);
    thread.querySelector(".empty")?.remove();
    const node = messageNode(m);
    const live = thread.querySelector(".live");
    if (live) thread.insertBefore(node, live); // blocul live ramane ultimul
    else thread.append(node);
    scrollDown();
  }

  function messageNode(m) {
    if (m.kind === "event") return el("div", { class: "event", "data-id": m.id }, m.text);
    const mine = m.author === "user";
    return el("div", { class: "msg" + (mine ? " me" : ""), "data-id": m.id },
      el("span", { class: "msg-meta" }, `${mine ? "Tu" : m.author} · ${clock(m.createdAt)}`),
      el("div", { class: "bubble" }, m.text),
      mine && !m.delivered && el("span", { class: "pending" }, "in asteptare"));
  }

  function scrollDown(force) {
    const nearBottom = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (force || nearBottom) thread.scrollTop = thread.scrollHeight;
  }

  async function send(e) {
    e.preventDefault();
    const text = input.value.trim();
    if (!text || !state.selected) return;
    try {
      const m = await api(`/api/agents/${state.selected}/messages`, { method: "POST", body: { text } });
      input.value = "";
      addMessage(m);
      scrollDown(true);
    } catch (err) {
      toast(err.message);
    }
  }

  // ---------- run-ul live al agentului selectat ----------
  function syncLive() {
    const a = selectedAgent();
    const runId = a?.activeRunId || null;
    if (runId === state.liveRunId) return;
    stopLive();
    if (!runId) return;
    state.liveRunId = runId;
    const box = el("div", { class: "live" }, el("div", { class: "live-head" }, `${a.name} lucreaza acum`));
    thread.append(box);
    scrollDown();
    const render = liveRenderer(box);
    const es = new EventSource(`/api/runs/${runId}/events`);
    state.liveStream = es;
    es.onmessage = (msg) => {
      let ev;
      try { ev = JSON.parse(msg.data); } catch { return; }
      if (ev.type === "app_done") {
        stopLive(); // raspunsul final vine ca mesaj normal in thread
        return;
      }
      render(ev);
      scrollDown();
    };
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) stopLive();
    };
  }

  function stopLive() {
    state.liveStream?.close();
    state.liveStream = null;
    state.liveRunId = null;
    thread.querySelector(".live")?.remove();
  }

  // ---------- actiuni ----------
  async function projectAction(what) {
    try {
      await api(`/api/projects/${projectId}/${what}`, { method: "POST" });
      await reload();
    } catch (err) {
      toast(err.message);
    }
  }

  async function taskAction(t, what) {
    try {
      await api(`/api/tasks/${t.id}/${what}`, { method: "POST" });
      await reload();
    } catch (err) {
      toast(err.message);
    }
  }

  // ---------- sincronizare live cu serverul ----------
  let reloadTimer;
  async function reload() {
    try {
      state.project = await api(`/api/projects/${projectId}`);
    } catch (err) {
      toast(err.message);
      return;
    }
    if (!selectedAgent()) state.selected = state.project.agents[0]?.id ?? null; // agent sters
    renderAll();
    syncLive();
  }
  const scheduleReload = () => {
    clearTimeout(reloadTimer);
    reloadTimer = setTimeout(reload, 150);
  };

  let hadError = false;
  const events = new EventSource(`/api/projects/${projectId}/events`);
  events.onmessage = (msg) => {
    let ev;
    try { ev = JSON.parse(msg.data); } catch { return; }
    if (ev.type === "message" && ev.data.agentId === state.selected) addMessage(ev.data);
    scheduleReload();
  };
  events.onerror = () => { hadError = true; };
  events.onopen = async () => {
    // dupa o reconectare (ex. restart server) reincarcam tot, am putut pierde evenimente
    if (!hadError) return;
    hadError = false;
    await reload();
    await loadThread();
    syncLive();
  };

  renderAll();
  await loadThread();
  syncLive();

  return () => {
    events.close();
    stopLive();
    clearTimeout(reloadTimer);
  };
}
