package pages

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/templates"
)

// The item page (AOC-048): /armory/{slug}.
//
// ⭐ ONE call, the /v1 one. The item comes from items.Service.Get — exactly what /v1/items/{slug}
// returns — so the page and the JSON cannot describe two different items. The handler owns only
// what a page owns: the 404, the <head> (title, description, canonical, og:image, JSON-LD) and the
// grouping of sources for display, which is templates.NewItemData.
//
// ⚠️ No per-reader state is read here. The page is cached at the edge for an hour, so the back
// link's "return to your search" is done in the browser (item.html), never from the Referer.
func (h *Handler) item(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	d, err := h.items.Get(r.Context(), slug)
	if err != nil {
		if errors.Is(err, httpx.ErrNotFound) || errors.Is(err, httpx.ErrInvalid) {
			// Cached briefly by the policy (a 404 is s-maxage=60), so a burst of dead links cannot
			// all reach the origin and a newly imported item appears within a minute.
			httpx.RejectHTML(w, r, http.StatusNotFound, "There is no item at this address")
			return
		}
		h.fail(w, r, err)
		return
	}

	path := "/armory/" + d.Slug
	data := templates.NewItemData(d)
	v := h.view(d.Name+" — AoC Codex", itemDescription(data), path)
	image := ""
	if d.TooltipImage != nil { // hydrate turns an empty string into nil, for both surfaces
		// The link preview IS the tooltip: a pasted URL in Discord shows the item as the game does.
		// It is portrait, so the small card — a large one is cropped to 2:1 and loses the name.
		image = *d.TooltipImage
		v.OGImage = image
		v.Card = "summary"
	}
	v.JSONLD = templates.NewThingLD(d.Name, v.Description, v.Canonical, image)
	h.render(w, r, "item", v, data)
}

// itemDescription is the search snippet, built only from the item's own values:
// "Epic Crossbow (Main Hand), item level 80, requires level 80 — an Age of Conan item: stats and
// where it comes from." The noun is the type when it says more than the slot (the page's TypeChip
// rule), else the slot; it names a section only when the item has something in it.
func itemDescription(data templates.ItemData) string {
	d := data.Item
	slots := make([]string, 0, len(d.Display.Slots))
	for _, s := range d.Display.Slots {
		slots = append(slots, s.Name)
	}
	var noun string
	var quals []string
	switch t := data.TypeChip(); {
	case t != "":
		noun = t
		if len(slots) > 0 {
			quals = append(quals, strings.Join(slots, "/"))
		}
	case len(slots) > 0:
		noun = strings.Join(slots, "/")
	}
	if d.Display.ArmourWeight != nil {
		quals = append(quals, d.Display.ArmourWeight.Name)
	}
	head := strings.TrimSpace(d.Display.Rarity.Name + " " + noun)
	if len(quals) > 0 {
		head += " (" + strings.Join(quals, ", ") + ")"
	}
	if d.ItemLevel != nil {
		head += fmt.Sprintf(", item level %d", *d.ItemLevel)
	}
	if d.RequiresLevel != nil {
		head += fmt.Sprintf(", requires level %d", *d.RequiresLevel)
	}
	var has []string
	if len(d.Stats) > 0 {
		has = append(has, "stats")
	}
	if len(d.Sources) > 0 {
		has = append(has, "where it comes from")
	}
	if len(d.SetPieces) > 1 {
		has = append(has, "its set")
	}
	tail := "an Age of Conan item"
	if len(has) > 0 {
		tail += ": " + strings.Join(has, ", ")
	}
	return head + " — " + tail + "."
}
