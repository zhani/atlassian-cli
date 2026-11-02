package jira

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParentRoundTrip(t *testing.T) {
	var is Issue
	if err := json.Unmarshal([]byte(`{"key":"P-2","fields":{"parent":{"key":"P-1"}}}`), &is); err != nil {
		t.Fatal(err)
	}
	if is.Fields.Parent == nil || is.Fields.Parent.Key != "P-1" {
		t.Fatalf("parent not decoded: %+v", is.Fields.Parent)
	}
	out, err := json.Marshal(is)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"parent":{`) {
		t.Errorf("parent not encoded as \"parent\": %s", out)
	}
}
