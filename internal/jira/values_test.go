package jira

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestSimplifyValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   any
		want any
	}{
		{nil, nil},
		{"", nil},
		{"text", "text"},
		{5.0, 5.0},
		{map[string]any{"value": "High", "id": "3"}, "High"},
		{map[string]any{"value": "EU", "child": map[string]any{"value": "Finland"}}, "EU / Finland"},
		{map[string]any{"displayName": "Ann", "accountId": "x"}, "Ann"},
		{map[string]any{"self": "http://x", "id": "1"}, nil},
		{[]any{map[string]any{"name": "v1"}, nil, "v2"}, []any{"v1", "v2"}},
		{[]any{map[string]any{"id": "1"}}, nil},
		{"com.atlassian.greenhopper.service.sprint.Sprint@1f[id=3,rapidViewId=1,state=ACTIVE,name=Sprint 7,startDate=x]", "Sprint 7"},
	}
	for _, tt := range tests {
		if diff := cmp.Diff(tt.want, SimplifyValue(tt.in)); diff != "" {
			t.Errorf("SimplifyValue(%v) diff (-want +got):\n%s", tt.in, diff)
		}
	}
}

func TestDisplayValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{3.0, "3"},
		{2.5, "2.5"},
		{[]any{"a", map[string]any{"value": "b"}}, "a, b"},
		{map[string]any{"id": "1"}, `{"id":"1"}`},
	}
	for _, tt := range tests {
		if got := DisplayValue(tt.in); got != tt.want {
			t.Errorf("DisplayValue(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
