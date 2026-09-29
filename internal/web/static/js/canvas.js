// Canvas-ul echipei: graf SVG cu nodul "Tu" sus, agentii ca noduri, delegari
// (linie plina cu sageata) si review-uri (linie punctata amber).
// Totul se construieste cu svg()/el(), niciodata cu innerHTML.
import { api } from "./api.js";
import { el, svg, kindOf, agentBody, toast } from "./ui.js";

const NODE_W = 184, NODE_H = 64, YOU_W = 64, YOU_H = 30, TOP = 16, LEVEL = 110;
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

export function teamCanvas({ onSelect, onEdit, onChanged }) {
  let project = { agents: [] };
  let selectedId = null;
  let selectedLink = null;   // "p:<id>" sau "r:<id>"
  let pendingProject = null; // proiect primit cat timp un drag e activ
  let busy = false;          // mutare sau legare in curs
  let pos = new Map();       // id -> {x, y} (in coordonate SVG)
  let youPos = { x: 0, y: TOP };
  let edges = [];            // {kind, from, to, els: [path...]}
  let nodeEls = new Map();   // id -> g.node
  let menu = null;

  const svgEl = svg("svg", { class: "team-canvas", preserveAspectRatio: "xMidYMin meet", role: "img", "aria-label": "Graful echipei" });
  const deleteBtn = el("button", { type: "button", class: "btn small canvas-delete", disabled: true, onclick: () => deleteLink() }, "Sterge legatura");
  const fullBtn = el("button", { type: "button", class: "btn small canvas-full-toggle", onclick: () => setFull(!wrap.classList.contains("full")) }, "Ecran mare");
  const legend = (cls, label) => el("span", { class: "legend-item" },
    svg("svg", { width: 28, height: 8, "aria-hidden": "true" }, svg("line", { x1: 1, y1: 4, x2: 27, y2: 4, class: "edge " + cls })), label);
  const bar = el("div", { class: "canvas-bar" }, legend("delegate", "Delegare"), legend("review", "Review"), el("span", { class: "grow" }), deleteBtn, fullBtn);
  const wrap = el("div", { class: "canvas-wrap", tabindex: "0" }, bar, svgEl);

  function setFull(on) {
    wrap.classList.toggle("full", on);
    fullBtn.textContent = on ? "Inchide" : "Ecran mare";
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

  function edgePath(kind, from, to) {
    const a = rect(from), b = rect(to);
    if (!a || !b) return "";
    if (kind === "delegate") return vCurve(a.x + a.w / 2, a.y + a.h, b.x + b.w / 2, b.y);
    // review: ancorat pe laturile orientate una spre alta
    const dx = b.x + b.w / 2 - (a.x + a.w / 2), dy = b.y + b.h / 2 - (a.y + a.h / 2);
    if (Math.abs(dy) > Math.abs(dx)) {
      return dy > 0
        ? vCurve(a.x + a.w / 2, a.y + a.h, b.x + b.w / 2, b.y)
        : vCurve(a.x + a.w / 2, a.y, b.x + b.w / 2, b.y + b.h);
    }
    return dx > 0
      ? hCurve(a.x + a.w, a.y + a.h / 2, b.x, b.y + b.h / 2)
      : hCurve(a.x, a.y + a.h / 2, b.x + b.w, b.y + b.h / 2);
  }

  function redrawEdges() {
    for (const e of edges) for (const p of e.els) p.setAttribute("d", edgePath(e.kind, e.from, e.to));
  }

  // ---------- desenare ----------
  function draw() {
    const width = wrap.clientWidth, height = svgEl.clientHeight;
    if (!width || !height) return; // inca nu e in pagina: ResizeObserver ne cheama
    const agents = project.agents;
    pos = autoLayout(agents, width);
    youPos = { x: width / 2 - YOU_W / 2, y: TOP };

    // reuniunea [0,0,width,height] cu marginile nodurilor + 24
    let minX = 0, minY = 0, maxX = width, maxY = height;
    for (const p of pos.values()) {
      minX = Math.min(minX, p.x - 24); minY = Math.min(minY, p.y - 24);
      maxX = Math.max(maxX, p.x + NODE_W + 24); maxY = Math.max(maxY, p.y + NODE_H + 24);
    }
    svgEl.setAttribute("viewBox", `${minX} ${minY} ${maxX - minX} ${maxY - minY}`);

    const defs = svg("defs", {},
      svg("marker", { id: "arrow", viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse" },
        svg("path", { class: "arrow", d: "M0 0 L10 5 L0 10 z" })),
      svg("marker", { id: "arrow-review", viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse" },
        svg("path", { class: "arrow review", d: "M0 0 L10 5 L0 10 z" })),
      svg("clipPath", { id: "node-clip" }, svg("rect", { x: 0, y: 0, width: NODE_W, height: NODE_H, rx: 10 })));

    const edgeLayer = svg("g", { class: "edges" });
    edges = [];
    const ids = new Set(agents.map((a) => a.id));
    const addEdge = (kind, from, to, link, deletable) => {
      const visible = svg("path", { class: `edge ${kind}`, "data-link": link, "marker-end": kind === "review" ? "url(#arrow-review)" : "url(#arrow)" });
      const els = [visible];
      edgeLayer.append(visible);
      if (deletable) {
        const hit = svg("path", {
          class: "edge-hit", "data-link": link,
          onclick: (e) => { e.stopPropagation(); selectedLink = link; syncLinkSelection(); wrap.focus(); },
        });
        edgeLayer.append(hit);
        els.push(hit);
      }
      edges.push({ kind, from, to, els });
    };
    for (const a of agents) {
      if (a.parentId && ids.has(a.parentId)) addEdge("delegate", a.parentId, a.id, "p:" + a.id, true);
      else if (!a.parentId) addEdge("delegate", "you", a.id, "p:" + a.id, false);
      if (a.reviewerId && ids.has(a.reviewerId)) addEdge("review", a.reviewerId, a.id, "r:" + a.id, true);
    }
    redrawEdges();

    nodeEls = new Map();
    const nodeLayer = svg("g", { class: "nodes" }, youNode(), agents.map(agentNode));
    svgEl.replaceChildren(defs, edgeLayer, nodeLayer);
    if (selectedLink && !svgEl.querySelector(`.edge-hit[data-link="${selectedLink}"]`)) selectedLink = null;
    syncLinkSelection();
    syncSelection();
  }

  function youNode() {
    return svg("g", { class: "node-you", transform: `translate(${youPos.x} ${youPos.y})` },
      svg("rect", { class: "card", width: YOU_W, height: YOU_H, rx: 8 }),
      svg("text", { class: "you-label", x: YOU_W / 2, y: YOU_H / 2 + 4, "text-anchor": "middle" }, "Tu"),
      handle(YOU_W / 2, YOU_H, "you"));
  }

  function agentNode(a) {
    const p = pos.get(a.id);
    const dot = "status-dot " + (a.status || "idle").toLowerCase();
    const g = svg("g", { class: "node", "data-agent": a.id, transform: `translate(${p.x} ${p.y})` },
      svg("g", { "clip-path": "url(#node-clip)" },
        svg("rect", { class: "card", width: NODE_W, height: NODE_H, rx: 10 }),
        svg("circle", { class: "avatar-c " + kindOf(a), cx: 24, cy: 32, r: 14 }),
        svg("text", { class: "avatar-t " + kindOf(a), x: 24, y: 37, "text-anchor": "middle" }, (a.name || "?").charAt(0).toUpperCase()),
        svg("text", { class: "name", x: 46, y: 24 }, shorten(a.name || "", 16)),
        svg("text", { class: "sub", x: 46, y: 40 }, shorten(a.role || "")),
        svg("text", { class: "sub", x: 46, y: 54 }, a.responsibilities ? shorten("Ce ia: " + a.responsibilities) : ""),
        svg("circle", { class: dot, cx: NODE_W - 14, cy: 14, r: 4 })),
      handle(NODE_W / 2, NODE_H, a.id));
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
      // ramane in viewBox-ul curent, ca nodul sa nu dispara pana la drop
      const vb = svgEl.viewBox.baseVal;
      const np = {
        x: Math.min(Math.max(start.pos.x + p.x - start.p.x, vb.x), vb.x + vb.width - NODE_W),
        y: Math.min(Math.max(start.pos.y + p.y - start.p.y, vb.y), vb.y + vb.height - NODE_H),
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
      const np = pos.get(id);
      const x = Math.round(np.x), y = Math.round(np.y);
      try {
        await api(`/api/agents/${id}/position`, { method: "PUT", body: { x, y } });
        // tinem pozitia si in proiectul din memorie pana vine cel reincarcat
        for (const proj of [project, pendingProject]) {
          const a = proj?.agents.find((ag) => ag.id === id);
          if (a) { a.posX = x; a.posY = y; }
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
      const hit = document.elementFromPoint(ev.clientX, ev.clientY)?.closest("[data-agent]");
      const targetId = hit ? Number(hit.dataset.agent) : null;
      busy = false;
      if (ev.type === "pointerup" && targetId && targetId !== sourceId) showMenu(sourceId, targetId, ev);
      else flush();
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
      el("button", { type: "button", "data-kind": "delegate", onclick: () => choose({ parentId: sourceId === "you" ? null : sourceId }) }, "Delegheaza catre " + target.name),
      sourceId !== "you" && el("button", { type: "button", "data-kind": "review", onclick: () => choose({ reviewerId: sourceId }) }, "Verifica munca lui " + target.name),
      el("button", { type: "button", "data-kind": "cancel", onclick: closeMenu }, "Renunta"));
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
    saveLinks(agent, kind === "p" ? { parentId: null } : { reviewerId: null });
  }

  // ---------- evenimente pe canvas ----------
  svgEl.addEventListener("click", (e) => {
    if (e.target === svgEl) { selectedLink = null; syncLinkSelection(); }
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
