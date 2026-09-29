package items

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/pierrehrt/aoc-api/internal/httpx"
)

// Handler is the JSON surface over Service. It holds NO logic of its own: everything it does is
// parse a request, call the service, and write the result. That is deliberate — the HTML armory
// page calls the same Service, and a rule implemented in a handler is implemented once out of two
// (CLAUDE.md rule 5b).
type Handler struct {
	svc *Service
	tax TaxonomyLister
}

func NewHandler(svc *Service, tax TaxonomyLister) *Handler { return &Handler{svc: svc, tax: tax} }

// Routes returns the /items sub-router, mounted by httpx as v1.Mount("/items", h.Routes()).
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.list)
	r.Get("/{slug}", h.get)
	return r
}

// TaxonomyRoutes is mounted separately at /v1/taxonomies: the filter vocabularies are not items,
// and putting them under /items would make the URL lie about what it returns.
func (h *Handler) TaxonomyRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/", h.taxonomies)
	return r
}

// Cache-Control is not set here. httpx.Cache gives every /v1 GET the one /v1 window (AOC-026,
// docs/architecture.md § Caching), and nothing but internal/httpx/cache.go may name the header.
// This package used to set its own — five minutes for items, an hour for taxonomies — and those
// gave way to the single table when the 0.1.0 release joined AOC-012 to AOC-026 (AOC-015).

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilters(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	res, err := h.svc.List(r.Context(), f)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.Respond(w, r, http.StatusOK, res)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	item, err := h.svc.Get(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.Respond(w, r, http.StatusOK, item)
}

func (h *Handler) taxonomies(w http.ResponseWriter, r *http.Request) {
	tx, err := h.tax.Taxonomies(r.Context())
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.Respond(w, r, http.StatusOK, tx)
}

// parseFilters validates the query string.
//
// ⚠️ An unknown VALUE is not rejected here — only a malformed one. Whether "legendaryy" is a real
// rarity is a database question, and asking the database is what the filter already does: it
// returns an empty page, which is the honest answer and a 200 with an empty array by criterion.
// What IS rejected is a parameter that cannot be read at all — limit=abc, pvp=maybe — because
// silently ignoring those returns a page the caller did not ask for and cannot tell is wrong.
// ParseFilters reads the list query string. ⭐ ONE parser for both surfaces (CLAUDE.md 5b): the
// HTML armory page (AOC-047) calls this, so a parameter cannot mean one thing in JSON and another
// on the page. Sort keys and booleans are validated here; a bad value is a 400 on both.
func ParseFilters(r *http.Request) (Filters, error) { return parseFilters(r) }

func parseFilters(r *http.Request) (Filters, error) {
	q := r.URL.Query()
	f := Filters{
		Rarity:        strings.TrimSpace(q.Get("rarity")),
		ItemType:      strings.TrimSpace(q.Get("item_type")),
		EquipLocation: strings.TrimSpace(q.Get("equip_location")),
		ArmourWeight:  strings.TrimSpace(q.Get("armour_weight")),
		Class:         strings.TrimSpace(q.Get("class")),
		Region:        strings.TrimSpace(q.Get("region")),
		Tier:          strings.TrimSpace(q.Get("tier")),
		Query:         strings.TrimSpace(q.Get("q")),
		Sort:          strings.TrimSpace(q.Get("sort")),
	}
	if !validSort(f.Sort) {
		return Filters{}, fmt.Errorf("%w: sort must be one of %s, got %q", httpx.ErrInvalid, strings.Join(Sorts, ", "), f.Sort)
	}

	// `place` may repeat: ?place=a&place=b is "these two dungeons", which is exactly the case
	// where a shared item must appear under each.
	for _, p := range q["place"] {
		for _, one := range strings.Split(p, ",") {
			if one = strings.TrimSpace(one); one != "" {
				f.Places = append(f.Places, one)
			}
		}
	}

	var err error
	if f.PvP, err = optionalBool(q, "pvp"); err != nil {
		return Filters{}, err
	}
	if f.Unchained, err = optionalBool(q, "unchained"); err != nil {
		return Filters{}, err
	}
	if f.Limit, err = optionalInt(q, "limit"); err != nil {
		return Filters{}, err
	}
	if f.Offset, err = optionalInt(q, "offset"); err != nil {
		return Filters{}, err
	}
	// The WHOLE bound, not half of it. Only the negative half was written, and the query's OFFSET
	// is an int32: offset=4294967296 wrapped to 0 and returned THE FIRST PAGE with 4294967296
	// echoed back in the envelope — a 200 whose rows contradict what it says about itself, so a
	// client paging on offset silently restarts and can loop. offset=2147483648 wrapped negative
	// and became a 500, which is a server error for a client-input problem.
	//
	// `limit` needs no equivalent because it is clamped to [1, 200] before its own cast.
	if f.Offset < 0 || f.Offset > math.MaxInt32 {
		return Filters{}, fmt.Errorf("%w: offset must be between 0 and %d", httpx.ErrInvalid, math.MaxInt32)
	}
	return f, nil
}

func optionalBool(q map[string][]string, key string) (*bool, error) {
	vs, ok := q[key]
	if !ok || len(vs) == 0 || vs[0] == "" {
		return nil, nil
	}
	b, err := strconv.ParseBool(vs[0])
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be true or false, got %q", httpx.ErrInvalid, key, vs[0])
	}
	return &b, nil
}

func optionalInt(q map[string][]string, key string) (int, error) {
	vs, ok := q[key]
	if !ok || len(vs) == 0 || vs[0] == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(vs[0])
	if err != nil {
		return 0, fmt.Errorf("%w: %s must be a whole number, got %q", httpx.ErrInvalid, key, vs[0])
	}
	return n, nil
}
