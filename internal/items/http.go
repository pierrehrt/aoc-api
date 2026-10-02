package items

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
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

// SourceRoutes is mounted at /v1/sources (AOC-050): the Armory's source panel, one tab at a time.
// Not under /items, for the same reason as the taxonomies: a tree of sources is not an item.
func (h *Handler) SourceRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/tree", h.tree)
	return r
}

// tree answers /v1/sources/tree?tab=<slug>&<any list filter>: the tab's branches, each counted under
// every other filter. A `source` on the request is not applied to the counts (Service.Tree).
func (h *Handler) tree(w http.ResponseWriter, r *http.Request) {
	f, err := parseFilters(r)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	t, err := h.svc.Tree(r.Context(), f)
	if err != nil {
		httpx.Fail(w, r, err)
		return
	}
	httpx.Respond(w, r, http.StatusOK, t)
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
		// The six facets are lists (AOC-064), read like `place`: repeated, comma-separated, or both.
		Rarities:       list(q, "rarity"),
		EquipLocations: list(q, "equip_location"),
		ArmourWeights:  list(q, "armour_weight"),
		Classes:        list(q, "class"),
		Currencies:     list(q, "currency"),
		Sets:           list(q, "set"),
		ItemType:       strings.TrimSpace(q.Get("item_type")),
		Region:         strings.TrimSpace(q.Get("region")),
		Tier:           strings.TrimSpace(q.Get("tier")),
		Query:          strings.TrimSpace(q.Get("q")),
		Sort:           strings.TrimSpace(q.Get("sort")),
	}
	if !validSort(f.Sort) {
		return Filters{}, fmt.Errorf("%w: sort must be one of %s, got %q", httpx.ErrInvalid, strings.Join(Sorts, ", "), f.Sort)
	}

	// `place` may repeat: ?place=a&place=b is "these two dungeons", which is exactly the case
	// where a shared item must appear under each.
	f.Places = list(q, "place")

	// The source panel (AOC-050): the tab, the node and the half of a location. Malformed is a 400;
	// a slug no row has matches nothing, like any other unknown value.
	f.Tab = strings.TrimSpace(q.Get("tab"))
	if f.Tab != "" && !slugRe.MatchString(f.Tab) {
		return Filters{}, fmt.Errorf("%w: tab must be a tab slug, got %q", httpx.ErrInvalid, f.Tab)
	}
	src, err := ParseSource(q.Get("source"))
	if err != nil {
		return Filters{}, err
	}
	if src.Group, err = parseGroup(q.Get("get")); err != nil {
		return Filters{}, err
	}
	f.Source = src

	if f.PvP, err = optionalBool(q, "pvp"); err != nil {
		return Filters{}, err
	}
	if f.Unchained, err = optionalBool(q, "unchained"); err != nil {
		return Filters{}, err
	}
	if f.Price, err = optionalBool(q, "price"); err != nil {
		return Filters{}, err
	}
	// The level ranges (AOC-049). Two ranges, because item level and required level differ on 234
	// items. A bound that is not a whole number from 0 up is malformed; min above max is too.
	for _, l := range []struct {
		key string
		dst **int32
	}{{"ilvl_min", &f.ILvlMin}, {"ilvl_max", &f.ILvlMax}, {"reqlvl_min", &f.ReqLvlMin}, {"reqlvl_max", &f.ReqLvlMax}} {
		if *l.dst, err = optionalLevel(q, l.key); err != nil {
			return Filters{}, err
		}
	}
	if err := f.validRanges(); err != nil {
		return Filters{}, err
	}
	if wf, err := optionalBool(q, "facets"); err != nil {
		return Filters{}, err
	} else if wf != nil {
		f.WithFacets = *wf
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

// Values is ParseFilters' inverse: the query string that parses back to f. ⭐ The page builds every
// link it prints from this — pager, sort, chips, the canonical — so a filter the parser accepts
// cannot fall out of a link (AOC-049: `/armory?region=x&p=2` used to lose `region` in its pager,
// because the page's URL builder only knew `q` and `sort`). TestFiltersRoundTripThroughValues sets
// every field and fails on one this forgets. Limit, Offset and WithFacets are not state a link
// carries; Sort is written as given (the page drops its own default).
func (f Filters) Values() url.Values {
	v := url.Values{}
	str := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	boolean := func(k string, b *bool) {
		if b != nil {
			v.Set(k, strconv.FormatBool(*b))
		}
	}
	level := func(k string, n *int32) {
		if n != nil {
			v.Set(k, strconv.Itoa(int(*n)))
		}
	}
	many := func(k string, xs []string) {
		for _, x := range xs {
			v.Add(k, x)
		}
	}
	str("q", f.Query)
	many("rarity", f.Rarities)
	str("item_type", f.ItemType)
	many("equip_location", f.EquipLocations)
	many("armour_weight", f.ArmourWeights)
	many("class", f.Classes)
	str("region", f.Region)
	str("tier", f.Tier)
	many("currency", f.Currencies)
	many("set", f.Sets)
	str("sort", f.Sort)
	many("place", f.Places)
	str("tab", f.Tab)
	str("source", f.Source.String())
	str("get", f.Source.Group)
	boolean("pvp", f.PvP)
	boolean("unchained", f.Unchained)
	boolean("price", f.Price)
	level("ilvl_min", f.ILvlMin)
	level("ilvl_max", f.ILvlMax)
	level("reqlvl_min", f.ReqLvlMin)
	level("reqlvl_max", f.ReqLvlMax)
	return v
}

// list reads a parameter that takes several values: repeated (?rarity=a&rarity=b), comma-separated
// (?rarity=a,b) or both, trimmed, empties dropped, each value once in first-seen order. nil when
// none — never an empty slice (listArg's trap).
func list(q map[string][]string, key string) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range q[key] {
		for _, one := range strings.Split(raw, ",") {
			if one = strings.TrimSpace(one); one != "" && !seen[one] {
				seen[one] = true
				out = append(out, one)
			}
		}
	}
	return out
}

// optionalLevel reads a level bound: absent or empty is no bound; anything but a whole number from
// 0 to the column's ceiling is a 400 — a bound that cannot be represented is refused, not wrapped.
func optionalLevel(q map[string][]string, key string) (*int32, error) {
	vs, ok := q[key]
	if !ok || len(vs) == 0 || strings.TrimSpace(vs[0]) == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(vs[0]), 10, 32) // bitSize 32: the column is an integer
	if err != nil || n < 0 {
		return nil, fmt.Errorf("%w: %s must be a whole number from 0 to %d, got %q", httpx.ErrInvalid, key, math.MaxInt32, vs[0])
	}
	l := int32(n)
	return &l, nil
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
