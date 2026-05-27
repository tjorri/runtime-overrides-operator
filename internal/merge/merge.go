// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package merge

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// DeepMerge recursively merges src into dst, returning the (modified) dst.
// Semantics:
//   - For keys present in both dst and src, if both values are map[string]any
//     the merge recurses; otherwise the src value wins (leaf or scalar collisions
//     are replaced).
//   - For keys present only in src, src's value is deep-copied into dst.
//   - The returned map aliases dst when non-nil; if dst is nil a fresh map is
//     returned. Callers must not retain references to src's interior — DeepMerge
//     deep-copies all sub-values so dst can outlive src safely.
//
// This is the "last wins on leaf collisions" semantic that mirrors dskit's
// own multi-file runtime_config merge.
func DeepMerge(dst, src map[string]any) map[string]any {
	if dst == nil {
		dst = make(map[string]any, len(src))
	}
	for k, v := range src {
		existing, ok := dst[k]
		if !ok {
			dst[k] = deepCopy(v)
			continue
		}
		existingMap, existingIsMap := existing.(map[string]any)
		vMap, vIsMap := v.(map[string]any)
		if existingIsMap && vIsMap {
			dst[k] = DeepMerge(existingMap, vMap)
			continue
		}
		// Leaf collision (or type mismatch): src wins.
		dst[k] = deepCopy(v)
	}
	return dst
}

// deepCopy recursively copies a value that came from a parsed JSON/YAML
// document (so the only composite types we expect are map[string]any and
// []any). Scalars and unknown types are returned by value.
func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = deepCopy(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = deepCopy(vv)
		}
		return out
	default:
		return x
	}
}

// SortedMarshal renders v as YAML with deterministic key ordering throughout.
//
// gopkg.in/yaml.v3 sorts map keys alphabetically when marshaling Go map types,
// so this is essentially yaml.Marshal with an explicit indent and a guarantee
// (encoded in tests) that the output is byte-stable across Go map iteration
// orders. The output ends with a single trailing newline (yaml.v3 convention).
func SortedMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
