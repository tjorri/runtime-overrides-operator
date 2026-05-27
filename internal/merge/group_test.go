// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package merge

import (
	"reflect"
	"testing"
)

func TestGroupByTenant(t *testing.T) {
	crs := []Override{
		{Namespace: "a", Name: "x", TenantId: "t1"},
		{Namespace: "b", Name: "y", TenantId: "t2"},
		{Namespace: "c", Name: "z", TenantId: "t1"},
	}
	got := GroupByTenant(crs)
	if len(got) != 2 {
		t.Fatalf("expected 2 tenant buckets, got %d", len(got))
	}
	if len(got["t1"]) != 2 {
		t.Errorf("t1 should have 2 CRs, got %d", len(got["t1"]))
	}
	if len(got["t2"]) != 1 {
		t.Errorf("t2 should have 1 CR, got %d", len(got["t2"]))
	}
}

func TestSortGroup_WeightAscThenLexNs(t *testing.T) {
	g := []Override{
		{Namespace: "z", Name: "a", Weight: 0},
		{Namespace: "a", Name: "z", Weight: 100},
		{Namespace: "a", Name: "a", Weight: 0},
		{Namespace: "a", Name: "b", Weight: 0},
	}
	SortGroup(g)

	want := []struct {
		ns, name string
		w        int32
	}{
		{"a", "a", 0},
		{"a", "b", 0},
		{"z", "a", 0},
		{"a", "z", 100},
	}
	for i, w := range want {
		if g[i].Namespace != w.ns || g[i].Name != w.name || g[i].Weight != w.w {
			t.Errorf("position %d: want (%s/%s w=%d), got (%s/%s w=%d)",
				i, w.ns, w.name, w.w, g[i].Namespace, g[i].Name, g[i].Weight)
		}
	}
}

func TestSortGroup_NegativeWeightsFirst(t *testing.T) {
	g := []Override{
		{Namespace: "a", Name: "a", Weight: 0},
		{Namespace: "a", Name: "b", Weight: -100},
		{Namespace: "a", Name: "c", Weight: 50},
	}
	SortGroup(g)
	wantOrder := []int32{-100, 0, 50}
	for i, w := range wantOrder {
		if g[i].Weight != w {
			t.Errorf("position %d: want weight %d, got %d", i, w, g[i].Weight)
		}
	}
}

func TestSortGroup_Stable(t *testing.T) {
	// Two CRs with identical (weight, ns, name) — should preserve input order.
	g := []Override{
		{Namespace: "a", Name: "a", Weight: 0, TenantId: "first-input"},
		{Namespace: "a", Name: "a", Weight: 0, TenantId: "second-input"},
	}
	SortGroup(g)
	if g[0].TenantId != "first-input" {
		t.Errorf("sort not stable, got %v", g)
	}
}

func TestSortGroup_Empty(t *testing.T) {
	var g []Override
	SortGroup(g)
	if !reflect.DeepEqual(g, []Override(nil)) {
		t.Errorf("sort of nil slice should be nil, got %v", g)
	}
}
