package xray

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestManualStepsBothLayouts(t *testing.T) {
	custom := decode(t, `{
		"customfield_1": {"value": "Manual"},
		"customfield_2": {"steps": [
			{"index": 2, "fields": {"Expected Result": "works", "Action": "do", "Note": "x", "Data": "d"},
			 "attachments": [{"id": 5, "fileName": "a.png", "fileURL": "https://j/x/5/a.png"}]},
			{"index": 1, "fields": {"Action": "first"}}]}}`)
	steps := ManualSteps(custom)
	if len(steps) != 2 || steps[0].Index != 2 {
		t.Fatalf("steps: %+v", steps)
	}
	if got := steps[0].Fields; !reflect.DeepEqual(got, [][2]string{{"Action", "do"}, {"Data", "d"}, {"Expected Result", "works"}, {"Note", "x"}}) {
		t.Errorf("field order: %v", got)
	}
	if len(steps[0].Files) != 1 || steps[0].Files[0].LocalName() != "5-a.png" {
		t.Errorf("files: %+v", steps[0].Files)
	}
	legacy := decode(t, `{"customfield_9": {"steps": [{"index": 1, "step": {"raw": "do", "rendered": "<p>do</p>"},
		"data": "", "result": {"raw": "ok"}}]}}`)
	if got := ManualSteps(legacy)[0].Fields; !reflect.DeepEqual(got, [][2]string{{"Action", "do"}, {"Expected Result", "ok"}}) {
		t.Errorf("legacy layout: %v", got)
	}
	if ManualSteps(decode(t, `{"customfield_3": {"steps": []}}`)) != nil {
		t.Error("empty steps should give nil")
	}
}

func TestRunRefs(t *testing.T) {
	custom := decode(t, `{"customfield_12": {"issueId": 1, "statuses": [
		{"group": "P-1", "statusResults": [{"environment": "Dev", "latest": 3, "latestId": 5001, "latestFinalId": 5001}]},
		{"group": "All", "statusResults": [{"latestId": 5002, "latestFinalId": 5001}]}]}}`)
	got := RunRefs(custom)
	if len(got) != 2 || got[0] != (RunRef{"5001", "P-1", "Dev"}) || got[1].ID != "5002" {
		t.Errorf("got %+v", got)
	}
}

func TestMarkdownFailedStepWithEvidence(t *testing.T) {
	steps := ManualSteps(decode(t, `{"cf": {"steps": [
		{"index": 1, "fields": {"Action": "open", "Expected Result": "opens"}},
		{"index": 2, "fields": {"Action": "send", "Expected Result": "sent"}}]}}`))
	run := decode(t, `{"id": 77, "status": "FAIL", "testExecKey": "P-9", "executedBy": "tester",
		"finishedOn": "2026-10-30T10:00:00", "steps": [
		{"index": 1, "status": "PASS"},
		{"index": 2, "status": "FAIL", "comment": {"raw": "nothing sent"}, "actualResult": {"raw": "timeout"},
		 "evidences": [{"id": 901, "fileName": "shot.png", "fileURL": "https://j/plugins/servlet/raven/attachment/901/shot.png"}],
		 "defects": [{"key": "P-10"}]}]}`)
	md := Markdown(steps, []map[string]any{run})
	for _, want := range []string{"## Test Details", "**Test run 77:** FAIL · execution P-9 · by tester",
		"### Step 1 — PASS", "### Step 2 — FAIL", "**Action:** send", "**Expected Result:** sent",
		"**Actual result:** timeout", "**Comment:** nothing sent", "**Evidence:** `shot.png`", "**Defects:** P-10"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if fs := Files(run); len(fs) != 1 || fs[0].URL != "https://j/plugins/servlet/raven/attachment/901/shot.png" {
		t.Errorf("files: %+v", fs)
	}
	if Markdown(nil, nil) != "" {
		t.Error("no steps, no runs: want empty")
	}
	if md := Markdown(nil, []map[string]any{run}); !strings.Contains(md, "### Step 2 — FAIL") {
		t.Errorf("run only:\n%s", md)
	}
}
