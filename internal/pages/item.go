package pages

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/httpx"
	"github.com/pierrehrt/aoc-api/internal/items"
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
	desc := itemDescription(d)
	v := h.view(d.Name+" — AoC Codex", desc, path)
	image := ""
	if d.TooltipImage != nil && *d.TooltipImage != "" {
		// The link preview IS the tooltip: a pasted URL in Discord shows the item as the game does.
		image = *d.TooltipImage
		v.OGImage = image
	}
	v.JSONLD = templates.NewThingLD(d.Name, v.Description, v.Canonical, image)
	h.render(w, r, "item", v, templates.NewItemData(d))
}

// itemDescription is the search snippet, built only from the item's own values:
// "Epic Necklace, item level 80, requires level 80 — an Age of Conan item: stats and sources."
// It names a section only when the item has something in it.
func itemDescription(d items.Detail) string {
	var what []string
	what = append(what, d.Display.Rarity.Name)
	switch {
	case len(d.Display.Slots) > 0:
		names := make([]string, 0, len(d.Display.Slots))
		for _, s := range d.Display.Slots {
			names = append(names, s.Name)
		}
		what = append(what, strings.Join(names, "/"))
	case d.Display.ItemType != nil:
		what = append(what, d.Display.ItemType.Name)
	}
	head := strings.Join(what, " ")
	if d.Display.ArmourWeight != nil {
		head += " (" + d.Display.ArmourWeight.Name + ")"
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
