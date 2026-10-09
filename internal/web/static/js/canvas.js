// Canvas-ul echipei: graf SVG cu nodul "Tu" sus, agentii ca noduri, delegari
// (linie plina cu sageata) si review-uri (linie punctata amber).
// Totul se construieste cu svg()/el(), niciodata cu innerHTML.
import { api } from "./api.js";
import { el, svg, kindOf, toneOf, agentBody, folderOf, toast } from "./ui.js";

const NODE_W = 196, NODE_H = 72, YOU_W = 84, YOU_H = 34, TOP = 16, LEVEL = 120;
const DRAG_PX = 4; // sub pragul asta e click, nu mutare

// autoLayout aseaza pe niveluri (dupa adancimea in arbore) doar agentii fara pozitie salvata.
function autoLayout(agents, width) {
  const byId = new Map(agents.map((a) => [a.id, a]));
  const depth = (a) => {
    let d = 0, cur = a;
    while (cur.parentId && byId.has(cur.parentId) && d < agents.length) { cur = byId.get(cur.parentId); d++; }
    return d;
  };
  const pos = new Map(), levels = [];
  for (const a of agents) {
    if (a.posX != null && a.posY != null) pos.set(a.id, { x: a.posX, y: a.posY });
    else (levels[depth(a)] ||= []).push(a);
  }
  levels.forEach((list, d) => list.forEach((a, i) => {
    const step = Math.max(NODE_W + 16, width / (list.length + 1));
    pos.set(a.id, { x: step * (i + 1) - NODE_W / 2, y: TOP + YOU_H + 50 + d * LEVEL });
  }));
  return pos;
}

const shorten = (s, n = 26) => (s.length > n ? s.slice(0, n - 3) + "..." : s);

// modelShort: "claude-opus-4-8" -> "Opus 4.8", "sonnet" -> "Sonnet"
function modelShort(id) {
  const parts = String(id || "").replace(/^claude-/, "").split("-");
  if (!parts[0]) return "";
  return [parts[0].charAt(0).toUpperCase() + parts[0].slice(1), parts.slice(1).join(".")].filter(Boolean).join(" ");
}

