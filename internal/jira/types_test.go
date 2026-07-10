package jira

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestIssueFieldsUnmarshalCapturesCustomFields(t *testing.T) {
	t.Parallel()
	raw := `{
		"summary": "Do the thing",
		"status": {"name": "Open"},
		"labels": ["a", "b"],
		"customfield_10001": "sprint-42",
		"customfield_10002": {"value": "High", "id": "3"},
		"customfield_10003": ["x", "y"],
		"customfield_10004": null
	}`

	var f IssueFields
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if f.Summary != "Do the thing" {
		t.Errorf("Summary = %q, want %q", f.Summary, "Do the thing")
	}
	if f.Status == nil || f.Status.Name != "Open" {
		t.Errorf("Status not decoded: %+v", f.Status)
	}

	want := map[string]any{
		"customfield_10001": "sprint-42",
		"customfield_10002": map[string]any{"value": "High", "id": "3"},
		"customfield_10003": []any{"x", "y"},
		// customfield_10004 is null and must be skipped
	}
	if diff := cmp.Diff(want, f.Custom); diff != "" {
		t.Errorf("Custom diff (-want +got):\n%s", diff)
	}
}

func TestIssueFieldsMarshalFlattensCustomFields(t *testing.T) {
	t.Parallel()
	f := IssueFields{
		Summary: "Do the thing",
		Custom: map[string]any{
			"customfield_10001": "sprint-42",
			"customfield_10002": map[string]any{"value": "High"},
		},
	}

	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if got["summary"] != "Do the thing" {
		t.Errorf("summary = %v, want %q", got["summary"], "Do the thing")
	}
	if got["customfield_10001"] != "sprint-42" {
		t.Errorf("customfield_10001 = %v, want %q", got["customfield_10001"], "sprint-42")
	}
	if _, ok := got["customfield_10002"]; !ok {
		t.Errorf("customfield_10002 missing from marshaled output: %s", data)
	}
	// The unexported overflow map must not leak as a "Custom" key.
	if _, ok := got["Custom"]; ok {
		t.Errorf("Custom key leaked into output: %s", data)
	}
}

func TestIssueFieldsRoundTrip(t *testing.T) {
	t.Parallel()
	raw := `{"summary":"S","customfield_10001":"v1","customfield_10002":42}`

	var f IssueFields
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var f2 IssueFields
	if err := json.Unmarshal(data, &f2); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if diff := cmp.Diff(f.Custom, f2.Custom); diff != "" {
		t.Errorf("custom fields lost on round-trip (-first +second):\n%s", diff)
	}
}
