package queue

import (
	"testing"

	"github.com/rudast/alpha-hack-2026/internal/domain"
)

func TestParseResult(t *testing.T) {
	res, err := parseResult(map[string]any{
		"event_id": "e1", "application_id": "a1", "from": "SCORING", "to": "APPROVED",
		"decision": `{"score":82}`, "comment": "ok", "unknown_field": "ignored",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.EventID != "e1" || res.ApplicationID != "a1" || res.From != domain.StatusScoring || res.To != domain.StatusApproved ||
		string(res.Decision) != `{"score":82}` || res.Comment != "ok" {
		t.Fatalf("got %+v", res)
	}
}

func TestParseResult_OptionalFieldsMayBeMissing(t *testing.T) {
	res, err := parseResult(map[string]any{"application_id": "a1", "from": "SUBMITTED", "to": "DATA_CHECK"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != nil || res.Comment != "" || res.EventID != "" {
		t.Fatalf("optional fields must stay empty, got %+v", res)
	}
}

func TestParseResult_RequiredFields(t *testing.T) {
	for name, values := range map[string]map[string]any{
		"no application_id": {"from": "SUBMITTED", "to": "DATA_CHECK"},
		"no from":           {"application_id": "a1", "to": "DATA_CHECK"},
		"no to":             {"application_id": "a1", "from": "SUBMITTED"},
		"empty message":     {},
		"non-string values": {"application_id": 1, "from": 2, "to": 3},
	} {
		if _, err := parseResult(values); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
