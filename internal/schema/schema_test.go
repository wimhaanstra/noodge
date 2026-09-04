package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmbeddedSchemaIsUsable(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(JSON(), &doc); err != nil {
		t.Fatalf("embedded schema is not valid JSON: %v", err)
	}

	id, _ := doc["$id"].(string)
	if !strings.HasSuffix(id, "/noodge.schema.json") {
		t.Errorf("$id: got %q", id)
	}

	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		t.Fatal("schema has no $defs")
	}
	for _, want := range []string{"Command", "Commands", "Param", "Step"} {
		if _, ok := defs[want]; !ok {
			t.Errorf("$defs is missing %q", want)
		}
	}
}

// The doc comments in internal/config are the only source of the field
// descriptions an editor shows on hover. A generator that silently drops them
// still produces a structurally valid schema, so assert they survived.
func TestSchemaCarriesFieldDescriptions(t *testing.T) {
	var doc struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Description string `json:"description"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(JSON(), &doc); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ def, field string }{
		{"Command", "description"},
		{"Command", "steps"},
		{"Command", "output"},
		{"Param", "name"},
		{"Param", "flag"},
	} {
		if got := doc.Defs[tc.def].Properties[tc.field].Description; got == "" {
			t.Errorf("%s.%s has no description in the schema", tc.def, tc.field)
		}
	}
}
