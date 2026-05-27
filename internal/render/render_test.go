// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package render

import "testing"

func TestRender_Empty(t *testing.T) {
	body, err := Render(Output{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := "overrides: {}\n"
	if string(body) != want {
		t.Errorf("empty tenants should render as %q, got %q", want, body)
	}
}

func TestRender_CanonicalShape(t *testing.T) {
	body, err := Render(Output{
		"any-tenant-ns": {
			"per_stream_rate_limit":   "10MB",
			"ingestion_rate_mb":       32,
			"ingestion_burst_size_mb": 64,
		},
		"any-other-tenant": {
			"ingestion_rate_mb": 8,
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	want := `overrides:
  any-other-tenant:
    ingestion_rate_mb: 8
  any-tenant-ns:
    ingestion_burst_size_mb: 64
    ingestion_rate_mb: 32
    per_stream_rate_limit: 10MB
`
	if string(body) != want {
		t.Errorf("unexpected shape:\ngot:\n%s---\nwant:\n%s", body, want)
	}
}

func TestHash_Deterministic(t *testing.T) {
	body := []byte("overrides:\n  acme:\n    x: 1\n")
	h1 := Hash(body)
	h2 := Hash(body)
	if h1 != h2 {
		t.Errorf("Hash should be deterministic: %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex sha256, got %d chars", len(h1))
	}
}

func TestHash_DifferentInputDifferentOutput(t *testing.T) {
	if Hash([]byte("a")) == Hash([]byte("b")) {
		t.Errorf("different inputs should produce different hashes")
	}
}
