// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package merge

import (
	"reflect"
	"testing"
)

func TestDeepMerge_Empty(t *testing.T) {
	got := DeepMerge(nil, map[string]any{})
	if len(got) != 0 {
		t.Errorf("want empty, got %v", got)
	}
	if got == nil {
		t.Errorf("DeepMerge must return non-nil even when src is empty")
	}
}

func TestDeepMerge_UnionDifferentKeys(t *testing.T) {
	a := map[string]any{"x": 1}
	b := map[string]any{"y": 2}
	got := DeepMerge(a, b)
	want := map[string]any{"x": 1, "y": 2}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestDeepMerge_SrcWinsOnLeafCollision(t *testing.T) {
	a := map[string]any{"x": 1}
	b := map[string]any{"x": 2}
	got := DeepMerge(a, b)
	if got["x"] != 2 {
		t.Errorf("want src to win, got %v", got)
	}
}

func TestDeepMerge_NestedRecurses(t *testing.T) {
	a := map[string]any{
		"top": map[string]any{
			"keep":     "a",
			"override": 1,
		},
	}
	b := map[string]any{
		"top": map[string]any{
			"override": 2,
			"new":      "b",
		},
	}
	got := DeepMerge(a, b)
	want := map[string]any{
		"top": map[string]any{
			"keep":     "a",
			"override": 2,
			"new":      "b",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want %v, got %v", want, got)
	}
}

func TestDeepMerge_TypeMismatchSrcWins(t *testing.T) {
	a := map[string]any{"x": map[string]any{"nested": 1}}
	b := map[string]any{"x": "string-now"}
	got := DeepMerge(a, b)
	if got["x"] != "string-now" {
		t.Errorf("scalar src must replace map dst, got %v", got)
	}
}

func TestDeepMerge_SrcDeepCopied(t *testing.T) {
	srcInner := map[string]any{"k": 1}
	src := map[string]any{"outer": srcInner}
	got := DeepMerge(nil, src)

	// Mutate src after merge; the merged result must not share storage.
	srcInner["k"] = 999
	if got["outer"].(map[string]any)["k"] == 999 {
		t.Errorf("DeepMerge aliased src interior — got mutation leak")
	}
}

func TestDeepMerge_SliceLeafReplaces(t *testing.T) {
	a := map[string]any{"list": []any{1, 2, 3}}
	b := map[string]any{"list": []any{9}}
	got := DeepMerge(a, b)
	want := []any{9}
	if !reflect.DeepEqual(got["list"], want) {
		t.Errorf("slices should be atomically replaced, got %v", got["list"])
	}
}

func TestSortedMarshal_DeterministicNested(t *testing.T) {
	// Build a deep nested map with keys that would normally be iterated in
	// random order; assert SortedMarshal produces byte-stable output.
	build := func() map[string]any {
		return map[string]any{
			"overrides": map[string]any{
				"any-tenant-ns": map[string]any{
					"per_stream_rate_limit":   "10MB",
					"ingestion_rate_mb":       32,
					"ingestion_burst_size_mb": 64,
				},
				"any-other-tenant": map[string]any{
					"ingestion_rate_mb": 8,
				},
			},
		}
	}

	first, err := SortedMarshal(build())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for i := range 100 {
		again, err := SortedMarshal(build())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if string(first) != string(again) {
			t.Fatalf("non-deterministic marshal at iteration %d:\nfirst:\n%s\n---\ngot:\n%s", i, first, again)
		}
	}

	// Assert the full output matches the canonical sorted shape.
	want := `overrides:
  any-other-tenant:
    ingestion_rate_mb: 8
  any-tenant-ns:
    ingestion_burst_size_mb: 64
    ingestion_rate_mb: 32
    per_stream_rate_limit: 10MB
`
	if string(first) != want {
		t.Errorf("unexpected marshaled shape:\ngot:\n%s---\nwant:\n%s", first, want)
	}
}

func TestTrackingMerge_NoConflictsSingleCR(t *testing.T) {
	sorted := []Override{
		{Namespace: "ns", Name: "a", Weight: 0, Overrides: map[string]any{"x": 1, "y": 2}},
	}
	merged, conflicts := TrackingMerge("tenant", sorted)
	if !reflect.DeepEqual(merged, map[string]any{"x": 1, "y": 2}) {
		t.Errorf("merge result: got %v", merged)
	}
	if len(conflicts) != 0 {
		t.Errorf("single CR should produce no conflicts, got %v", conflicts)
	}
}

func TestTrackingMerge_DifferentWeightOverrideRecorded(t *testing.T) {
	sorted := []Override{
		{Namespace: "ns", Name: "base", Weight: 0, Overrides: map[string]any{"x": 1}},
		{Namespace: "ns", Name: "boost", Weight: 100, Overrides: map[string]any{"x": 2}},
	}
	merged, conflicts := TrackingMerge("tenant", sorted)
	if merged["x"] != 2 {
		t.Errorf("higher weight should win, got %v", merged)
	}
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict record, got %d: %v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.SameWeight {
		t.Errorf("different-weight override should not be flagged SameWeight")
	}
	if c.Winner.Name != "boost" || len(c.Losers) != 1 || c.Losers[0].Name != "base" {
		t.Errorf("conflict provenance wrong: %+v", c)
	}
}

func TestTrackingMerge_SameWeightFlaggedAsConflict(t *testing.T) {
	sorted := []Override{
		{Namespace: "a-ns", Name: "first", Weight: 50, Overrides: map[string]any{"x": 1}},
		{Namespace: "b-ns", Name: "second", Weight: 50, Overrides: map[string]any{"x": 2}},
	}
	_, conflicts := TrackingMerge("tenant", sorted)
	if len(conflicts) != 1 || !conflicts[0].SameWeight {
		t.Errorf("same-weight collision should be flagged: %+v", conflicts)
	}
}

func TestTrackingMerge_SameValueNoConflict(t *testing.T) {
	// Two CRs writing the same value to the same leaf should NOT conflict.
	sorted := []Override{
		{Namespace: "ns", Name: "a", Weight: 0, Overrides: map[string]any{"x": 7}},
		{Namespace: "ns", Name: "b", Weight: 100, Overrides: map[string]any{"x": 7}},
	}
	_, conflicts := TrackingMerge("tenant", sorted)
	if len(conflicts) != 0 {
		t.Errorf("identical leaf values should not conflict, got %v", conflicts)
	}
}

func TestTrackingMerge_NestedPathRecorded(t *testing.T) {
	sorted := []Override{
		{Namespace: "ns", Name: "a", Weight: 0, Overrides: map[string]any{
			"nested": map[string]any{"inner": 1},
		}},
		{Namespace: "ns", Name: "b", Weight: 100, Overrides: map[string]any{
			"nested": map[string]any{"inner": 2},
		}},
	}
	merged, conflicts := TrackingMerge("tenant", sorted)
	if merged["nested"].(map[string]any)["inner"] != 2 {
		t.Errorf("nested merge wrong: %v", merged)
	}
	if len(conflicts) != 1 || len(conflicts[0].Path) != 2 ||
		conflicts[0].Path[0] != "nested" || conflicts[0].Path[1] != "inner" {
		t.Errorf("expected nested path [nested,inner], got %+v", conflicts)
	}
}
