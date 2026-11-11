// Tab-ul Brainstorm al unui proiect: subiecte in stanga (ca niste canale), in dreapta firul subiectului
// deschis. Fiecare mesaj al utilizatorului e o runda; sub el, cate un card per agent participant
// (idee, idee "wild" sau o intrebare inapoi). Ideile alese stau fixate sus.
import { api, upload } from "./api.js";
import { el, avatar, kindOf, toneOf, toast, clock } from "./ui.js";
import { userText } from "./md.js";
import { onPasteFiles, fileThumb, releaseThumb } from "./paste.js";
import { openIdea as openIdeaModal, syncIdea, closeIdea, plainText, ideaTags, stateText } from "./brainstorm-idea.js";

// brainstormView(projectId, { agents, onBadge })
//   agents: () => agentii proiectului (lista curenta din workspace)
//   onBadge(n): numarul intrebarilor deschise din toate subiectele (insigna tab-ului)
// open(topicId): deschide subiectul (null = primul); refresh(topicId): dupa un eveniment SSE
// "brainstorm" (0 / lipsa = reincarca tot, alt subiect = doar lista).
export function brainstormView(projectId, { agents, onBadge }) {
  const st = {
    topics: [],      // lista din GET /api/projects/{id}/brainstorm
    topicId: null,   // subiectul cerut prin adresa (null = primul)
    data: null,      // GET /api/brainstorm/{tid}: { topic, files, rounds }
    mode: "thread",  // "thread", "new" (formularul de subiect nou) sau "edit"
    pending: [],     // fisierele alese pentru urmatoarea runda
    busy: new Set(),   // butoane apasate: cardId:actiune, ca sa nu trimitem de doua ori
    sending: false,    // runda se trimite: Send ramane dezactivat si daca un eveniment redeseneaza
    selected: new Set(), // ideile bifate pentru Combine (id-uri, in ordinea bifarii)
    combineWith: null,   // agentul ales in bara Combine (null = autorul primei bife)
    combining: false,    // Combine se trimite
    loaded: false,
  };
  const agentsNow = () => (typeof agents === "function" ? agents() : agents) || [];
  const agentBy = (id) => agentsNow().find((a) => a.id === id);
  const face = (name, id, size) => avatar(name, kindOf(agentBy(id)), size);
  const topicHref = (id) => `#/p/${projectId}/brainstorm/${id}`;
  // meniul "Send to lead" deschis acum (se inchide la clic in afara, ca celelalte meniuri)
  let sendMenu = null;
  const closeMenus = (e) => { if (sendMenu && !sendMenu.contains(e.target)) sendMenu.open = false; };
  document.addEventListener("pointerdown", closeMenus);

  const list = el("nav", { class: "bs-topics", "aria-label": "Topics" });
  const head = el("div", { class: "bs-head" });
  const picked = el("div", { class: "bs-picked", hidden: true });
  const thread = el("div", { class: "bs-thread" });
  const form = el("div", { class: "bs-form-slot", hidden: true });

  // campul de jos: textarea care creste (ca in chat), Attach, Send
  const input = el("textarea", { rows: 1, placeholder: "Ask the team for ideas…", "aria-label": "Message to the participants" });
  const picker = el("input", { type: "file", multiple: true, hidden: true });
  const chips = el("div", { class: "chips bs-pending" });
  const sendBtn = el("button", { type: "submit", class: "btn primary" }, "Send");
  // syncSend: Send cere text (serverul refuza o runda doar cu fisiere), nu merge pe un subiect
  // arhivat si nici cat se trimite runda
  const syncSend = () => {
    const archived = !!st.data?.topic.archived;
    const noText = !input.value.trim();
    sendBtn.disabled = archived || st.sending || noText;
    sendBtn.title = !archived && noText && st.pending.length ? "Write a message to send with the files" : "";
  };
  const composerForm = el("form", { onsubmit: sendRound },
    el("button", { type: "button", class: "clip", "aria-label": "Attach files", title: "Attach files", onclick: () => picker.click() }, "\u{1F4CE}"),
    picker, input, sendBtn);
  const archivedNote = el("div", { class: "bs-archived-note", hidden: true });
  // bara Combine: apare deasupra campului de scris cand e bifata cel putin o idee
  const combineBar = el("div", { class: "bs-combine", hidden: true, role: "region", "aria-label": "Combine ideas" });
  const composer = el("div", { class: "composer bs-composer" }, combineBar, archivedNote, chips, composerForm);
  const grow = () => {
    input.style.height = "auto";
    input.style.height = input.scrollHeight + "px";
  };
  input.addEventListener("input", () => { grow(); syncSend(); });
  input.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      composerForm.requestSubmit();
    }
  });
  picker.addEventListener("change", () => { addPending(picker.files); picker.value = ""; });
  onPasteFiles(input, addPending);
  dropZone(composer, addPending);

  const main = el("section", { class: "bs-main" }, head, form, picked, thread, composer);
  const empty = el("div", { class: "bs-empty", hidden: true });
  const root = el("div", { class: "tab-body bs" }, list, main, empty);

  // ---------- date ----------
  async function loadList() {
    try {
      const res = await api(`/api/projects/${projectId}/brainstorm`);
      st.topics = res.topics || [];
      onBadge?.(res.openQuestions || 0);
    } catch (err) {
      toast(err.message);
    }
    renderList();
  }

  // current: subiectul afisat (cel din adresa, altfel primul nearhivat)
  function current() {
    const want = st.topics.find((t) => t.id === st.topicId);
    return want || st.topics.find((t) => !t.archived) || st.topics[0] || null;
  }

  async function loadTopic() {
    const t = current();
    if (!t) { st.data = null; render(); return; }
    try {
      const data = await api(`/api/brainstorm/${t.id}`);
      if (current()?.id !== t.id) return; // s-a schimbat subiectul intre timp
      const same = st.data?.topic.id === t.id;
      if (!same) clearSelection(); // bifele tin doar de subiectul deschis
      st.data = data;
      render(same ? "keep" : "bottom");
    } catch (err) {
      if (err.status === 404) { st.data = null; render(); return; }
      toast(err.message);
    }
  }

  // ---------- lista de subiecte ----------
  function renderList() {
    const cur = current();
    const row = (t) => el("a", {
      class: "bs-topic" + (st.mode !== "new" && t.id === cur?.id ? " active" : "") + (t.archived ? " archived" : ""),
      href: topicHref(t.id), "aria-current": st.mode !== "new" && t.id === cur?.id ? "page" : null,
      onclick: () => { if (st.mode !== "thread") { st.mode = "thread"; render(); } },
    },
    el("span", { class: "bs-hash", "aria-hidden": "true" }, "#"),
    el("span", { class: "bs-topic-name" }, t.name),
    t.waiting ? el("span", { class: "bs-topic-dot", title: `${t.waiting} ${t.waiting === 1 ? "card" : "cards"} waiting` }) : null,
    t.openQuestions ? el("span", { class: "bs-count", title: `${t.openQuestions} open ${t.openQuestions === 1 ? "question" : "questions"}` }, t.openQuestions) : null);
    const active = st.topics.filter((t) => !t.archived);
    const archived = st.topics.filter((t) => t.archived);
    fill(list,
      el("div", { class: "bs-topics-label" }, "Topics"),
      ...active.map(row),
      el("button", {
        type: "button", class: "bs-topic bs-new" + (st.mode === "new" ? " active" : ""),
        onclick: () => openForm("new"),
      }, el("span", { class: "bs-hash", "aria-hidden": "true" }, "+"), el("span", { class: "bs-topic-name" }, "New topic")),
      archived.length ? el("details", { class: "bs-archived", open: archived.some((t) => t.id === cur?.id) || null },
        el("summary", {}, `Archived (${archived.length})`), ...archived.map(row)) : null);
  }

  // ---------- subiectul deschis ----------
  // render: scroll = "bottom" (subiect deschis acum) sau "keep" (actualizare)
  function render(scroll = "keep") {
    renderList();
    const noTopics = st.loaded && !st.topics.length && st.mode !== "new";
    empty.hidden = !noTopics;
    main.hidden = noTopics;
    if (noTopics) {
      empty.replaceChildren(
        el("h2", {}, "Brainstorm with your team"),
        el("p", {}, "Open a topic, ask a question and every participant answers with an idea. Pick the good ones and send them to a lead."),
        el("button", { type: "button", class: "btn primary", onclick: () => openForm("new") }, "+ New topic"));
      return;
    }
    const inForm = st.mode !== "thread";
    form.hidden = !inForm;
    head.hidden = picked.hidden = thread.hidden = composer.hidden = inForm;
    if (inForm) return;
    if (!st.data) {
      head.replaceChildren();
      picked.hidden = true;
      renderCombine(); // fara subiect bara dispare
      thread.replaceChildren(el("p", { class: "muted" }, st.loaded ? "Loading…" : ""));
      return;
    }
    const nearBottom = thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    const top = thread.scrollTop;
    renderHead();
    renderPicked();
    pruneSelection();
    renderThread();
    renderCombine();
    const byId = new Map(allCards().map((c) => [c.id, c]));
    syncIdea((id) => byId.get(id)); // modala deschisa: cardul proaspat
    const t = st.data.topic;
    archivedNote.hidden = !t.archived;
    archivedNote.replaceChildren("This topic is archived. ",
      el("button", { type: "button", class: "link-btn", onclick: () => patchTopic({ archived: false }) }, "Unarchive"), " to continue.");
    input.disabled = t.archived;
    syncSend();
    if (scroll === "bottom" || nearBottom) thread.scrollTop = thread.scrollHeight;
    else thread.scrollTop = top;
  }

  function renderHead() {
    const { topic: t, files } = st.data;
    sendMenu = null;
    const leads = agentsNow().filter((a) => a.isRoot || a.isLead);
    const anyPicked = allCards().some((c) => c.picked);
    const filePicker = el("input", { type: "file", multiple: true, hidden: true });
    filePicker.addEventListener("change", () => { uploadFiles(filePicker.files); filePicker.value = ""; });
    const fileRow = el("div", { class: "bs-files" },
      ...files.map((f) => el("span", { class: "bs-file" },
        el("a", { href: `/api/brainstorm/${t.id}/files/${f.id}`, target: "_blank", rel: "noopener", title: f.name }, f.name),
        el("button", { type: "button", "aria-label": `Remove ${f.name}`, title: "Remove", onclick: () => removeFile(f) }, "×"))),
      filePicker,
      el("button", { type: "button", class: "btn small ghost", onclick: () => filePicker.click(), disabled: t.archived || null }, "\u{1F4CE} Upload"));
    dropZone(fileRow, uploadFiles);

    // Send to lead: liderii si radacinile; implicit prima radacina
    let send = null;
    if (leads.length) {
      const sel = el("select", { class: "select-small", "aria-label": "Lead" },
        leads.map((a) => el("option", { value: a.id }, a.name)));
      sel.value = String((leads.find((a) => a.isRoot) || leads[0]).id);
      const menu = el("details", { class: "menu bs-send" },
        el("summary", { class: "btn small", title: anyPicked ? "Send the picked ideas to a lead" : "Pick at least one idea first" }, "Send to lead"),
        el("div", { class: "menu-panel bs-send-panel" },
          el("span", { class: "label" }, "Send the picked ideas to"),
          sel,
          el("button", {
            type: "button", class: "btn primary small", disabled: !anyPicked || null,
            onclick: async (e) => {
              e.currentTarget.disabled = true;
              try {
                await api(`/api/brainstorm/${t.id}/send`, { method: "POST", body: { leadId: Number(sel.value) } });
                menu.open = false;
                toast(`Sent to ${leads.find((a) => a.id === Number(sel.value))?.name || "the lead"}.`, "ok");
              } catch (err) { toast(err.message); }
              e.currentTarget.disabled = false;
            },
          }, "Send"),
          anyPicked ? null : el("span", { class: "hint" }, "Pick at least one idea first.")));
      send = sendMenu = menu;
    }
    fill(head,
      el("div", { class: "bs-head-top" },
        el("h2", { class: "bs-title" }, el("span", { class: "bs-hash" }, "#"), t.name),
        el("div", { class: "bs-faces", "aria-label": "Participants" },
          t.participants.map((id) => {
            const a = agentBy(id);
            return a ? el("span", { title: a.name }, face(a.name, id, 24)) : null;
          })),
        el("span", { class: "grow" }),
        el("div", { class: "bs-head-actions" },
          el("button", { type: "button", class: "btn small ghost", onclick: () => openForm("edit") }, "Edit"),
          el("button", {
            type: "button", class: "btn small ghost", title: "Save a summary of this topic in the project's Files",
            onclick: async (e) => {
              e.currentTarget.disabled = true;
              try {
                const res = await api(`/api/brainstorm/${t.id}/save`, { method: "POST" });
                toast(`Saved ${res.name} to Files.`, "ok");
              } catch (err) { toast(err.message); }
              e.currentTarget.disabled = false;
            },
          }, "Save to Files"),
          send)),
      t.description ? el("p", { class: "bs-desc" }, t.description) : null,
      fileRow);
  }

  // Picked: ideile alese, fixate sus ca tile-uri mici (titlu + autor); clic = modala
  function renderPicked() {
    const cards = allCards().filter((c) => c.picked);
    picked.hidden = !cards.length;
    picked.replaceChildren(
      el("div", { class: "bs-picked-label" }, `Picked (${cards.length})`),
      el("div", { class: "bs-picked-list" }, cards.map((c) => el("div", {
        class: "bs-pin" + (c.kind === "wild" ? " wild" : ""), style: `--c:${tone(c)}`,
      },
      el("button", { type: "button", class: "bs-pin-open", title: "Open the idea", onclick: () => openIdea(c, cards) },
        el("span", { class: "bs-pin-title" }, c.title || "Idea"),
        el("span", { class: "bs-pin-who" }, face(c.agentName, c.agentId, 16), c.agentName || "Agent")),
      el("button", { type: "button", class: "bs-pin-x", "aria-label": "Unpick", title: "Unpick", onclick: () => cardAction(c, "unpick") }, "×")))));
  }

  function renderThread() {
    const { rounds } = st.data;
    if (!rounds.length) {
      thread.replaceChildren(el("div", { class: "bs-hint" },
        el("p", {}, "Describe a problem or ask a question below. Every participant answers with one idea; some may ask you something first.")));
      return;
    }
    thread.replaceChildren(...rounds.map((r) => el("div", { class: "bs-round" },
      el("div", { class: "bs-ask" },
        el("div", { class: "bs-bubble" }, userText(r.text || "")),
        el("span", { class: "bs-time" }, clock(r.createdAt))),
      el("div", { class: "bs-cards" }, r.cards.map((c) => card(c, r.cards))),
      r.combined?.length ? el("div", { class: "bs-combined" },
        el("div", { class: "bs-combined-label" }, "Combined"),
        el("div", { class: "bs-cards" }, r.combined.map((c) => card(c, r.combined)))) : null)));
  }

  // ---------- selectia si bara Combine ----------
  // selectable: se pot combina doar ideile terminate (idee / wild, si cele combinate), nu pe un subiect arhivat
  const selectable = (c) => c.status === "done" && (c.kind === "idea" || c.kind === "wild") && !st.data?.topic.archived;

  function clearSelection() {
    st.selected.clear();
    st.combineWith = null;
  }

  // pruneSelection: scoate bifele ideilor care nu mai exista sau nu se mai pot combina (dupa reincarcare)
  function pruneSelection() {
    const ok = new Set(allCards().filter(selectable).map((c) => c.id));
    for (const id of st.selected) if (!ok.has(id)) st.selected.delete(id);
    if (!st.selected.size) st.combineWith = null;
  }

  // toggleSelect: bifa de pe tile; schimba doar tile-ul si bara (fara redesenare, focusul ramane pe bifa)
  function toggleSelect(c, on, tile) {
    if (on) st.selected.add(c.id); else st.selected.delete(c.id);
    if (!st.selected.size) st.combineWith = null;
    tile.classList.toggle("sel", on);
    renderCombine();
  }

  // renderCombine: "<N> selected · Combine with <agent> · Clear · Combine"; Combine doar cu exact doua bifate
  function renderCombine() {
    const n = st.selected.size;
    combineBar.hidden = !n || !st.data;
    if (combineBar.hidden) { combineBar.replaceChildren(); return; }
    const byId = new Map(allCards().map((c) => [c.id, c]));
    const people = st.data.topic.participants.map(agentBy).filter(Boolean);
    const first = byId.get([...st.selected][0]);
    const def = people.find((a) => a.id === st.combineWith) || people.find((a) => a.id === first?.agentId) || people[0];
    const who = el("select", {
      class: "select-small", "aria-label": "Agent who combines the ideas",
      onchange: () => { st.combineWith = Number(who.value); },
    }, people.map((a) => el("option", { value: a.id }, a.name)));
    if (def) who.value = String(def.id);
    const ready = n === 2 && !!def && !st.combining;
    combineBar.replaceChildren(
      el("span", { class: "bs-combine-count" }, `${n} selected`),
      el("span", { class: "bs-combine-sep", "aria-hidden": "true" }, "·"),
      el("label", { class: "bs-combine-who" }, el("span", {}, "Combine with"), who),
      el("span", { class: "bs-combine-actions" },
        el("button", { type: "button", class: "btn small ghost", onclick: () => { clearSelection(); render(); } }, "Clear"),
        el("button", {
          type: "button", class: "btn small primary", disabled: !ready || null,
          title: n !== 2 ? "Select two ideas" : !def ? "Add a participant to this topic first" : null,
          onclick: combine,
        }, "Combine")));
  }

  async function combine() {
    const t = st.data?.topic;
    const [a, b] = [...st.selected];
    if (!t || st.selected.size !== 2 || st.combining) return;
    const agentId = Number(combineBar.querySelector("select")?.value) || 0; // alegerea din bara (sau implicitul ei)
    st.combining = true;
    renderCombine();
    try {
      await api(`/api/brainstorm/${t.id}/combine`, { method: "POST", body: { a, b, agentId } });
      clearSelection();
    } catch (err) {
      toast(err.message);
    }
    st.combining = false;
    await loadTopic();
    renderCombine(); // si daca reincarcarea a esuat
  }

  // ---------- tile-urile ----------
  // allCards: toate cardurile subiectului (si cele combinate), in ordinea rundelor
  const allCards = () => (st.data?.rounds || []).flatMap((r) => [...r.cards, ...(r.combined || [])]);
  const tone = (c) => toneOf(c.agentName, kindOf(agentBy(c.agentId)))[0];

  // jump: deruleaza la tile-ul unui card si il evidentiaza 1.5 s
  function jump(id) {
    const node = document.getElementById(`bs-card-${id}`);
    if (!node) return;
    node.scrollIntoView({ block: "center", behavior: "smooth" });
    node.classList.remove("flash");
    void node.offsetWidth; // reporneste animatia daca era deja evidentiat
    node.classList.add("flash");
    setTimeout(() => node.classList.remove("flash"), 1500);
  }

  const ideaCtx = {
    face: (c, size) => face(c.agentName, c.agentId, size),
    role: (c) => agentBy(c.agentId)?.role || "",
    busy: (c, action) => st.busy.has(`${c.id}:${action}`),
    act: (c, action, body) => cardAction(c, action, body),
    edit: editCard,
    jump,
  };
  // openIdea(c, siblings): modala ideii; siblings = tile-urile din acelasi rand (sagetile)
  const openIdea = (c, siblings, opts) => openIdeaModal(c, siblings, ideaCtx, opts);

  // card(c, siblings): tile-ul unui card (idee, wild, intrebare, combinat sau in asteptare); clic = modala
  function card(c, siblings) {
    const done = c.status === "done";
    const question = done && c.kind === "question";
    const open = question && !c.answer;
    const cls = ["bs-tile", `s-${c.status}`];
    if (done) cls.push(`k-${c.kind}`);
    if (c.picked) cls.push("picked");
    if (open) cls.push("open");
    if (!done) cls.push("dim");
    const canSelect = selectable(c);
    if (canSelect && st.selected.has(c.id)) cls.push("sel");
    const role = agentBy(c.agentId)?.role;
    const stop = (e) => e.stopPropagation(); // butoanele de pe tile nu deschid modala

    // textul: extrasul ideii, raspunsul la intrebare sau starea
    let text = null;
    if (done && c.kind !== "question") text = plainText(c.text);
    else if (question && c.text && c.text !== c.title) text = plainText(c.text);
    const title = done ? (c.title || (c.kind === "question" ? "Question" : "Idea")) : (c.title || null);

    let foot;
    if (!done) {
      foot = [
        el("span", { class: "bs-state " + c.status },
          c.status === "running" ? el("span", { class: "typing-dots", "aria-hidden": "true" }, el("i"), el("i"), el("i")) : null,
          stateText(c)),
        el("span", { class: "grow" }),
        c.status === "waiting" && !c.priority ? btn(c, "run-now", "Run now") : null,
        c.status === "waiting" || c.status === "running" ? btn(c, "stop", "Stop", "btn small ghost") : null,
        c.status === "stopped" || c.status === "failed" ? btn(c, "retry", "Retry") : null,
      ];
    } else if (open) {
      foot = el("div", { class: "bs-options", onclick: stop },
        c.options.map((o) => el("button", {
          type: "button", class: "btn small", disabled: st.busy.has(`${c.id}:answer`) || null,
          onclick: () => cardAction(c, "answer", { answer: o }),
        }, o)),
        el("button", { type: "button", class: "btn small ghost", onclick: () => openIdea(c, siblings, { other: true }) }, "Other…"));
    } else {
      foot = [
        question && c.answer ? el("span", { class: "bs-you", title: c.answer }, el("b", {}, "You: "), c.answer) : null,
        ...ideaTags(c, jump),
        c.followsId ? el("span", { class: "bs-follow", title: "Follows an earlier card" }, "↳") : null,
      ];
    }
    // "Combined from: A + B": titlurile duc la tile-urile surselor
    let from = null;
    if (c.combinedFrom?.length) {
      const links = [];
      c.combinedFrom.forEach((s, k) => {
        if (k) links.push(" + ");
        links.push(el("button", { type: "button", class: "link-btn", onclick: (e) => { e.stopPropagation(); jump(s.id); } }, s.title || "Idea"));
      });
      from = el("p", { class: "bs-from" }, el("span", {}, "Combined from: "), links);
    }
    const check = canSelect ? el("label", { class: "bs-check", title: "Select to combine" },
      el("input", {
        type: "checkbox", checked: st.selected.has(c.id) || null, "aria-label": `Select ${c.title || "idea"} to combine`,
        onchange: (e) => toggleSelect(c, e.target.checked, e.target.closest(".bs-tile")),
      })) : null;
    return el("article", {
      class: cls.join(" "), id: `bs-card-${c.id}`, style: `--c:${tone(c)}`,
      tabindex: "0", "aria-label": `${c.agentName || "Agent"}: ${c.title || stateText(c) || "idea"}`,
      onclick: (e) => { if (!e.target.closest("button, a, input, textarea, label")) openIdea(c, siblings); },
      onkeydown: (e) => { if (e.key === "Enter" && e.target === e.currentTarget) { e.preventDefault(); openIdea(c, siblings); } },
    },
    el("div", { class: "bs-who" }, face(c.agentName, c.agentId, 20),
      el("span", { class: "bs-agent" }, c.agentName || "Agent", role ? el("span", { class: "bs-role" }, ` · ${role}`) : null),
      check),
    title ? el("h3", { class: "bs-tile-title", title }, title) : null,
    from,
    text ? el("p", { class: "bs-tile-text" }, text) : null,
    el("div", { class: "bs-tile-foot" }, foot));
  }

  function btn(c, action, label, cls = "btn small") {
    const key = `${c.id}:${action}`;
    return el("button", {
      type: "button", class: cls, disabled: st.busy.has(key) || null,
      onclick: (e) => { e.stopPropagation(); cardAction(c, action); },
    }, label);
  }

  // editCard: Save din modala pe o idee combinata; intoarce true daca a mers
  async function editCard(c, title, text) {
    try {
      await api(`/api/brainstorm/cards/${c.id}/edit`, { method: "POST", body: { title, text } });
    } catch (err) {
      toast(err.message);
      return false;
    }
    await loadTopic();
    return true;
  }

  async function cardAction(c, action, body) {
    const key = `${c.id}:${action}`;
    if (st.busy.has(key)) return;
    st.busy.add(key);
    try {
      await api(`/api/brainstorm/cards/${c.id}/${action}`, { method: "POST", body });
    } catch (err) {
      toast(err.message);
    }
    st.busy.delete(key);
    await loadTopic(); // pick / unpick nu trimit mereu eveniment; reincarcam oricum
    if (action === "answer") loadList();
  }

  // ---------- runda noua ----------
  function addPending(files) {
    for (const f of Array.from(files || [])) st.pending.push(f);
    renderPending();
  }
  function renderPending() {
    syncSend(); // hint-ul de pe Send depinde de fisiere
    chips.replaceChildren(...st.pending.map((f, i) => el("span", { class: "chip" }, fileThumb(f), f.name,
      el("button", {
        type: "button", "aria-label": `Remove ${f.name}`,
        onclick: () => { releaseThumb(f); st.pending.splice(i, 1); renderPending(); },
      }, "×"))));
  }

  async function sendRound(e) {
    e.preventDefault();
    const t = st.data?.topic;
    const text = input.value.trim();
    if (!t || st.sending || t.archived) return;
    if (!text) {
      if (st.pending.length) toast("Write a message to send with the files.");
      return;
    }
    st.sending = true;
    syncSend();
    const fd = new FormData();
    fd.append("text", text);
    for (const f of st.pending) fd.append("files", f, f.name);
    try {
      await upload(`/api/brainstorm/${t.id}/rounds`, fd);
      input.value = "";
      grow();
      st.pending.forEach(releaseThumb);
      st.pending = [];
      renderPending();
      st.sending = false;
      await loadTopic();
      thread.scrollTop = thread.scrollHeight;
      loadList();
    } catch (err) {
      toast(err.message);
    }
    st.sending = false;
    syncSend();
  }

  // ---------- fisierele subiectului ----------
  async function uploadFiles(files) {
    const t = st.data?.topic;
    files = Array.from(files || []);
    if (!t || !files.length) return;
    const fd = new FormData();
    for (const f of files) fd.append("files", f, f.name);
    try {
      await upload(`/api/brainstorm/${t.id}/files`, fd);
      await loadTopic();
    } catch (err) { toast(err.message); }
  }

  async function removeFile(f) {
    const t = st.data?.topic;
    if (!t || !confirm(`Remove ${f.name} from this topic?`)) return;
    try {
      await api(`/api/brainstorm/${t.id}/files/${f.id}`, { method: "DELETE" });
      await loadTopic();
    } catch (err) { toast(err.message); }
  }

  // ---------- formularul: subiect nou / editare ----------
  function openForm(mode) {
    st.mode = mode;
    const t = mode === "edit" ? st.data?.topic : null;
    const people = agentsNow();
    const chosen = new Set(t ? t.participants : people.filter((a) => !a.detached).map((a) => a.id));
    const name = el("input", { type: "text", maxlength: 60, placeholder: "multiplayer", value: t?.name || "", required: true });
    const desc = el("textarea", { rows: 3, maxlength: 4000, placeholder: "What should the team think about? Goals, limits, what you already tried.", value: t?.description || "" });
    const faces = el("div", { class: "bs-people" });
    const paintPeople = () => faces.replaceChildren(...people.map((a) => el("button", {
      type: "button", class: "bs-person", "aria-pressed": String(chosen.has(a.id)),
      onclick: () => { chosen.has(a.id) ? chosen.delete(a.id) : chosen.add(a.id); paintPeople(); },
    }, face(a.name, a.id, 22), el("span", {}, a.name), a.detached ? el("span", { class: "bs-person-note" }, "not linked") : null)));
    paintPeople();
    const save = el("button", { type: "submit", class: "btn primary" }, t ? "Save" : "Create topic");
    const cancel = el("button", {
      type: "button", class: "btn ghost",
      onclick: () => { st.mode = "thread"; render(); },
    }, "Cancel");
    const archive = t ? el("button", {
      type: "button", class: "btn ghost",
      onclick: async () => { await patchTopic({ archived: !t.archived }); st.mode = "thread"; render(); },
    }, t.archived ? "Unarchive" : "Archive") : null;
    const f = el("form", {
      class: "section bs-form",
      onsubmit: async (e) => {
        e.preventDefault();
        save.disabled = true;
        const body = { name: name.value.trim(), description: desc.value.trim(), participants: [...chosen] };
        try {
          if (t) {
            await api(`/api/brainstorm/${t.id}`, { method: "PATCH", body });
            st.mode = "thread";
            await loadList();
            await loadTopic();
          } else {
            const created = await api(`/api/projects/${projectId}/brainstorm`, { method: "POST", body });
            st.mode = "thread";
            st.topics.push({ ...created, openQuestions: 0, waiting: 0 });
            location.hash = topicHref(created.id); // workspace -> open(created.id)
          }
        } catch (err) {
          toast(err.message);
          save.disabled = false;
        }
      },
    },
    el("h2", { class: "bs-form-title" }, t ? `Edit # ${t.name}` : "New topic"),
    el("div", { class: "field" }, el("label", {}, "Name"), name),
    el("div", { class: "field" }, el("label", {}, "Description"), desc),
    el("div", { class: "field" }, el("span", { class: "label" }, "Participants"), faces,
      el("span", { class: "hint" }, "Each participant answers every message with one idea.")),
    el("div", { class: "bs-form-actions" }, archive, el("span", { class: "grow" }), cancel, save));
    form.replaceChildren(f);
    render();
    name.focus();
  }

  async function patchTopic(body) {
    const t = st.data?.topic;
    if (!t) return;
    try {
      await api(`/api/brainstorm/${t.id}`, { method: "PATCH", body });
      await loadList();
      await loadTopic();
    } catch (err) { toast(err.message); }
  }

  // ---------- intrarea si evenimentele ----------
  let timer = null;
  let full = false;
  async function reloadAll() {
    await loadList();
    st.loaded = true;
    if (st.mode === "edit") return; // formularul deschis nu se redeseneaza
    if (current()?.id !== st.data?.topic.id) st.data = null;
    if (st.mode === "thread") await loadTopic();
    else render();
  }

  return {
    el: root,
    async open(topicId) {
      if (st.loaded && topicId === st.topicId && st.mode === "thread" && st.data) {
        thread.scrollTop = thread.scrollHeight; // datele sunt deja aici (incarcate pentru insigna)
        return;
      }
      st.topicId = topicId ?? null;
      if (st.mode === "new" || topicId != null) st.mode = "thread";
      await reloadAll();
      if (st.data) thread.scrollTop = thread.scrollHeight;
    },
    // refresh: dupa evenimentul "brainstorm"; subiectul deschis (sau 0) se reincarca, altfel doar lista
    refresh(topicId) {
      if (!topicId || topicId === st.data?.topic.id) full = true;
      clearTimeout(timer);
      timer = setTimeout(() => {
        const all = full;
        full = false;
        if (all) reloadAll(); else loadList();
      }, 120);
    },
    // destroy: la iesirea din proiect (workspace cleanup)
    destroy() {
      clearTimeout(timer);
      closeIdea();
      document.removeEventListener("pointerdown", closeMenus);
    },
  };
}

// fill: ca replaceChildren, dar sare peste null (replaceChildren ar scrie "null")
function fill(node, ...kids) {
  node.replaceChildren(...kids.flat().filter((k) => k != null && k !== false));
}

// dropZone: fisierele trase peste nod; contorul tine cont de dragenter/dragleave pe copii
function dropZone(node, onFiles) {
  const hasFiles = (e) => Array.from(e.dataTransfer?.types || []).includes("Files");
  let depth = 0;
  node.addEventListener("dragenter", (e) => { if (hasFiles(e)) { depth++; node.classList.add("dropping"); } });
  node.addEventListener("dragover", (e) => { if (hasFiles(e)) e.preventDefault(); });
  node.addEventListener("dragleave", () => { if (--depth <= 0) { depth = 0; node.classList.remove("dropping"); } });
  node.addEventListener("drop", (e) => {
    depth = 0;
    node.classList.remove("dropping");
    if (!hasFiles(e)) return;
    e.preventDefault();
    e.stopPropagation();
    onFiles(e.dataTransfer.files);
  });
}
