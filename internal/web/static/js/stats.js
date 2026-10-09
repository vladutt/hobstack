// Tab-ul Stats al unui proiect: cheltuiala, performanta echipei si activitate pe o perioada.
// Graficele sunt SVG desenat de mana, fara librarii.
import { api } from "./api.js";
import { el, svg, toneOf } from "./ui.js";

const RANGES = [["today", "Today"], ["7d", "7 days"], ["30d", "30 days"], ["all", "All time"]];
const PREV_LABEL = { today: "vs yesterday", "7d": "vs prev 7 days", "30d": "vs prev 30 days" };
const RANGE_KEY = "hobstack-stats-range";
const TASK_STATUS = { queued: "Queued", in_progress: "In progress", review: "Review", blocked: "Blocked", done: "Done" };

function savedRange() {
  try {
    const v = localStorage.getItem(RANGE_KEY);
    return RANGES.some(([id]) => id === v) ? v : "7d";
  } catch { return "7d"; }
}
function saveRange(v) {
  try { localStorage.setItem(RANGE_KEY, v); } catch {}
}

// ---------- formatare ----------
const money = (v) => "$" + (v || 0).toFixed(2);
function compact(n) {
  n = n || 0;
  if (n >= 1e6) return (n / 1e6).toFixed(1) + "M";
  if (n >= 1e3) return (n / 1e3).toFixed(1) + "k";
  return String(n);
}
// duration: 2d 4h, 3h 12m, 45m, 30s
function duration(sec) {
  sec = Math.round(sec || 0);
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60);
  if (d) return `${d}d ${h}h`;
  if (h) return `${h}h ${m}m`;
  if (m) return `${m}m`;
  return `${sec}s`;
}
// axisMoney: etichetele scurte de pe axa Y ($5k, $250, $0.5)
const axisMoney = (v) => (v >= 1000 ? "$" + compact(v) : "$" + Number(v.toFixed(2)));
const successRate = (t) => (t.runsDone + t.runsFailed ? t.runsDone / (t.runsDone + t.runsFailed) : null);

function bucketLabel(iso, bucket) {
  const d = new Date(iso);
  if (bucket === "hour") return d.toLocaleTimeString("en", { hour: "2-digit", minute: "2-digit", hour12: false });
  return d.toLocaleDateString("en", { month: "short", day: "numeric" });
}

// ---------- grafice ----------
const W = 720, H = 200, PAD = { l: 48, r: 8, t: 10, b: 26 };

// niceMax: o valoare rotunda >= v pentru axa Y
function niceMax(v) {
  if (!(v > 0)) return 1;
  const p = 10 ** Math.floor(Math.log10(v));
  return [1, 2, 2.5, 5, 10].map((m) => m * p).find((x) => x >= v);
}

// axes: liniile orizontale cu etichete pe Y si etichetele bucket-urilor pe X (max ~8)
function axes(series, bucket, max, fmt) {
  const iw = W - PAD.l - PAD.r, ih = H - PAD.t - PAD.b, slot = iw / series.length;
  const out = [];
  for (let k = 0; k <= 4; k++) {
    const y = PAD.t + ih - (ih * k) / 4;
    out.push(svg("line", { x1: PAD.l, x2: W - PAD.r, y1: y, y2: y, class: "stats-grid" }));
    out.push(svg("text", { x: PAD.l - 6, y: y + 4, "text-anchor": "end", class: "stats-axis" }, fmt((max * k) / 4)));
  }
  const step = Math.ceil(series.length / 8);
  series.forEach((b, i) => {
    if (i % step) return;
    out.push(svg("text", { x: PAD.l + slot * (i + 0.5), y: H - 8, "text-anchor": "middle", class: "stats-axis" }, bucketLabel(b.start, bucket)));
  });
  return out;
}

