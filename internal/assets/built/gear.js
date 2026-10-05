// The gear builder's island (AOC-051) — the first JavaScript island on the site (docs/architecture.md
// § Rendering). Mounted on [data-island="gear"]; no framework, no bundler, served as it is written.
//
// ⭐ It decides nothing. Where an item goes, what a two-hander holds, what a class cannot wear and what
// is summed are the server's (internal/builds), and the pane is the server's HTML: this file only
// sends what the reader did and keeps what a browser alone can keep. Without it the builder still
// works — every control in the pane is a link or a form field, and each row's "+" is a link.
//
// What only it does:
//   - drag a list row onto a slot → the request a "+" makes, naming the slot (add=<slot>:<id>);
//   - "share armor link" copies the build's link (without a script it is a plain link);
//   - several builds, as the design draws them (#1 … #5, ‹ ›, +), kept in this browser and shared by
//     its tabs. Each has an id that rides in the URL (gear_id), so a page only ever writes the build its
//     URL names; the rest wait in localStorage. A shared link opens as a build of its own and never
//     overwrites one.

const KEY = "aoc-gear-builds";
const MAX = 5; // the design's builds (maxBuilds)

const pane = document.querySelector('[data-island="gear"]');
if (pane) {
  if (window.htmx) mount();
  else document.addEventListener("DOMContentLoaded", mount);
}

