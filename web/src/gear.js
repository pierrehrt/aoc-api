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
//     its tabs: the URL holds the tab's current one, the rest wait in localStorage. A shared link opens
//     as a build of its own and never overwrites one; Back and reload edit the tab's own.

const KEY = "aoc-gear-builds";
const MAX = 5; // the design's builds (maxBuilds)

const pane = document.querySelector('[data-island="gear"]');
if (pane) {
  if (window.htmx) mount();
  else document.addEventListener("DOMContentLoaded", mount);
}

function mount() {
  const body = () => document.getElementById("gear-body");

  // The build the server drew: the pane's hidden `gear` inputs and the class picker. null when the
  // URL carries no build (an empty one is `gear=`, which is a build).
  function pageBuild() {
    const b = body();
    if (!b) return null;
    const q = new URLSearchParams();
    const gear = b.querySelectorAll('input[type=hidden][name="gear"]');
    gear.forEach((i) => q.append("gear", i.value));
    const c = b.querySelector('select[name="gear_class"]');
    if (c && c.value) q.set("gear_class", c.value);
    return gear.length || (c && c.value) ? q.toString() : null;
  }
  const items = (qs) => new URLSearchParams(qs).getAll("gear").filter((v) => v !== "").length;
  const empty = (qs) => !qs || (items(qs) === 0 && !new URLSearchParams(qs).get("gear_class"));

  // The builds kept in this browser: [{id, qs}], shared by every tab through localStorage.
  // ⭐ Every change re-reads the list and touches only its own build, by id. A tab never writes back a
  // copy it holds: with two tabs open, the last one to act erased what the other had kept (AOC-051
  // verify round 1, F1). Which build THIS tab is on is the tab's own (sessionStorage). Every access is
  // guarded: with storage blocked or full, the builder still works on the one build its URL holds.
  const CUR = "aoc-gear-current";
  const newId = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 8);
  let held = []; // the list as this tab last read it: all there is when storage is blocked
  function list() {
    try {
      const s = JSON.parse(localStorage.getItem(KEY));
      if (s && Array.isArray(s.builds) && s.builds.length && s.builds.every((b) => b && typeof b.id === "string" && typeof b.qs === "string")) held = s.builds;
    } catch (e) {}
    if (!held.length) held = [{ id: newId(), qs: "" }];
    return held;
  }
  let cur = null;
  try { cur = sessionStorage.getItem(CUR); } catch (e) {}
  function setCur(id) {
    cur = id;
    try { sessionStorage.setItem(CUR, id); } catch (e) {}
  }
  // This tab's build in a list just read: its index (the first build, if another tab dropped it).
  function current(l) {
    let i = l.findIndex((b) => b.id === cur);
    if (i < 0) {
      i = 0;
      setCur(l[0].id);
    }
    return i;
  }
  // A change: read the list again, apply it, prune, write it. More than MAX only when a shared link
  // arrived on a full list: empty builds go, never one with something in it, nor this tab's.
  function change(fn) {
    const l = list().map((b) => ({ id: b.id, qs: b.qs }));
    fn(l);
    for (let i = l.length - 1; l.length > MAX && i >= 0; i--) {
      if (l[i].id !== cur && empty(l[i].qs)) l.splice(i, 1);
    }
    held = l;
    try { localStorage.setItem(KEY, JSON.stringify({ builds: l })); } catch (e) {}
  }
  // This tab's build now holds qs: kept by its id, or added back if another tab dropped it meanwhile.
  function keep(qs) {
    change((l) => {
      const b = l.find((x) => x.id === cur);
      if (b) b.qs = qs;
      else l.push({ id: cur, qs: qs });
    });
  }

  // On load: a build in the URL is one already kept (this tab is on it now), this tab's own build
  // edited (Back and reload only: it is the same tab), or a build of its own — in this tab's empty
  // build, or a new one — so it never overwrites one. Arriving from elsewhere on the site with a build
  // no kept one matches is a build of its own too: another tab may have changed the one this tab was on.
  {
    const l = list();
    const i = current(l);
    const arrived = pageBuild();
    if (arrived !== null) {
      const nav = (performance.getEntriesByType("navigation")[0] || {}).type;
      const same = l.find((b) => b.qs === arrived);
      if (same) setCur(same.id);
      else if (nav === "back_forward" || nav === "reload" || empty(l[i].qs)) keep(arrived);
      else {
        setCur(newId());
        keep(arrived);
      }
    }
  }

  // A request for this state with another build (or an action on it), through htmx like a link's: the
  // page's script copies its URL into the form as it goes, the server answers with the state's own URL
  // in HX-Push-Url, and the pane comes back out of band. From #results, so it syncs on the form
  // (hx-sync) but sends none of its fields — the URL is the whole state.
  function request(params) {
    const u = new URL(location.href);
    ["gear", "gear_class", "add", "p"].forEach((k) => { if (k in params || k === "add") u.searchParams.delete(k); });
    for (const k in params) [].concat(params[k]).forEach((v) => u.searchParams.append(k, v));
    window.htmx.ajax("GET", u.pathname + "?" + u.searchParams.toString(), { source: "#results", target: "#results", swap: "innerHTML" });
  }
  function buildParams(qs) {
    const q = new URLSearchParams(empty(qs) ? "gear=" : qs);
    return { gear: q.getAll("gear").length ? q.getAll("gear") : [""], gear_class: q.getAll("gear_class") };
  }
  const kept = () => { const l = list(); return l[current(l)].qs; };
  // An add, named by the reader: a drop on a slot, or a row's "+". When the URL holds no build, this
  // tab's kept one goes with it, so a "+" on a fresh visit adds to that build, not to a new empty one.
  function add(value) {
    const params = { add: value };
    if (pageBuild() === null && !empty(kept())) Object.assign(params, buildParams(kept()));
    request(params);
  }
  function select(i) {
    const l = list();
    setCur(l[i].id);
    request(buildParams(l[i].qs));
  }
  function addBuild() {
    setCur(newId());
    keep("");
    request(buildParams(""));
  }

  // The design's dots, number and arrows. Drawn after every answer, since the pane is the server's, and
  // when another tab changes the list.
  function render() {
    const b = body();
    if (!b) return;
    const l = list(), at = current(l);
    const num = b.querySelector("[data-gear-number]");
    if (num) num.textContent = " #" + (at + 1);
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
      l.forEach((x, i) => dot("#" + (i + 1), i === at, items(x.qs) > 0, "Build #" + (i + 1) + " · " + items(x.qs) + " items", () => select(i)));
      if (l.length < MAX) dot("+", false, false, "A new build", addBuild);
    }
    // With no build in the URL the server counts 0; the strip and the phone button show the kept one.
    if (pageBuild() === null && !empty(l[at].qs)) {
      const total = (b.textContent.match(/\d+\/(\d+) slots/) || [])[1];
      if (total) ["gear-strip-count", "gear-count"].forEach((id) => {
        const c = document.getElementById(id);
        if (c) c.textContent = " · " + items(l[at].qs) + "/" + total;
      });
    }
    const prev = b.querySelector("[data-gear-prev]"), next = b.querySelector("[data-gear-next]");
    const can = [at > 0, at + 1 < l.length || l.length < MAX];
    [prev, next].forEach((el, i) => {
      if (!el) return;
      el.setAttribute("aria-disabled", String(!can[i]));
      el.classList.toggle("text-muted", can[i]);
      el.classList.toggle("text-[#3d3933]", !can[i]);
    });
  }
  render();
  window.addEventListener("storage", (e) => { if (e.key === KEY) render(); });

  // Every answer redraws the pane and its counts (out of band): the build it holds is this tab's build
  // now. After the settle, once every part of the answer is in.
  document.addEventListener("htmx:afterSettle", (e) => {
    if (!e.detail.target || e.detail.target.id !== "results") return;
    const b = pageBuild();
    if (b !== null) keep(b);
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
    if (t.closest("[data-gear-open]") && pageBuild() === null && !empty(kept())) {
      request(buildParams(kept()));
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
      const l = list(), i = current(l);
      if (prev && i > 0) select(i - 1);
      if (next) {
        if (i + 1 < l.length) select(i + 1);
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