// spendChart: bare stivuite pe agent, culorile avatarelor
function spendChart(d, agentKind) {
  const s = d.series;
  const totals = s.map((b) => Object.values(b.costBy || {}).reduce((a, v) => a + v, 0));
  const max = niceMax(Math.max(0, ...totals));
  const iw = W - PAD.l - PAD.r, ih = H - PAD.t - PAD.b, slot = iw / s.length;
  const bw = Math.max(2, Math.min(28, slot * 0.7));
  const y = (v) => PAD.t + ih - (v / max) * ih;
  const bars = [];
  s.forEach((b, i) => {
    const x = PAD.l + i * slot + (slot - bw) / 2;
    let acc = 0;
    for (const a of d.agents) {
      const v = (b.costBy || {})[a.agentId] || 0;
      if (!v) continue;
      bars.push(svg("rect", { x, y: y(acc + v), width: bw, height: Math.max(1, y(acc) - y(acc + v)), fill: toneOf(a.name, agentKind(a.agentId))[0] },
        svg("title", {}, `${a.name}: ${money(v)} · ${bucketLabel(b.start, d.bucket)}`)));
      acc += v;
    }
  });
  return svg("svg", { viewBox: `0 0 ${W} ${H}`, class: "stats-chart", role: "img", "aria-label": "Spend over time" },
    axes(s, d.bucket, max, axisMoney), bars);
}

// throughputChart: doua linii, task-uri create si terminate
function throughputChart(d) {
  const s = d.series;
  const max = niceMax(Math.max(0, ...s.map((b) => Math.max(b.created, b.done))));
  const iw = W - PAD.l - PAD.r, ih = H - PAD.t - PAD.b, slot = iw / s.length;
  const pt = (i, v) => [PAD.l + slot * (i + 0.5), PAD.t + ih - (v / max) * ih];
  const line = (key) => [
    svg("polyline", { class: `stats-line ${key}`, points: s.map((b, i) => pt(i, b[key]).join(",")).join(" ") }),
    ...s.map((b, i) => {
      const [cx, cy] = pt(i, b[key]);
      return svg("circle", { cx, cy, r: 3, class: `stats-dot ${key}` },
        svg("title", {}, `${key === "created" ? "Created" : "Done"}: ${b[key]} · ${bucketLabel(b.start, d.bucket)}`));
    }),
  ];
  return svg("svg", { viewBox: `0 0 ${W} ${H}`, class: "stats-chart", role: "img", "aria-label": "Tasks created and done" },
    axes(s, d.bucket, max, (v) => (Number.isInteger(v) ? String(v) : "")), line("created"), line("done"));
}

// ---------- sectiuni ----------
const TILES = [
  ["Spend", (t) => t.costUsd, money],
  ["Tokens", (t) => t.tokens, compact],
  ["Tasks done", (t) => t.tasksDone, String],
  ["Median time to done", (t) => t.medianDoneSec || null, duration],
  ["Run success", successRate, (v) => Math.round(v * 100) + "%"],
  ["Questions to you", (t) => t.questions, String],
];

function tiles(d, range) {
  return el("div", { class: "stats-tiles" }, TILES.map(([label, get, fmt]) => {
    const cur = get(d.totals);
    const prev = d.prev ? get(d.prev) : null;
    let delta = null;
    if (prev && cur != null) {
      const pct = Math.round(((cur - prev) / prev) * 100);
      delta = el("div", { class: "stats-delta" }, `${pct > 0 ? "+" : ""}${pct}% ${PREV_LABEL[range]}`);
    }
    return el("div", { class: "stats-tile" },
      el("div", { class: "stats-tile-label" }, label),
      el("div", { class: "stats-tile-value" }, cur == null ? "—" : fmt(cur)),
      delta);
  }));
}

function legend(d, agentKind) {
  return el("div", { class: "stats-legend" }, d.agents.filter((a) => a.costUsd > 0).map((a) =>
    el("span", {}, el("i", { style: `background:${toneOf(a.name, agentKind(a.agentId))[0]}` }), a.name)));
}

const COLS = [
  ["name", "Agent", null], ["runs", "Runs", String], ["failed", "Failed", String], ["costUsd", "Cost", money],
  ["tokens", "Tokens", compact], ["seconds", "Work time", duration], ["tasksDone", "Done", String],
  ["sendBacks", "Sent back", String], ["filesChanged", "Files", String],
];

function card(title, ...children) {
  return el("section", { class: "stats-card" }, el("h3", {}, title), children);
}