// onCreate(pos, link): agent nou; pos e {x, y} in coordonate SVG sau null (butonul "+ Agent"),
// link e {parentId, detached}: sub un agent, legat de "Tu" sau nelegat.
// onDelete(id): butonul x de pe nod.
const UNLINKED = { parentId: null, detached: true };
export function teamCanvas({ onSelect, onEdit, onChanged, onCreate, onDelete, onAdd }) {
  let project = { agents: [] };
  let selectedId = null;
  let selectedLink = null;   // "p:<id>" sau "r:<id>"
  let pendingProject = null; // proiect primit cat timp un drag e activ
  let busy = false;          // mutare sau legare in curs
  let pos = new Map();       // id -> {x, y} (in coordonate SVG)
  // ultimul viewBox, pe marimea canvas-ului: la aceeasi marime doar creste, ca o
  // mutare sa nu rescaleze (si sa nu "miste") restul nodurilor
  let lastView = null;       // {width, height, box: [minX, minY, maxX, maxY]}
  let youPos = { x: 0, y: TOP };
  let edges = [];            // {kind, from, to, els: [path...]}
  let nodeEls = new Map();   // id -> g.node
  let menu = null;

  const svgEl = svg("svg", { class: "team-canvas", preserveAspectRatio: "xMidYMin meet", role: "img", "aria-label": "Team graph" });
  const deleteBtn = el("button", { type: "button", class: "btn small canvas-delete", disabled: true, onclick: () => deleteLink() }, "Delete link");
  const fullBtn = el("button", { type: "button", class: "btn small canvas-full-toggle", onclick: () => setFull(!wrap.classList.contains("full")) }, "Full screen");
  const legend = (cls, label) => el("span", { class: "legend-item" },
    svg("svg", { width: 28, height: 8, "aria-hidden": "true" }, svg("line", { x1: 1, y1: 4, x2: 27, y2: 4, class: "edge " + cls })), label);
  // "+ Agent" deschide pagina de agent nou (onAdd); fara ea, un agent nelegat pe canvas
  const addBtn = el("button", { type: "button", class: "btn small primary", onclick: () => (onAdd ? onAdd() : onCreate(null, UNLINKED)) }, "+ Agent");
  const bar = el("div", { class: "canvas-bar" }, legend("delegate", "reports to"), legend("review", "reviewed by"),
    el("span", { class: "grow" }), deleteBtn, fullBtn, addBtn);
  // zoom: 1 = tot graful incape; pan = deplasarea centrului, in unitati SVG
  let zoom = 1, panX = 0, panY = 0;
  let fitBox = null; // [x, y, w, h]: viewBox-ul la zoom 1
  const zoomLabel = el("span", { class: "zoom-label" }, "100%");
  const zoomBtn = (label, aria, fn) => el("button", { type: "button", class: "zoom-btn", "aria-label": aria, title: aria, onclick: fn }, label);
  const zoomBar = el("div", { class: "canvas-zoom" },
    zoomBtn("\u2212", "Zoom out", () => setZoom(zoom / 1.25)), zoomLabel,
    zoomBtn("+", "Zoom in", () => setZoom(zoom * 1.25)),
    zoomBtn("Fit", "Fit the whole team", () => { panX = panY = 0; setZoom(1); }));
  // "How a task travels": drumul unui task prin echipa reala; se poate ascunde (tinut minte)
  const travel = el("div", { class: "canvas-travel" });
  let travelHidden = false;
  try { travelHidden = localStorage.getItem("hobstack-travel") === "0"; } catch {}
  const travelToggle = el("button", { type: "button", class: "btn ghost small travel-toggle", onclick: () => setTravel(!travelHidden) });
  function setTravel(hidden) {
    travelHidden = hidden;
    try { localStorage.setItem("hobstack-travel", hidden ? "0" : "1"); } catch {}
    draw(); // si incadrarea: panoul deschis tine loc jos
  }
  const wrap = el("div", { class: "canvas-wrap", tabindex: "0" }, bar, svgEl, travel, zoomBar);

  function setFull(on) {
    wrap.classList.toggle("full", on);
    fullBtn.textContent = on ? "Close" : "Full screen";
    closeMenu();
    // cat timp e ecran mare, Escape iese si fara focus pe canvas (focusul se pierde
    // cand un buton se dezactiveaza sau meniul se inchide)
    document.removeEventListener("keydown", fullEscape);
    if (on) document.addEventListener("keydown", fullEscape);
    wrap.focus();
    // redesenarea vine din ResizeObserver
  }
  function fullEscape(e) {
    if (e.key !== "Escape" || wrap.contains(e.target)) return; // in wrap se ocupa handler-ul lui
    const t = document.activeElement;
    if (t && (t.tagName === "TEXTAREA" || (t.tagName === "INPUT" && t.type !== "button"))) return;
    if (menu) closeMenu(); else setFull(false);
  }

  // ---------- coordonate ----------
  function toSvg(e) {
    const pt = svgEl.createSVGPoint();
    pt.x = e.clientX; pt.y = e.clientY;
    return pt.matrixTransform(svgEl.getScreenCTM().inverse());
  }

  // rect: dreptunghiul unui nod (agent sau "you")
  function rect(id) {
    if (id === "you") return { x: youPos.x, y: youPos.y, w: YOU_W, h: YOU_H };
    const p = pos.get(id);
    return p && { x: p.x, y: p.y, w: NODE_W, h: NODE_H };
  }

  // ---------- linii ----------
  const vCurve = (x1, y1, x2, y2) => { const ym = (y1 + y2) / 2; return `M ${x1} ${y1} C ${x1} ${ym}, ${x2} ${ym}, ${x2} ${y2}`; };
  const hCurve = (x1, y1, x2, y2) => { const xm = (x1 + x2) / 2; return `M ${x1} ${y1} C ${xm} ${y1}, ${xm} ${y2}, ${x2} ${y2}`; };

  // BEND: cat de mult se deplaseaza curba de review cand reviewer-ul e si seful,
  // ca sa nu acopere linia de delegare (care altfel n-ar mai putea fi selectata)
  const BEND = 28;

  function edgePath(kind, from, to, bend) {
    const a = rect(from), b = rect(to);
    if (!a || !b) return "";
    if (kind === "delegate") {
      const dx = b.x + b.w / 2 - (a.x + a.w / 2);
      // de la "Tu" spre un agent din lateral: iese pe o parte, pe deasupra celorlalte
      // noduri, ca sa nu para ca pleaca din agentul de sub "Tu"
      if (from === "you" && Math.abs(dx) > NODE_W / 2) {
        const x1 = dx < 0 ? a.x : a.x + a.w, y1 = a.y + a.h / 2, x2 = b.x + b.w / 2;
        return `M ${x1} ${y1} C ${x2} ${y1}, ${x2} ${y1}, ${x2} ${b.y}`;
      }
      return vCurve(a.x + a.w / 2, a.y + a.h, b.x + b.w / 2, b.y);
    }
    // review: ancorat pe laturile orientate una spre alta, deplasat cu o daca e nevoie
    const o = bend ? BEND : 0;
    const dx = b.x + b.w / 2 - (a.x + a.w / 2), dy = b.y + b.h / 2 - (a.y + a.h / 2);
    if (Math.abs(dy) > Math.abs(dx)) {
      return dy > 0
        ? vCurve(a.x + a.w / 2 + o, a.y + a.h, b.x + b.w / 2 + o, b.y)
        : vCurve(a.x + a.w / 2 + o, a.y, b.x + b.w / 2 + o, b.y + b.h);
    }
    return dx > 0
      ? hCurve(a.x + a.w, a.y + a.h / 2 + o, b.x, b.y + b.h / 2 + o)
      : hCurve(a.x, a.y + a.h / 2 + o, b.x + b.w, b.y + b.h / 2 + o);
  }

  function redrawEdges() {
    for (const e of edges) {
      const d = edgePath(e.kind, e.from, e.to, e.bend);
      for (const p of e.els) p.setAttribute("d", d);
      if (e.x && d) {
        // butonul x sta la mijlocul liniei
        const len = e.els[0].getTotalLength();
        const m = e.els[0].getPointAtLength(len / 2);
        e.x.setAttribute("transform", `translate(${m.x} ${m.y})`);
      }
    }
  }

  // ---------- desenare ----------
  function draw() {
    const width = wrap.clientWidth, height = svgEl.clientHeight;
    if (!width || !height) return; // inca nu e in pagina: ResizeObserver ne cheama
    const agents = project.agents;
    pos = autoLayout(agents, width);
    // "Tu" sta deasupra radacinilor (agentii legati direct de utilizator)
    const roots = agents.filter((a) => !a.parentId && !a.detached).map((a) => pos.get(a.id)).filter(Boolean);
    youPos = roots.length
      ? { x: roots.reduce((s, p) => s + p.x + NODE_W / 2, 0) / roots.length - YOU_W / 2, y: Math.min(...roots.map((p) => p.y)) - YOU_H - 56 }
      : { x: width / 2 - YOU_W / 2, y: TOP };

    // cutia nodurilor (+ 32 margine); viewBox-ul e cel putin cat canvas-ul, ca un graf
    // mic sa nu fie marit, si centrat pe orizontala pe noduri
    let bx0 = youPos.x, by0 = youPos.y, bx1 = youPos.x + YOU_W, by1 = youPos.y + YOU_H;
    for (const p of pos.values()) {
      bx0 = Math.min(bx0, p.x); by0 = Math.min(by0, p.y);
      bx1 = Math.max(bx1, p.x + NODE_W); by1 = Math.max(by1, p.y + NODE_H);
    }
    bx0 -= 32; by0 -= 32; bx1 += 32; by1 += 32;
    // la aceeasi marime a canvas-ului cutia doar creste, ca o mutare sa nu rescaleze restul
    if (lastView && lastView.width === width && lastView.height === height) {
      const [a, b, c, d] = lastView.box;
      bx0 = Math.min(bx0, a); by0 = Math.min(by0, b); bx1 = Math.max(bx1, c); by1 = Math.max(by1, d);
    }
    lastView = { width, height, box: [bx0, by0, bx1, by1] };
    // viewBox-ul are proportiile canvas-ului; un graf mic nu se mareste (s >= 1), iar jos
    // ramane loc pentru panoul "How a task travels", ca sa nu acopere noduri
    drawTravel();
    const reserve = travel.hidden || travel.classList.contains("collapsed") ? 0 : travel.offsetHeight + 24;
    const s = Math.max(1, (bx1 - bx0) / width, (by1 - by0) / Math.max(120, height - reserve));
    fitBox = [(bx0 + bx1) / 2 - (width * s) / 2, by0, width * s, height * s];
    applyView();

    const defs = svg("defs", {},
      svg("marker", { id: "arrow", viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse" },
        svg("path", { class: "arrow", d: "M0 0 L10 5 L0 10 z" })),
      svg("marker", { id: "arrow-review", viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse" },
        svg("path", { class: "arrow review", d: "M0 0 L10 5 L0 10 z" })),
      svg("clipPath", { id: "node-clip" }, svg("rect", { x: 0, y: 0, width: NODE_W, height: NODE_H, rx: 12 })));

    const edgeLayer = svg("g", { class: "edges" });
    edges = [];
    const ids = new Set(agents.map((a) => a.id));
    // fiecare legatura: linia vizibila, o linie groasa invizibila (usor de nimerit)
    // si un x la mijloc, afisat la hover sau cand e selectata
    const addEdge = (kind, from, to, link, bend = false) => {
      const visible = svg("path", { class: `edge ${kind}`, "data-link": link, "marker-end": kind === "review" ? "url(#arrow-review)" : "url(#arrow)" });
      const hit = svg("path", {
        class: "edge-hit", "data-link": link,
        onclick: (e) => { e.stopPropagation(); selectedLink = link; syncLinkSelection(); wrap.focus(); },
      });
      const x = svg("g", { class: "edge-x", role: "button", "aria-label": "Delete link" },
        svg("circle", { r: 9 }), svg("path", { d: "M -3.5 -3.5 L 3.5 3.5 M 3.5 -3.5 L -3.5 3.5" }));
      x.addEventListener("pointerdown", (e) => e.stopPropagation());
      x.addEventListener("click", (e) => { e.stopPropagation(); selectedLink = link; deleteLink(); });
      edgeLayer.append(svg("g", { class: "edge-group", "data-link": link }, visible, hit, x));
      edges.push({ kind, from, to, els: [visible, hit], x, bend });
    };
    for (const a of agents) {
      if (a.parentId && ids.has(a.parentId)) addEdge("delegate", a.parentId, a.id, "p:" + a.id);
      else if (!a.parentId && !a.detached) addEdge("delegate", "you", a.id, "p:" + a.id);
      if (a.reviewerId && ids.has(a.reviewerId)) addEdge("review", a.reviewerId, a.id, "r:" + a.id, a.reviewerId === a.parentId);
    }

    nodeEls = new Map();
    const nodeLayer = svg("g", { class: "nodes" }, youNode(), agents.map(agentNode));
    svgEl.replaceChildren(defs, edgeLayer, nodeLayer);
    redrawEdges(); // dupa ce liniile sunt in pagina: pozitia x-ului cere getTotalLength
    if (selectedLink && !svgEl.querySelector(`.edge-hit[data-link="${selectedLink}"]`)) selectedLink = null;
    syncLinkSelection();
    syncSelection();
  }

  function youNode() {
    return svg("g", { class: "node-you", transform: `translate(${youPos.x} ${youPos.y})` },
      svg("rect", { class: "card", width: YOU_W, height: YOU_H, rx: YOU_H / 2 }),
      svg("text", { class: "you-label", x: YOU_W / 2, y: YOU_H / 2 + 5, "text-anchor": "middle" }, "You"),
      handle(YOU_W / 2, YOU_H, "you"));
  }

  function agentNode(a) {
    const p = pos.get(a.id);
    const status = (a.status || "idle").toLowerCase();
    const asks = (project.questions || []).some((q) => q.agentId === a.id);
    // task-ul run-ului activ (nu primul task In progress al agentului)
    const workingOn = a.workingTaskId ? (project.tasks || []).find((t) => t.id === a.workingTaskId) : null;
    const [tone, ink] = toneOf(a.name, kindOf(a));
    // linia de stare: ce face agentul acum (sau de ce sta)
    const state = a.detached ? ["detached", "asks"]
      : asks ? ["needs you", "asks"]
      : status === "working" ? [workingOn ? `working \u00b7 T-${workingOn.number}` : "working", "working"]
      : status === "queued" ? ["queued", "queued"]
      : ["idle", "idle"];
    const folder = (project.folders || []).length > 1 ? folderOf(project, a)?.name : "";
    const del = svg("g", { class: "node-x", role: "button", "aria-label": "Delete agent " + a.name, transform: `translate(${NODE_W - 2} 2)` },
      svg("circle", { r: 9 }), svg("path", { d: "M -3.5 -3.5 L 3.5 3.5 M 3.5 -3.5 L -3.5 3.5" }));
    del.addEventListener("pointerdown", (e) => e.stopPropagation()); // nu porneste mutarea nodului
    del.addEventListener("click", (e) => { e.stopPropagation(); onDelete(a.id); });
    const tip = [a.createdBy ? "Added by " + (project.agents.find((x) => x.id === a.createdBy)?.name || "a lead") : "",
      a.responsibilities ? "Takes: " + a.responsibilities : ""].filter(Boolean).join("\n");
    const g = svg("g", {
      class: "node" + (a.detached ? " detached" : "") + (status === "working" ? " working" : "") + (asks ? " asks" : ""),
      "data-agent": a.id, transform: `translate(${p.x} ${p.y})`,
    },
      tip ? svg("title", {}, tip) : null,
      svg("g", { "clip-path": "url(#node-clip)" },
        svg("rect", { class: "card", width: NODE_W, height: NODE_H, rx: 12 }),
        svg("circle", { class: "avatar-c", cx: 32, cy: NODE_H / 2, r: 17, style: `fill:${tone}` }),
        svg("text", { class: "avatar-t", x: 32, y: NODE_H / 2 + 5, "text-anchor": "middle", style: `fill:${ink}` }, (a.name || "?").charAt(0).toUpperCase()),
        svg("text", { class: "name", x: 60, y: 25 }, shorten(a.name || "", 17)),
        svg("text", { class: "sub", x: 60, y: 43 }, shorten([a.role, modelShort(a.model), folder].filter(Boolean).join(" \u00b7 "), 22)),
        svg("circle", { class: "status-dot " + state[1], cx: 64, cy: 56, r: 3 }),
        svg("text", { class: "state " + state[1], x: 72, y: 60 }, state[0])),
      handle(NODE_W / 2, NODE_H, a.id), del);
    g.addEventListener("pointerdown", (e) => startMove(e, a.id, g));
    g.addEventListener("dblclick", () => onEdit(a.id));
    nodeEls.set(a.id, g);
    return g;
  }

  function handle(cx, cy, sourceId) {
    const h = svg("circle", { class: "handle", cx, cy, r: 6 });
    h.addEventListener("pointerdown", (e) => startLink(e, sourceId, h));
    return h;
  }

  // selectia agentului si a liniei se schimba pe loc, fara sa reconstruim DOM-ul
  // (ca dublu-click-ul sa nu piarda nodul de sub cursor)
  function syncSelection() {
    for (const [id, g] of nodeEls) g.classList.toggle("selected", id === selectedId);
  }
  function syncLinkSelection() {
    for (const g of svgEl.querySelectorAll(".edges .edge-group")) g.classList.toggle("selected", g.dataset.link === selectedLink);
    for (const p of svgEl.querySelectorAll(".edges .edge")) p.classList.toggle("selected", p.dataset.link === selectedLink);
    deleteBtn.disabled = !selectedLink;
  }

  // ---------- mutare ----------
  function startMove(e, id, g) {
    if (e.button !== 0) return;
    closeMenu();
    const start = { cx: e.clientX, cy: e.clientY, p: toSvg(e), pos: { ...pos.get(id) } };
    let dragging = false;
    g.setPointerCapture(e.pointerId);

    const move = (ev) => {
      if (!dragging) {
        if (Math.hypot(ev.clientX - start.cx, ev.clientY - start.cy) < DRAG_PX) return;
        dragging = true; busy = true;
        g.classList.add("dragging");
      }
      const p = toSvg(ev);
      // ramane in viewBox-ul curent, cu marginea de 24 din draw(): la drop nu se
      // mareste viewBox-ul, deci nu se rescaleaza restul canvas-ului
      const vb = svgEl.viewBox.baseVal;
      const np = {
        x: Math.min(Math.max(start.pos.x + p.x - start.p.x, vb.x + 24), vb.x + vb.width - NODE_W - 24),
        y: Math.min(Math.max(start.pos.y + p.y - start.p.y, vb.y + 24), vb.y + vb.height - NODE_H - 24),
      };
      pos.set(id, np);
      g.setAttribute("transform", `translate(${np.x} ${np.y})`);
      redrawEdges();
    };
    const up = async (ev) => {
      g.removeEventListener("pointermove", move);
      g.removeEventListener("pointerup", up);
      g.removeEventListener("pointercancel", up);
      if (!dragging) {
        selectedLink = null; syncLinkSelection();
        onSelect(id);
        return;
      }
      // salvam nodul mutat si "inghetam" nodurile asezate automat: fara pozitie
      // salvata, autoLayout le-ar reimparti pe nivel si ar sari fara sa le atingi
      const toSave = project.agents
        .filter((a) => a.id === id || a.posX == null || a.posY == null)
        .map((a) => { const p = pos.get(a.id); return { id: a.id, x: Math.round(p.x), y: Math.round(p.y) }; });
      try {
        await Promise.all(toSave.map((s) => api(`/api/agents/${s.id}/position`, { method: "PUT", body: { x: s.x, y: s.y } })));
        // tinem pozitiile si in proiectul din memorie pana vine cel reincarcat
        for (const proj of [project, pendingProject]) {
          for (const s of toSave) {
            const a = proj?.agents.find((ag) => ag.id === s.id);
            if (a) { a.posX = s.x; a.posY = s.y; }
          }
        }
      } catch (err) {
        toast(err.message);
      }
      busy = false;
      flush(); // redesenam: pozitia noua, sau cea veche pe eroare
    };
    g.addEventListener("pointermove", move);
    g.addEventListener("pointerup", up);
    g.addEventListener("pointercancel", up);
  }

  // ---------- legare ----------
  function startLink(e, sourceId, h) {
    if (e.button !== 0) return;
    e.stopPropagation();
    e.preventDefault();
    closeMenu();
    busy = true;
    const r = rect(sourceId);
    const x1 = r.x + r.w / 2, y1 = r.y + r.h;
    const temp = svg("path", { class: "edge temp", d: vCurve(x1, y1, x1, y1) });
    svgEl.append(temp);
    h.setPointerCapture(e.pointerId);

    const move = (ev) => { const p = toSvg(ev); temp.setAttribute("d", vCurve(x1, y1, p.x, p.y)); };
    const up = (ev) => {
      h.removeEventListener("pointermove", move);
      h.removeEventListener("pointerup", up);
      h.removeEventListener("pointercancel", up);
      temp.remove();
      const under = document.elementFromPoint(ev.clientX, ev.clientY);
      const hit = under?.closest("[data-agent]");
      const targetId = hit ? Number(hit.dataset.agent) : null;
      busy = false;
      if (ev.type === "pointerup" && targetId && targetId !== sourceId) showMenu(sourceId, targetId, ev);
      else if (ev.type === "pointerup" && under === svgEl) {
        // lasat in gol: agent nou sub sursa (de pe "Tu": fara sef), centrat unde ai lasat
        const p = toSvg(ev);
        flush();
        onCreate({ x: Math.round(p.x - NODE_W / 2), y: Math.round(p.y) },
          { parentId: sourceId === "you" ? null : sourceId, detached: false });
      } else flush();
    };
    h.addEventListener("pointermove", move);
    h.addEventListener("pointerup", up);
    h.addEventListener("pointercancel", up);
  }

  const agentById = (id) => project.agents.find((a) => a.id === id);

  function showMenu(sourceId, targetId, e) {
    const target = agentById(targetId);
    if (!target) return;
    const w = wrap.getBoundingClientRect();
    menu = el("div", { class: "link-menu", style: `left:${Math.round(e.clientX - w.left)}px; top:${Math.round(e.clientY - w.top)}px` },
      el("button", { type: "button", "data-kind": "delegate", onclick: () => choose({ parentId: sourceId === "you" ? null : sourceId, detached: false }) }, "Delegate to " + target.name),
      sourceId !== "you" && el("button", { type: "button", "data-kind": "review", onclick: () => choose({ reviewerId: sourceId }) }, "Review work of " + target.name),
      el("button", { type: "button", "data-kind": "cancel", onclick: closeMenu }, "Cancel"));
    wrap.append(menu);
    // in limitele canvas-ului (dupa ce are dimensiuni)
    const left = Math.max(0, Math.min(e.clientX - w.left, w.width - menu.offsetWidth - 4));
    const top = Math.max(0, Math.min(e.clientY - w.top, w.height - menu.offsetHeight - 4));
    menu.style.left = Math.round(left) + "px";
    menu.style.top = Math.round(top) + "px";
    // click in afara meniului il inchide (dupa ce evenimentul curent s-a terminat)
    setTimeout(() => document.addEventListener("pointerdown", outside, true), 0);
    async function choose(patch) {
      closeMenu();
      await saveLinks(target, patch);
    }
  }
  const outside = (e) => { if (menu && !menu.contains(e.target)) closeMenu(); };
  function closeMenu() {
    document.removeEventListener("pointerdown", outside, true);
    if (!menu) return;
    menu.remove();
    menu = null;
    wrap.focus();
    flush();
  }

  // saveLinks: acelasi drum pentru meniu si pentru stergere
  async function saveLinks(target, patch) {
    try {
      await api(`/api/agents/${target.id}`, { method: "PUT", body: agentBody(target, patch) });
    } catch (err) {
      toast(err.message); // ex. "legatura ar crea un ciclu"
    }
    await onChanged(); // si pe eroare: redesenam starea reala
  }

  function deleteLink() {
    if (!selectedLink) return;
    const [kind, idText] = selectedLink.split(":");
    const agent = agentById(Number(idText));
    selectedLink = null;
    syncLinkSelection();
    wrap.focus(); // butonul se dezactiveaza: pastram focusul pe canvas
    if (!agent) return;
    // fara sef, agentul ramane nelegat (nu devine automat lider sub "Tu")
    saveLinks(agent, kind === "p" ? UNLINKED : { reviewerId: null });
  }

  // ---------- zoom si deplasare ----------
  function applyView() {
    if (!fitBox) return;
    const [x, y, w, h] = fitBox;
    const vw = w / zoom, vh = h / zoom;
    // la zoom 1 graful sta sus (ca inainte); mai aproape, centrul + deplasarea
    const cx = x + w / 2 + panX, cy = zoom === 1 ? y + vh / 2 + panY : y + h / 2 + panY;
    svgEl.setAttribute("viewBox", `${cx - vw / 2} ${cy - vh / 2} ${vw} ${vh}`);
    zoomLabel.textContent = Math.round(zoom * 100) + "%";
  }
  function setZoom(z) {
    zoom = Math.min(4, Math.max(0.4, z));
    if (Math.abs(zoom - 1) < 0.01) zoom = 1;
    closeMenu();
    applyView();
  }
  // tras de gol = deplasare; un click fara miscare ramane click (deselectare, dublu-click)
  svgEl.addEventListener("pointerdown", (e) => {
    if (e.target !== svgEl || e.button !== 0 || busy) return;
    const start = { x: e.clientX, y: e.clientY, panX, panY };
    const scale = svgEl.viewBox.baseVal.width / (svgEl.clientWidth || 1);
    let moved = false;
    const move = (ev) => {
      const dx = ev.clientX - start.x, dy = ev.clientY - start.y;
      if (!moved && Math.hypot(dx, dy) < DRAG_PX) return;
      moved = true;
      svgEl.classList.add("panning");
      panX = start.panX - dx * scale;
      panY = start.panY - dy * scale;
      applyView();
    };
    const up = () => {
      svgEl.removeEventListener("pointermove", move);
      svgEl.removeEventListener("pointerup", up);
      svgEl.removeEventListener("pointercancel", up);
      svgEl.classList.remove("panning");
    };
    svgEl.setPointerCapture(e.pointerId);
    svgEl.addEventListener("pointermove", move);
    svgEl.addEventListener("pointerup", up);
    svgEl.addEventListener("pointercancel", up);
  });
  // Ctrl/Cmd + rotita = zoom (rotita simpla ramane derularea paginii)
  svgEl.addEventListener("wheel", (e) => {
    if (!e.ctrlKey && !e.metaKey) return;
    e.preventDefault();
    setZoom(zoom * (e.deltaY < 0 ? 1.1 : 1 / 1.1));
  }, { passive: false });

  // drawTravel: You -> goal -> radacina -> task -> un agent -> done -> reviewer-ul lui -> radacina -> Done
  function drawTravel() {
    const agents = project.agents || [];
    const byId = new Map(agents.map((a) => [a.id, a]));
    const roots = agents.filter((a) => !a.parentId && !a.detached);
    const root = roots[0];
    const worker = agents.find((a) => a.parentId && byId.get(a.parentId)?.id === root?.id && a.reviewerId) ||
      agents.find((a) => a.parentId === root?.id);
    if (!root || !worker || travelHidden) {
      travel.hidden = !root || !worker;
      travelToggle.textContent = "How a task travels";
      travel.replaceChildren(travelToggle);
      travel.classList.add("collapsed");
      return;
    }
    travel.hidden = false;
    travel.classList.remove("collapsed");
    const chip = (a) => el("span", { class: "travel-chip" }, el("span", { class: "dot", style: `background:${toneOf(a.name, kindOf(a))[0]}` }), a.name);
    const step = (t, cls = "") => el("span", { class: "travel-step " + cls }, t);
    const reviewer = byId.get(worker.reviewerId);
    travelToggle.textContent = "\u00d7";
    travelToggle.setAttribute("aria-label", "Hide");
    travel.replaceChildren(
      el("div", { class: "travel-head" }, el("strong", {}, "How a task travels"), travelToggle),
      el("div", { class: "travel-path" },
        el("span", { class: "travel-chip" }, "You"), step("\u2192 goal \u2192"), chip(root), step("\u2192 task \u2192"), chip(worker), step("\u2192 done \u2192"),
        reviewer ? [chip(reviewer), step("approves \u2192", "ok")] : null,
        chip(root), step("accepts \u2192 Done")),
      el("p", {}, `${reviewer ? "Rejected reviews go back to the assignee with notes. " : ""}Questions go up the solid line; only ${roots.map((a) => a.name).join(", ")} ${roots.length === 1 ? "talks" : "talk"} to you.`),
      el("p", { class: "travel-tip" }, "Drag \u25CF onto an agent to link it, or onto empty space for a new agent under it \u00b7 double-click empty space = detached agent \u00b7 drag empty space to move around."));
  }

  // ---------- evenimente pe canvas ----------
  svgEl.addEventListener("click", (e) => {
    if (e.target === svgEl) { selectedLink = null; syncLinkSelection(); }
  });
  // dublu-click pe gol: agent nou cu centrul in punctul respectiv
  svgEl.addEventListener("dblclick", (e) => {
    if (e.target !== svgEl) return;
    const p = toSvg(e);
    onCreate({ x: Math.round(p.x - NODE_W / 2), y: Math.round(p.y - NODE_H / 2) }, UNLINKED);
  });
  wrap.addEventListener("keydown", (e) => {
    if (e.key === "Escape") {
      if (menu) closeMenu();
      else if (wrap.classList.contains("full")) setFull(false);
    } else if ((e.key === "Delete" || e.key === "Backspace") && selectedLink) {
      e.preventDefault();
      deleteLink();
    }
  });

  // flush deseneaza proiectul retinut cat timp un drag a fost activ
  function flush() {
    if (busy) return;
    if (pendingProject) { project = pendingProject; pendingProject = null; }
    draw();
  }

  const observer = new ResizeObserver(() => { if (!busy) draw(); });
  observer.observe(wrap);

  return {
    el: wrap,
    render(next, selected) {
      selectedId = selected;
      if (busy || menu) { pendingProject = next; syncSelection(); return; }
      // aceeasi data, alta selectie: doar actualizam clasele
      if (next === project && nodeEls.size) { syncSelection(); return; }
      project = next;
      draw();
    },
    destroy() {
      observer.disconnect();
      document.removeEventListener("keydown", fullEscape);
      document.removeEventListener("pointerdown", outside, true);
      menu?.remove();
    },
  };
}
