package builds

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// Handler is the JSON surface over Service. It holds no logic of its own: the Armory page calls the
// same Service, and a rule written in a handler is written once out of two (CLAUDE.md rule 5b).
type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes returns the /builds sub-router, mounted as v1.Mount("/builds", h.Routes()).
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/compute", h.compute)
	return r
}

// compute answers /v1/builds/compute?gear=<slot>:<id>&…&gear_class=<slug> — the parameters the
// Armory page takes — with what the page's builder shows. An id no item has is a 400 here (the
// page shows it as an "unknown item" row): a caller that sends a build wants to be told it is wrong.
// `add` is the page's action and is not read here.
func (h *Handler) compute(w http.ResponseWriter, r *http.Request) {
	b, err := Parse(r.URL.Query())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	res, err := h.svc.Compute(r.Context(), b)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	for _, s := range res.Slots {
		if s.Status == StatusUnknown {
			httpx.Fail(w, r, fmt.Errorf("%w: %s: there is no item %d", httpx.ErrInvalid, ParamGear, s.ItemID))
			return
		}
	}
	httpx.Respond(w, r, http.StatusOK, res)
}
