// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package merge

import (
	"encoding/json"
	"testing"
)

// FuzzSortedMarshalDeterminism generates random nested maps and asserts
// SortedMarshal produces byte-identical output across repeated calls
// regardless of Go's randomized map iteration order. Inputs are JSON
// documents (which deserialize to the same shape we work with internally).
func FuzzSortedMarshalDeterminism(f *testing.F) {
	// Seed corpus covers small + nested + empty + collision-prone inputs.
	for _, seed := range []string{
		`{}`,
		`{"x":1}`,
		`{"b":2,"a":1,"c":3}`,
		`{"top":{"y":2,"x":1,"z":[1,2,3]}}`,
		`{"overrides":{"any-other-tenant":{"ingestion_rate_mb":8},"any-tenant-ns":{"per_stream_rate_limit":"10MB","ingestion_rate_mb":32,"ingestion_burst_size_mb":64}}}`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		var v any
		if err := json.Unmarshal([]byte(input), &v); err != nil {
			t.Skip() // not valid JSON, not our problem
		}
		m, ok := v.(map[string]any)
		if !ok {
			t.Skip() // top-level must be an object
		}

		first, err := SortedMarshal(m)
		if err != nil {
			t.Skip() // marshal errors aren't determinism violations
		}
		for range 5 {
			again, err := SortedMarshal(m)
			if err != nil {
				t.Fatalf("marshal errored on retry: %v", err)
			}
			if string(first) != string(again) {
				t.Fatalf("non-deterministic marshal for input %q:\nfirst:\n%s\nagain:\n%s",
					input, first, again)
			}
		}
	})
}
