import { api } from "./api.js";
import { el, toast } from "./ui.js";
import { liveRenderer } from "./live.js";

// foremanChat: panoul de chat cu Foreman la pasul 3 al wizard-ului.
// Utilizatorul cere schimbari; Foreman (analistul, reluat cu --resume) actualizeaza
// direct propunerea din sesiune, iar setup.js reface formularul prin onChanged.
//
//   session    sesiunea de setup (id, chat, busy, chatRunId, version)
//   appName    numele aplicatiei din /api/info ("foreman"), afisat capitalizat
//   folderMode true in "Set up agents with AI" (doar agenti noi)
//   collect()  propunerea curenta din formular (aceeasi ca la Create / Add agents)
//   onChanged(session)  chemat cand versiunea propunerii a crescut sau dupa Undo
//   onBusy(bool)        chemat cand Foreman incepe / termina (setup.js blocheaza formularul)
//
// Intoarce { el, destroy() }. Elementul se poate muta in alta
// parte a paginii (setup.js il muta la reconstruirea formularului): conversatia si
// stream-ul in curs raman, fiindca instanta e aceeasi.
export function foremanChat({ session, appName, folderMode, collect, onChanged, onBusy = () => {} }) {
  const name = capitalize(appName || "foreman");
  const id = session.id;
  let version = session.version || 0; // ultima versiune a propunerii vazuta de pagina
  let chat = session.chat || [];
  let undoable = false;  // ultimul raspuns al lui Foreman a schimbat propunerea
  let busy = false;
  let stream = null;     // EventSource-ul run-ului de chat
  let live = null;       // caseta live "<Name> is working..."
  let destroyed = false;

  const intro = "I proposed this team. Tell me what to change: add or remove agents, rename them, rewrite instructions or context." +
    (folderMode ? " I only add new agents here; your existing team stays as it is." : "");
  const thread = el("div", { class: "fc-thread" });
  const input = el("textarea", { rows: 2, maxlength: 4000, placeholder: `Ask ${name} for a change...` });
  const sendBtn = el("button", { type: "submit", class: "btn primary" }, "Send");
  // Enter trimite, Shift+Enter face rand nou (isComposing: tastaturi IME)
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); send(); }
  });
  const root = el("div", { class: "foreman-chat" },
    el("div", { class: "fc-head" }, name),
    thread,
    el("form", { onsubmit: (e) => { e.preventDefault(); send(); } }, input, sendBtn));

  // draw redeseneaza conversatia din `chat`; caseta live (daca exista) ramane la final
  function draw() {
    const msgs = chat.map((m, i) => {
      const role = m.role === "user" || m.role === "foreman" ? m.role : "event";
      const undo = undoable && i === chat.length - 1 && !busy
        ? el("div", { class: "fc-undo" }, el("button", { type: "button", class: "btn small", onclick: undoLast }, "Undo"))
        : null;
      return el("div", { class: `fc-msg ${role}` }, m.text, undo);
    });
    thread.replaceChildren(el("div", { class: "fc-msg foreman" }, intro), ...msgs, ...(live ? [live] : []));
    thread.scrollTop = thread.scrollHeight;
  }

  function setBusy(on) {
    busy = on;
    input.disabled = on;
    sendBtn.disabled = on;
    onBusy(on);
  }

  async function send() {
    const message = input.value.trim();
    if (!message || busy) return;
    setBusy(true);
    let s;
    try {
      s = await api(`/api/setup/${id}/chat`, { method: "POST", body: { message, proposal: collect() } });
      if (!s) s = await api(`/api/setup/${id}`); // protectie: raspuns fara corp
    } catch (err) {
      toast(err.message);
      // serverul poate fi apucat sa adauge mesajul si un eveniment "failed": refacem conversatia
      try { chat = (await api(`/api/setup/${id}`))?.chat || chat; } catch { /* ramane cum era */ }
      draw();
      setBusy(false);
      return; // textul ramane in camp, se poate retrimite
    }
    input.value = "";
    undoable = false;
    chat = s.chat || chat;
    if (s.busy && s.chatRunId) follow(s.chatRunId);
    else finish();
  }

  // follow arata activitatea live a run-ului de chat, pana la app_done
  function follow(runId) {
    closeStream();
    const stopBtn = el("button", { type: "button", class: "btn small", onclick: () => {
      api(`/api/setup/${id}/stop`, { method: "POST" }).catch((err) => toast(err.message));
    } }, "Stop");
    live = el("div", { class: "live fc-live" },
      el("div", { class: "live-head" }, `${name} is working...`, " ", stopBtn));
    const render = liveRenderer(live);
    draw();
    stream = new EventSource(`/api/runs/${runId}/events`);
    stream.onmessage = (msg) => {
      let ev;
      try { ev = JSON.parse(msg.data); } catch { return; }
      if (ev.type === "app_done") { closeStream(); finish(); return; }
      render(ev);
      thread.scrollTop = thread.scrollHeight;
    };
    stream.onerror = () => { if (stream?.readyState === EventSource.CLOSED) { closeStream(); finish(); } };
  }

  function closeStream() { stream?.close(); stream = null; }

  // finish: ca checkResult din setup.js, finalul din server poate veni imediat dupa app_done
  async function finish(tries = 0) {
    if (destroyed) return;
    let s;
    try { s = await api(`/api/setup/${id}`); } catch (err) {
      toast(err.message);
      live = null;
      draw();
      setBusy(false);
      return;
    }
    if (destroyed) return;
    if (s.busy && tries < 20) { setTimeout(() => finish(tries + 1), 250); return; }
    if (s.busy && s.chatRunId) { follow(s.chatRunId); return; } // inca lucreaza: stream-ul s-a rupt, il reluam
    live = null;
    chat = s.chat || chat;
    busy = false; // Undo apare in draw doar cand nu lucreaza
    if (s.version > version) {
      version = s.version;
      undoable = true;
      onChanged(s); // formularul se reface; panoul e mutat in noua coloana
    }
    draw();
    setBusy(false);
    input.focus();
  }

  async function undoLast() {
    if (busy) return;
    setBusy(true);
    try {
      const s = await api(`/api/setup/${id}/undo`, { method: "POST" });
      version = s.version;
      chat = s.chat || chat;
      undoable = false;
      onChanged(s);
    } catch (err) {
      toast(err.message);
    }
    draw();
    setBusy(false);
  }

  draw();
  // la redeschiderea paginii: un run de chat in curs se reia live
  if (session.busy && session.chatRunId) {
    setBusy(true);
    follow(session.chatRunId);
  }

  return {
    el: root,
    destroy() { destroyed = true; closeStream(); root.remove(); },
  };
}

function capitalize(s) {
  return s.charAt(0).toUpperCase() + s.slice(1);
}
