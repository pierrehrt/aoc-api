package items

import (
	"net/http"
	"testing"
)

// AOC-062: `/v1/items` gains `item_type_name`, read from the JSON body the way a consumer reads it —
// the key's spelling is the contract, not the Go field (CLAUDE.md 5c). `item_type` keeps its slug.
func TestTheListRowCarriesItemTypeNameAsJSON(t *testing.T) {
	typ, name := "test-type", "Test Type"
	q := sharedItem()
	q.items[0].ItemType, q.items[0].ItemTypeName = &typ, &name
	rec, body := get(t, q, "/v1/items?limit=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	rows, _ := body["items"].([]any)
	if len(rows) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
	row := rows[0].(map[string]any)
	if row["item_type_name"] != "Test Type" || row["item_type"] != "test-type" {
		t.Errorf("item_type_name %v, item_type %v — want the name beside the unchanged slug", row["item_type_name"], row["item_type"])
	}

	// No type: neither key, rather than an empty name.
	q2 := sharedItem()
	_, body2 := get(t, q2, "/v1/items?limit=1")
	if r := body2["items"].([]any)[0].(map[string]any); r["item_type_name"] != nil {
		t.Errorf("a typeless row serialises item_type_name %v", r["item_type_name"])
	}
}