// usageCard: limitele contului Claude al proiectului (fereastra de 5 ore si cea saptamanala),
// din ultimul run care le-a raportat. d.usage null = niciun run inca.
function usageCard(d) {
  const title = `Claude usage \u00b7 ${d.account || "Default"}`;
  const u = d.usage;
  if (!u) return card(title, el("p", { class: "muted" }, "No usage data yet; it appears after an agent's first run."));
  return card(title,
    el("div", { class: "usage-rows" },
      usageRow("5-hour window", u.fiveHourUsed, u.fiveHourResets, u.status, false),
      usageRow("Weekly", u.sevenDayUsed, u.sevenDayResets, u.status, true)),
    el("p", { class: "usage-updated muted" }, `Updated ${ago(u.updatedAt)} (from the latest agent run)`));
}

// usageRow: bara unei ferestre. Chihlimbar peste 80% sau la avertisment, rosu la 100% / limita atinsa.
function usageRow(label, used, resets, status, weekly) {
  const pct = Math.max(0, Math.min(100, Math.round((used || 0) * 100)));
  const full = pct >= 100 || (status === "rejected" && pct >= 99);
  const level = full ? "bad" : pct >= 80 || (status === "allowed_warning" && pct >= 50) ? "warn" : "ok";
  const when = resetText(resets, weekly);
  const text = full ? `Limit reached${when ? ` \u00b7 resets ${when}` : ""}`
    : `${pct}% used \u00b7 ${100 - pct}% left${when ? ` \u00b7 resets ${when}` : ""}`;
  return el("div", { class: "usage-row" },
    el("div", { class: "usage-head" }, el("span", { class: "usage-label" }, label), el("span", { class: `usage-text ${level}` }, text)),
    el("div", { class: "usage-bar", role: "meter", "aria-label": label, "aria-valuemin": "0", "aria-valuemax": "100", "aria-valuenow": String(pct) },
      el("i", { class: level, style: `width:${pct}%` })));
}

// resetText: "at 14:30" (azi), "tomorrow 09:00", altfel "Mon 18:00"; "" daca necunoscut
function resetText(iso, weekly) {
  const t = Date.parse(iso);
  if (!t || t < Date.UTC(2001, 0, 1)) return "";
  const d = new Date(t);
  const hm = d.toLocaleTimeString("en-GB", { hour: "2-digit", minute: "2-digit" });
  const today = new Date();
  const tomorrow = new Date(today.getFullYear(), today.getMonth(), today.getDate() + 1);
  if (d.toDateString() === today.toDateString()) return `at ${hm}`;
  if (d.toDateString() === tomorrow.toDateString()) return `tomorrow ${hm}`;
  const day = d.toLocaleDateString("en-US", weekly ? { weekday: "short", month: "short", day: "numeric" } : { weekday: "short" });
  return `${day} ${hm}`;
}

