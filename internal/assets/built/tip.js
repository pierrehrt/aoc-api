// The tooltip beside the cursor (AOC-074): hovering an item's name, a list row's or an equipped slot's,
// shows that item's own tooltip image (items.tooltip_image, carried by the name as `data-tip`), placed
// as the validated design's prototype places it (`tipAt`). The second island (docs/architecture.md
// § Rendering), served as written like gear.js.
//
// ⭐ An enhancement only. Without it, without JavaScript and on a touch screen, the name is a link to
// the item page, which shows the tooltip in full: nothing is reachable only through this card.
//
// It decides nothing and invents nothing: the image is the item's own, never a stand-in (the design's
// three are placeholders chosen by rarity), and an item without one shows no card.

const PAD = 14; // the design's margin from the window's edges
const DX = 18, DY = 14; // the card's top-left, from the cursor

// Only where there is a pointer that hovers: a touch screen has no hover, and a tap opens the item.
const hovers = window.matchMedia("(hover: hover) and (pointer: fine)");

let card = null; // made once, on first use
let on = null; // the name being hovered
const at = { x: 0, y: 0 }; // the cursor
const images = new Map(); // url → the loaded image, or false when it failed to load

function make() {
  card = document.createElement("div");
  card.className = "tip-card";
  card.setAttribute("aria-hidden", "true"); // the item page carries the tooltip for every reader
  card.hidden = true;
  card.append(document.createElement("img"));
  card.firstChild.alt = "";
  document.body.append(card);
}

// The design's arithmetic: beside the cursor, moved to its left at the window's right edge, raised at
// its bottom, and scaled down to fit the window's height. The image's size is not in the data, so it
// is read from the image once loaded (internal/pages/item_test.go).
function place() {
  const im = on ? images.get(on.dataset.tip) : null;
  if (!im) {
    if (card) card.hidden = true; // still loading (it shows on load), or it failed (it never shows)
    return;
  }
  if (!card) make();
  const scale = Math.min(1, (window.innerHeight - PAD * 2) / im.naturalHeight);
  const w = Math.round(im.naturalWidth * scale), h = Math.round(im.naturalHeight * scale);
  let x = at.x + DX, y = at.y + DY;
  if (x + w > window.innerWidth - PAD) x = Math.max(PAD, at.x - w - DX);
  if (y + h > window.innerHeight - PAD) y = Math.max(PAD, window.innerHeight - h - PAD);
  if (card.firstChild.src !== im.src) card.firstChild.src = im.src;
  Object.assign(card.style, { left: x + "px", top: y + "px", width: w + "px", height: h + "px" });
  card.hidden = false;
}

function hide() {
  on = null;
  if (card) card.hidden = true;
}

// The card appears only once its image has loaded: never an empty or half-sized box. A failed image
// shows nothing.
function enter(el) {
  on = el;
  const url = el.dataset.tip;
  const known = images.get(url);
  if (known !== undefined) return place();
  const im = new Image();
  im.onload = () => {
    images.set(url, im);
    if (on === el) place();
  };
  im.onerror = () => images.set(url, false);
  im.src = url;
}

// Delegated, so names redrawn by htmx (a filter, a sort, a page, a builder action) need nothing.
document.addEventListener("mouseover", (e) => {
  if (!hovers.matches) return;
  const el = e.target.closest ? e.target.closest("[data-tip]") : null;
  at.x = e.clientX;
  at.y = e.clientY;
  if (el && el !== on) enter(el);
  else if (!el && on) hide();
});
document.addEventListener("mousemove", (e) => {
  if (!on) return;
  at.x = e.clientX;
  at.y = e.clientY;
  place();
});
document.addEventListener("mouseout", (e) => {
  if (on && (!e.relatedTarget || !on.contains(e.relatedTarget))) hide();
});
// A drag onto a slot, opening the item, a scroll under the cursor, an answer redrawing the names, or
// leaving the window: the card goes, as the design hides it on a drag and on opening.
["dragstart", "click", "htmx:beforeSwap"].forEach((t) => document.addEventListener(t, hide, true));
document.addEventListener("scroll", hide, true);
window.addEventListener("blur", hide);