function mount() {
  const body = () => document.getElementById("gear-body");

  // The build the server drew: the pane's hidden inputs (`gear`, `gear_id`) and the class picker, as
  // {id, qs} — qs without the id. null when the URL carries no build.
  function pageBuild() {
    const b = body();
    if (!b) return null;
    const q = new URLSearchParams();
    const gear = b.querySelectorAll('input[type=hidden][name="gear"]');
    gear.forEach((i) => q.append("gear", i.value));
    const c = b.querySelector('select[name="gear_class"]');
    if (c && c.value) q.set("gear_class", c.value);
    const idIn = b.querySelector('input[type=hidden][name="gear_id"]');
    const id = idIn ? idIn.value : "";
    if (!gear.length && !(c && c.value) && !id) return null;
    return { id: id, qs: q.toString() };
  }
  const items = (qs) => new URLSearchParams(qs).getAll("gear").filter((v) => v !== "").length;
  const empty = (qs) => !qs || (items(qs) === 0 && !new URLSearchParams(qs).get("gear_class"));

  // The builds kept in this browser: [{id, qs, t}], shared by every tab through localStorage.
  // ⭐ WHICH build a page is, is in its URL: `gear_id`, carried by every link like the build itself
  // (AOC-051 verify round 2, F4). So a page only ever writes the build its URL names, and nothing is
  // guessed: Back, reload, a history jump and another tab each change that build alone. Guessing — by
  // a tab's memory, by "the first one", by the kind of navigation — let one tab write over another's
  // build in rounds 1 and 2. Every write re-reads the list first (F1). Every access is guarded: with
  // storage blocked or full, the builder still works on the one build its URL holds.
  const CUR = "aoc-gear-current";
  const newId = () => (Date.now().toString(36) + Math.random().toString(36).slice(2, 8)).slice(0, 24);
  let held = []; // the list as this tab last read it: all there is when storage is blocked
  function list() {
    try {
      const s = JSON.parse(localStorage.getItem(KEY));
      if (s && Array.isArray(s.builds) && s.builds.every((b) => b && typeof b.id === "string" && typeof b.qs === "string")) held = s.builds;
    } catch (e) {}
    return held;
  }
  // A build now holds qs: the one with this id, or a new one if none has it (a link from elsewhere, or a
  // build pruned meanwhile). More than MAX only when a link arrived on a full list: empty builds go,
  // never one with something in it, nor the one just written.
  function keep(id, qs) {
    const l = list().map((b) => Object.assign({}, b));
    const b = l.find((x) => x.id === id);
    if (b) {
      b.qs = qs;
      b.t = Date.now();
    } else l.push({ id: id, qs: qs, t: Date.now() });
    for (let i = l.length - 1; l.length > MAX && i >= 0; i--) {
      if (l[i].id !== id && empty(l[i].qs)) l.splice(i, 1);
    }
    held = l;
    try { localStorage.setItem(KEY, JSON.stringify({ builds: l })); } catch (e) {}
  }
  // The build this tab last showed, for a page whose URL has none (a fresh visit): only ever READ, to
  // bring it back when the reader opens the builder, never written through.
  let tab = null;
  try { tab = sessionStorage.getItem(CUR); } catch (e) {}
  function setTab(id) {
    tab = id;
    try { sessionStorage.setItem(CUR, id); } catch (e) {}
  }
  // The build this page is on: the URL's; with none, the tab's last, else the one changed most recently.
  function mine() {
    const l = list(), p = pageBuild();
    if (p && p.id) return l.find((x) => x.id === p.id) || { id: p.id, qs: p.qs };
    return l.find((x) => x.id === tab) || l.slice().sort((x, y) => (y.t || 0) - (x.t || 0))[0] || null;
  }

  // A request for this state with another build (or an action on it), through htmx like a link's: the
  // page's script copies its URL into the form as it goes, the server answers with the state's own URL
  // in HX-Push-Url, and the pane comes back out of band. From #results, so it syncs on the form
  // (hx-sync) but sends none of its fields — the URL is the whole state.
  function request(params) {
    const u = new URL(location.href);
    ["gear", "gear_class", "gear_id", "add", "p"].forEach((k) => { if (k in params || k === "add") u.searchParams.delete(k); });
    for (const k in params) [].concat(params[k]).forEach((v) => u.searchParams.append(k, v));
    window.htmx.ajax("GET", u.pathname + "?" + u.searchParams.toString(), { source: "#results", target: "#results", swap: "innerHTML" });
  }
  function buildParams(b) {
    const q = new URLSearchParams(empty(b.qs) ? "gear=" : b.qs);
    return { gear: q.getAll("gear").length ? q.getAll("gear") : [""], gear_class: q.getAll("gear_class"), gear_id: [b.id] };
  }

  // ⭐ Opening a page never writes a kept build; only a reader's action does (AOC-051 verify round 3, F7:
  // a bookmark of an older state erased every edit made since, because each load wrote its URL's
  // state). So `adopt` is told which it is:
  //   - an ANSWER to an action (a "+", a drop, a ×, the class) writes its build, by id;
  //   - a LOAD whose state equals the kept build only selects it; one from the tab's own history (Back,
  //     Forward, reload) shows the older state and writes nothing — the next action writes it, which
  //     is undo; any other load (a bookmark, a link, the address bar) with another state of a kept
  //     build is a build of its own, so it never overwrites one;
  //   - an id this browser does not keep is added: it overwrites nothing.
  // A build arriving with no id (a shared link; a page made without a script) is a kept build if one
  // holds exactly it, otherwise a build of its own. A new id goes into the address (a replace) and into
  // the page's links (the same state asked again, which the server answers with HX-Replace-Url).
  const norm = (qs) => (empty(qs) ? "" : qs);
  function fork(qs) {
    const same = empty(qs) ? null : list().find((b) => b.qs === qs);
    const id = same ? same.id : newId();
    if (!same) keep(id, qs);
    setTab(id);
    const u = new URL(location.href);
    u.searchParams.set("gear_id", id);
    try { history.replaceState(history.state, "", u.pathname + "?" + u.searchParams.toString()); } catch (e) {}
    request({ gear_id: [id] });
  }
  function adopt(onLoad) {
    const p = pageBuild();
    if (!p) return;
    if (!p.id) return fork(p.qs);
    const k = list().find((x) => x.id === p.id);
    if (!onLoad || !k) {
      keep(p.id, p.qs);
      setTab(p.id);
      return;
    }
    if (norm(k.qs) === norm(p.qs)) return setTab(p.id);
    const nav = (performance.getEntriesByType("navigation")[0] || {}).type;
    if (nav === "back_forward" || nav === "reload") return setTab(p.id);
    fork(p.qs);
  }
  adopt(true);

  // An add, named by the reader: a drop on a slot, or a row's "+". When the URL holds no build, this
  // tab's kept one goes with it, so a "+" on a fresh visit adds to that build, not to a new empty one.
  function add(value) {
    const params = { add: value };
    if (pageBuild() === null) {
      const m = mine();
      // the kept build, or a new one named now, so its answer already carries its id
      Object.assign(params, m ? buildParams(m) : { gear_id: [newId()] });
    }
    request(params);
  }
  function select(i) {
    const b = list()[i];
    if (!b) return;
    setTab(b.id);
    request(buildParams(b));
  }
  function addBuild() {
    const id = newId();
    keep(id, "");
    setTab(id);
    request(buildParams({ id: id, qs: "" }));
  }

  // The design's dots, number and arrows. Drawn after every answer, since the pane is the server's, and
  // when another tab changes the list.
  function render() {
    const b = body();
    if (!b) return;
    // Nothing kept yet is one empty build, as the design draws its first state ("#1", "+").
    const kept = list(), m = mine();
    const l = kept.length ? kept : [{ id: "", qs: "" }];
    const at = kept.length ? (m ? l.findIndex((x) => x.id === m.id) : -1) : 0;
    const num = b.querySelector("[data-gear-number]");
    if (num) num.textContent = at >= 0 ? " #" + (at + 1) : "";
    const dots = b.querySelector("[data-gear-dots]");
    if (dots) {
      dots.replaceChildren();
      const dot = (label, on, filled, title, fn) => {
        const d = document.createElement("button");
        d.type = "button";
        d.textContent = label;
        d.title = title;
        d.className = "flex h-5 min-w-5 flex-none items-center justify-center rounded-[4px] border px-[5px] font-mono text-[10.5px] font-medium hover:border-link max-lg:h-9 max-lg:min-w-9 " +
          (on ? "border-link bg-ink-selected text-paper" : filled ? "border-line-control bg-ink-field text-muted" : "border-line-control text-faint");
        if (on) d.setAttribute("aria-current", "true");
        d.addEventListener("click", fn);
        dots.append(d);
      };
      l.forEach((x, i) => dot("#" + (i + 1), i === at, items(x.qs) > 0, "Build #" + (i + 1) + " · " + items(x.qs) + " items", () => { if (i !== at) select(i); }));
      if (l.length < MAX) dot("+", false, false, "A new build", addBuild);
    }
    // With no build in the URL the server counts 0; the strip and the phone button show the kept one.
    if (pageBuild() === null && m && !empty(m.qs)) {
      const total = (b.textContent.match(/\d+\/(\d+) slots/) || [])[1];
      if (total) ["gear-strip-count", "gear-count"].forEach((id) => {
        const c = document.getElementById(id);
        if (c) c.textContent = " · " + items(m.qs) + "/" + total;
      });
    }
    const prev = b.querySelector("[data-gear-prev]"), next = b.querySelector("[data-gear-next]");
    const can = [at > 0, (at >= 0 && at + 1 < l.length) || l.length < MAX];
    [prev, next].forEach((el, i) => {
      if (!el) return;
      el.setAttribute("aria-disabled", String(!can[i]));
      el.classList.toggle("text-muted", can[i]);
      el.classList.toggle("text-[#3d3933]", !can[i]);
    });
  }
  render();
  window.addEventListener("storage", (e) => { if (e.key === KEY) render(); });

  // Every answer redraws the pane and its counts (out of band): what it shows is the build its URL
  // names. After the settle, once every part of the answer is in.
  document.addEventListener("htmx:afterSettle", (e) => {
    if (!e.detail.target || e.detail.target.id !== "results") return;
    adopt(false);
    render();
    // A refused add says why in the pane: open it (the sheet on a phone), or nobody sees the reason.
    if (body() && body().querySelector('[role="status"]')) {
      const id = window.matchMedia("(min-width: 64rem)").matches ? "gear-open" : "gear-sheet";
      const c = document.getElementById(id);
      if (c) c.checked = true;
    }
  });

  // Enter in the search box submits the form natively, and an empty class picker would put a bare
  // gear_class= in the address — it names no build, but it is noise main never had (verify round 1, F2).
  document.addEventListener("submit", (e) => {
    const c = e.target.querySelector && e.target.querySelector('select[name="gear_class"]');
    if (c && !c.value) {
      c.disabled = true;
      setTimeout(() => { c.disabled = false; }, 0);
    }
  }, true);

  document.addEventListener("click", (e) => {
    const t = e.target.closest ? e.target : e.target.parentElement;
    if (!t) return;
    // Opening the builder on a URL with no build brings back this tab's kept one.
    if (t.closest("[data-gear-open]") && pageBuild() === null) {
      const m = mine();
      if (m && !empty(m.qs)) request(buildParams(m));
    }
    // The filters' strip folds the builder; it opens the filters too, as the design's toggle does.
    if (t.closest(".filters-strip-gear")) {
      const c = document.getElementById("filters-collapsed");
      if (c) c.checked = false;
    }
    const plus = t.closest("a.gear-add");
    if (plus) {
      e.preventDefault();
      e.stopPropagation(); // htmx would send the link's own URL, which has no kept build in it
      const id = new URL(plus.href, location.href).searchParams.get("add");
      if (id) add(id);
      return;
    }
    const share = t.closest("[data-gear-copy]");
    if (share && navigator.clipboard) {
      e.preventDefault();
      navigator.clipboard.writeText(share.href).then(() => {
        share.textContent = "link copied";
        setTimeout(() => { share.textContent = "share armor link"; }, 1600);
      }, () => {});
    }
    const prev = t.closest("[data-gear-prev]"), next = t.closest("[data-gear-next]");
    if (prev || next) {
      const l = list(), m = mine(), i = m ? l.findIndex((x) => x.id === m.id) : -1;
      if (prev && i > 0) select(i - 1);
      if (next) {
        if (i >= 0 && i + 1 < l.length) select(i + 1);
        else if (l.length < MAX) addBuild();
      }
    }
  }, true);

  // Dragging a list row onto a slot (the design). The slots that list the row's slot say "drop to
  // equip", the others "does not fit" — read from the row's own data; the server still decides.
  let drag = null;
  const slots = () => document.querySelectorAll("#gear-body [data-gear-slot]");
  function unmark() { slots().forEach((s) => { delete s.dataset.drop; }); }
  document.addEventListener("dragstart", (e) => {
    const row = e.target.closest && e.target.closest("tr[data-gear-id]");
    if (!row) return;
    drag = { id: row.dataset.gearId, fits: new Set((row.dataset.gearSlots || "").split(" ").filter(Boolean)) };
    try {
      e.dataTransfer.effectAllowed = "copy";
      e.dataTransfer.setData("text/plain", drag.id);
    } catch (err) {}
    slots().forEach((s) => { s.dataset.drop = drag.fits.has(s.dataset.gearSlot) ? "ok" : "no"; });
  });
  document.addEventListener("dragend", () => { drag = null; unmark(); });
  const target = (e) => e.target.closest && e.target.closest("#gear-body [data-gear-slot], .gear-strip");
  document.addEventListener("dragover", (e) => {
    const s = target(e);
    if (!s || !drag) return;
    const ok = s.classList.contains("gear-strip") || drag.fits.has(s.dataset.gearSlot);
    if (!ok) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "copy";
    slots().forEach((x) => { if (x.dataset.drop === "over" && x !== s) x.dataset.drop = "ok"; });
    if (s.dataset.gearSlot) s.dataset.drop = "over";
  });
  document.addEventListener("dragleave", (e) => {
    const s = target(e);
    if (s && s.dataset.drop === "over" && !s.contains(e.relatedTarget)) s.dataset.drop = "ok";
  });
  document.addEventListener("drop", (e) => {
    const s = target(e);
    if (!s || !drag) return;
    e.preventDefault();
    const id = drag.id, slot = s.dataset.gearSlot;
    drag = null;
    unmark();
    add(slot ? slot + ":" + id : id); // onto the folded strip: wherever the rules put it
  });
  // Rows are drawn by the server and redrawn by every answer; they become draggable here, where
  // dragging means something.
  const draggable = () => document.querySelectorAll("tr[data-gear-id]").forEach((r) => { r.draggable = true; });
  draggable();
  document.addEventListener("htmx:afterSwap", draggable);
}