function ago(iso) {
  const s = Math.max(0, (Date.now() - Date.parse(iso)) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86400)} d ago`;
}

// statsView: corpul tab-ului. load() aduce datele pentru perioada aleasa.
// agentKind(id) -> "lead" / "worker", pentru culoarea agentului.
export function statsView(projectId, { agentKind, agentRole = () => "" }) {
  const state = { range: savedRange(), sort: "costUsd", data: null };
  const rangeBar = el("div", { class: "stats-ranges", role: "group", "aria-label": "Time range" });
  const content = el("div", { class: "stats-content" });
  const root = el("section", { class: "tab-body stats-body" },
    el("div", { class: "stats-toolbar" }, el("h2", {}, "Stats"), el("span", { class: "grow" }), rangeBar),
    content);
  let seq = 0; // un raspuns vechi (alt range) nu suprascrie unul nou

  function renderRanges() {
    rangeBar.replaceChildren(...RANGES.map(([id, label]) => el("button", {
      type: "button", class: "btn small ghost", "aria-pressed": String(id === state.range),
      onclick: () => { if (id === state.range) return; state.range = id; saveRange(id); load(); },
    }, label)));
  }

  async function load() {
    renderRanges();
    const my = ++seq;
    if (!state.data) content.replaceChildren(el("p", { class: "muted" }, "Loading…"));
    content.classList.add("loading");
    try {
      const data = await api(`/api/projects/${projectId}/stats?range=${state.range}`);
      if (my !== seq) return;
      state.data = data;
      render();
    } catch (err) {
      if (my !== seq) return;
      content.replaceChildren(el("div", { class: "stats-error" },
        el("p", {}, `Couldn't load stats: ${err.message}`),
        el("button", { type: "button", class: "btn small", onclick: load }, "Retry")));
    } finally {
      if (my === seq) content.classList.remove("loading");
    }
  }

  function agentsTable(d) {
    const key = state.sort;
    const rows = [...d.agents].sort((a, b) => (key === "name" ? a.name.localeCompare(b.name) : b[key] - a[key]));
    return el("div", { class: "stats-table-wrap" }, el("table", { class: "stats-table" },
      el("thead", {}, el("tr", {}, COLS.map(([k, label]) => el("th", { "aria-sort": k === key ? (k === "name" ? "ascending" : "descending") : null },
        el("button", { type: "button", onclick: () => { state.sort = k; render(); } }, label))))),
      el("tbody", {}, rows.map((a) => el("tr", {}, COLS.map(([k, , fmt]) => el("td", {},
        fmt ? fmt(a[k]) : agentCell(a))))))));
  }

  // agentCell: numele (link catre agent) si rolul, mai sters; agentul sters nu mai are rol
  function agentCell(a) {
    const role = agentRole(a.agentId);
    return [el("a", { href: `#/p/${projectId}/team/${a.agentId}` }, a.name),
      role ? el("span", { class: "stats-role" }, ` (${role})`) : null];
  }

  function topTasks(d) {
    if (!d.topTasks.length) return el("p", { class: "muted" }, "No task runs in this range.");
    return el("ol", { class: "stats-list" }, d.topTasks.map((t) => el("li", {},
      el("a", { href: `#/p/${projectId}/tasks/${t.taskId}` }, el("span", { class: "muted" }, `T-${t.number} `), t.title),
      el("span", { class: "grow" }),
      el("span", { class: `pill stats-status st-${t.status}` }, TASK_STATUS[t.status] || t.status),
      el("span", { class: "muted" }, `${t.runs} ${t.runs === 1 ? "run" : "runs"}`),
      el("strong", {}, money(t.costUsd)))));
  }

  function activity(d) {
    const t = d.totals;
    return [
      el("div", { class: "stats-facts" },
        el("div", {}, el("strong", {}, String(t.filesChanged)), " files changed"),
        el("div", {}, el("strong", {}, String(t.commands)), " commands", t.commandsFailed ? el("span", { class: "muted" }, ` (${t.commandsFailed} failed)`) : null),
        el("div", {}, el("strong", {}, String(t.questions)), " questions asked · ", el("strong", {}, String(t.answered)), " answered")),
      d.topFiles.length
        ? el("ol", { class: "stats-list" }, d.topFiles.map((f) => el("li", {},
          el("code", { title: f.path }, el("bdi", { dir: "ltr" }, f.path)), el("span", { class: "grow" }), el("span", { class: "muted" }, `${f.changes}×`))))
        : null,
    ];
  }

  function render() {
    const d = state.data;
    const t = d.totals;
    const empty = !d.agents.length && !t.tasksCreated && !t.tasksDone && !t.filesChanged && !t.commands && !t.questions && !t.answered;
    if (empty) {
      content.replaceChildren(usageCard(d), tiles(d, state.range), el("p", { class: "stats-empty muted" }, "No activity in this range."));
      return;
    }
    content.replaceChildren(
      usageCard(d),
      tiles(d, state.range),
      el("div", { class: "stats-grid-2" },
        card("Spend over time", spendChart(d, agentKind), legend(d, agentKind)),
        card("Throughput", throughputChart(d),
          el("div", { class: "stats-legend" },
            el("span", {}, el("i", { class: "created" }), "Created"),
            el("span", {}, el("i", { class: "done" }), "Done")))),
      card("Agents", agentsTable(d)),
      el("div", { class: "stats-grid-2" },
        card("Top tasks by cost", topTasks(d)),
        card("Activity", activity(d))));
  }

  return { el: root, load };
}
