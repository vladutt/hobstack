import { api, upload } from "./api.js";
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
    pending: [],      // fisiere alese pentru urmatorul mesaj
    doneRunId: null,  // ultimul run terminat: activeRunId poate ramane vechi pana la reload
  };
  let disposed = false; // view-ul a fost inchis: nu mai deschidem stream-uri si nu mai desenam
  // view-ul e inchis daca s-a apelat curatarea sau daca #view a fost golit intre timp
  const gone = () => disposed || !header.isConnected;
  state.selected = state.project.agents[0]?.id ?? null;

  const hasFiles = (e) => Array.from(e.dataTransfer?.types || []).includes("Files");
  const dropResets = []; // resetari de contor + outline, apelate la orice drop/dragend
  // titlul proiectului sta in coloana din stanga, ca chat-ul sa inceapa din
  // varful paginii si sa incapa intreg pe ecran (inclusiv campul de scris)
  const header = el("div", { class: "ws-head" });
  const team = el("div", { class: "team" });
  const board = el("div", { class: "board" });
  const count = el("span", { class: "hint" });
  const filesList = el("div", { class: "files-list" });
  const projectPicker = el("input", { type: "file", multiple: true, hidden: true });
  projectPicker.addEventListener("change", () => { uploadProjectFiles(projectPicker.files); projectPicker.value = ""; });
  const filesSection = el("section", { class: "section" },
    el("div", { class: "section-head" },
      el("h2", {}, "Fisiere"),
      el("button", { type: "button", class: "btn small", onclick: () => projectPicker.click() }, "Incarca"),
      projectPicker),
    filesList);
  setupDrop(filesSection, uploadProjectFiles);
  const consoleHead = el("div", { class: "console-head" });
  const thread = el("div", { class: "thread", "aria-live": "polite" });
  const input = el("textarea", { rows: 1, "aria-label": "Mesaj" });
  const note = el("span", { class: "hint" });
  const chips = el("div", { class: "chips" });
  const chatPicker = el("input", { type: "file", multiple: true, hidden: true });
  chatPicker.addEventListener("change", () => { addPending(chatPicker.files); chatPicker.value = ""; });
  const clip = el("button", {
    type: "button", class: "clip", "aria-label": "Ataseaza fisiere", title: "Ataseaza fisiere",
    onclick: () => chatPicker.click(),
  }, "\u{1F4CE}");
  let sending = false; // trimitere in curs: blocheaza dublul submit
  const sendBtn = el("button", { type: "submit", class: "btn primary" }, "Trimite");
  const composer = el("form", { onsubmit: send }, clip, chatPicker, input, sendBtn);
  input.addEventListener("paste", (e) => {
    const files = e.clipboardData?.files;
    if (!files || !files.length) return;
    // daca e si text (ex. copiat din Word), lasam textul sa se lipeasca
    if (!e.clipboardData.getData("text/plain")) e.preventDefault();
    addPending(files);
  });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      composer.requestSubmit();
    }
  });

  const consoleBox = el("aside", { class: "console" },
    consoleHead, thread, el("div", { class: "composer" }, chips, composer, note));
  setupDrop(consoleBox, addPending);

  root.append(
    el("div", { class: "ws" },
      el("div", { class: "ws-main" },
        header,
        el("section", { class: "section" },
          el("div", { class: "section-head" },
            el("div", {}, el("h2", {}, "Echipa"), el("span", { class: "hint" }, "Selecteaza un agent ca sa vorbesti direct cu el")),
            el("a", { class: "btn small", href: `#/p/${projectId}/agent/new` }, "+ Adauga agent")),
          team),
        el("section", { class: "section" },
          el("div", { class: "section-head" }, el("h2", {}, "Task-uri"), count),
          board),
        filesSection),
      consoleBox));

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
    if (!lead) {
      team.replaceChildren(el("p", { class: "hint" }, "Proiectul nu are niciun agent."));
      return;
    }
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
    input.placeholder = isLead ? `Scrie-i lui ${a.name}...` : `Mesaj pentru ${a.name}`;
    note.textContent = state.project.paused ? "Echipa e pe pauza: mesajul asteapta pana o pornesti." : "";
  }

  // ---------- thread ----------
  async function select(id) {
    if (id !== state.selected) { state.pending = []; renderChips(); }
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
    if (gone() || id !== state.selected) return; // s-a schimbat selectia intre timp
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
      m.text ? el("div", { class: "bubble" }, m.text) : null,
      m.attachments && m.attachments.length > 0 && el("div", { class: "attachments" }, m.attachments.map(attachmentNode)),
      mine && !m.delivered && el("span", { class: "pending" }, "in asteptare"));
  }

  function scrollDown(force) {
    const nearBottom = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (force || nearBottom) thread.scrollTop = thread.scrollHeight;
  }

  async function send(e) {
    e.preventDefault();
    const text = input.value.trim();
    if ((!text && !state.pending.length) || !state.selected || sending) return;
    const agentId = state.selected; // agentul pentru care trimitem, tinut minte cat dureaza cererea
    sending = true;
    sendBtn.disabled = true;
    try {
      let m;
      const path = `/api/agents/${agentId}/messages`;
      if (state.pending.length) {
        const fd = new FormData();
        fd.append("text", text);
        for (const f of state.pending) fd.append("files", f, f.name);
        m = await upload(path, fd);
      } else {
        m = await api(path, { method: "POST", body: { text } });
      }
      // daca userul a schimbat agentul intre timp, nu atingem chatul si lista celuilalt agent
      if (state.selected === agentId) {
        input.value = "";
        state.pending = [];
        renderChips();
        addMessage(m);
        scrollDown(true);
      }
    } catch (err) {
      toast(err.message);
    } finally {
      sending = false;
      sendBtn.disabled = false;
    }
  }

  // ---------- atasamente in chat ----------
  function addPending(fileList) {
    for (const f of Array.from(fileList)) {
      // imaginile lipite (Cmd+V) vin adesea cu numele generic "image.png"
      const generic = f.type.startsWith("image/") && (!f.name || /^image\.\w+$/.test(f.name));
      let file = f;
      if (generic) {
        const ext = { "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp" }[f.type] || "png";
        const d = new Date();
        const t = d.toTimeString().slice(0, 8).replaceAll(":", "") + String(d.getMilliseconds()).padStart(3, "0");
        file = new File([f], `screenshot-${t}.${ext}`, { type: f.type });
      }
      state.pending.push(file);
    }
    renderChips();
  }

  function renderChips() {
    chips.replaceChildren(...state.pending.map((f, i) =>
      el("span", { class: "chip" }, f.name,
        el("button", {
          type: "button", "aria-label": `Scoate ${f.name}`,
          onclick: () => { state.pending.splice(i, 1); renderChips(); },
        }, "\u00d7"))));
  }

  function attachmentNode(a) {
    const url = `/api/files/${a.id}`;
    if (a.mime && a.mime.startsWith("image/")) {
      return el("a", { href: url, target: "_blank", rel: "noopener" },
        el("img", { src: url, alt: a.name, loading: "lazy" }));
    }
    return el("a", { class: "file-link", href: url, target: "_blank", rel: "noopener" }, a.name);
  }

  // ---------- fisiere de proiect ----------
  function fmtSize(n) {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }

  async function loadFiles() {
    let files;
    try {
      files = await api(`/api/projects/${projectId}/files`);
    } catch (err) {
      toast(err.message);
      return;
    }
    if (gone()) return;
    if (!files.length) {
      filesList.replaceChildren(el("p", { class: "hint" }, "Niciun fisier. Trage fisiere aici sau apasa Incarca."));
      return;
    }
    filesList.replaceChildren(...files.map((f) =>
      el("div", { class: "file-row" },
        el("a", { href: `/api/files/${f.id}`, target: "_blank", rel: "noopener" }, f.name),
        el("span", { class: "hint" }, fmtSize(f.size)),
        el("button", {
          type: "button", class: "btn small", "aria-label": `Sterge ${f.name}`,
          onclick: () => removeFile(f),
        }, "x"))));
  }

  async function uploadProjectFiles(fileList) {
    const list = Array.from(fileList);
    if (!list.length) return;
    const fd = new FormData();
    for (const f of list) fd.append("files", f, f.name);
    try {
      await upload(`/api/projects/${projectId}/files`, fd);
    } catch (err) {
      toast(err.message);
    }
    await loadFiles();
  }

  async function removeFile(f) {
    if (!confirm(`Stergi fisierul ${f.name}?`)) return;
    try {
      await api(`/api/files/${f.id}`, { method: "DELETE" });
    } catch (err) {
      toast(err.message);
    }
    await loadFiles();
  }

  // drag & drop: dragover cu preventDefault, altfel browserul nu accepta drop-ul
  function setupDrop(node, onFiles) {
    let depth = 0; // dragenter/dragleave se declanseaza si pe copii
    dropResets.push(() => { depth = 0; node.classList.remove("dropping"); });
    node.addEventListener("dragenter", (e) => { if (hasFiles(e)) { depth++; node.classList.add("dropping"); } });
    node.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
    node.addEventListener("dragleave", () => { if (--depth <= 0) { depth = 0; node.classList.remove("dropping"); } });
    node.addEventListener("drop", (e) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth = 0;
      node.classList.remove("dropping");
      onFiles(e.dataTransfer.files);
    });
  }

  // garda la nivel de fereastra: un fisier scapat pe langa zone ar naviga browserul
  const guardOver = (e) => { if (hasFiles(e)) e.preventDefault(); };
  const guardDrop = (e) => {
    if (hasFiles(e)) e.preventDefault();
    dropResets.forEach((fn) => fn());
  };
  const guardEnd = () => dropResets.forEach((fn) => fn());
  window.addEventListener("dragover", guardOver);
  window.addEventListener("drop", guardDrop);
  window.addEventListener("dragend", guardEnd);

  // ---------- run-ul live al agentului selectat ----------
  function syncLive() {
    if (gone()) return;
    const a = selectedAgent();
    const runId = a?.activeRunId || null;
    if (runId === state.liveRunId) return;
    if (runId && runId === state.doneRunId) return; // run terminat, activeRunId e inca vechi
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
        state.doneRunId = runId;
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
    if (gone()) return;
    if (!selectedAgent()) state.selected = state.project.agents[0]?.id ?? null; // agent sters
    renderAll();
    syncLive();
  }
  const scheduleReload = () => {
    clearTimeout(reloadTimer);
    reloadTimer = setTimeout(reload, 150);
  };

  renderAll();
  loadFiles();
  await loadThread();
  syncLive();

  // stream-ul proiectului se deschide abia acum: pana aici nu exista nimic de inchis
  if (gone()) {
    stopLive();
    window.removeEventListener("dragover", guardOver);
    window.removeEventListener("drop", guardDrop);
    window.removeEventListener("dragend", guardEnd);
    return () => {};
  }
  let hadError = false;
  const events = new EventSource(`/api/projects/${projectId}/events`);
  events.onmessage = (msg) => {
    let ev;
    try { ev = JSON.parse(msg.data); } catch { return; }
    if (ev.type === "message" && ev.data.agentId === state.selected) addMessage(ev.data);
    if (ev.type === "files") loadFiles();
    scheduleReload();
  };
  events.onerror = () => { hadError = true; };
  events.onopen = async () => {
    // dupa o reconectare (ex. restart server) reincarcam tot, am putut pierde evenimente
    if (!hadError) return;
    hadError = false;
    await reload();
    loadFiles();
    await loadThread();
    syncLive();
  };


  return () => {
    disposed = true;
    window.removeEventListener("dragover", guardOver);
    window.removeEventListener("drop", guardDrop);
    window.removeEventListener("dragend", guardEnd);
    events.close();
    stopLive();
    clearTimeout(reloadTimer);
  };
}
