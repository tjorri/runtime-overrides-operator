// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package validate

import (
	"strings"
	"sync"
	"testing"
)

// TestMain ensures InitDefaults runs once before any subtests fire. This
// mirrors the cmd/main.go startup contract — see init.go.
func TestMain(m *testing.M) {
	InitDefaults()
	m.Run()
}

func TestLoki_ValidPasses(t *testing.T) {
	v := New(TargetLoki)
	if v == nil {
		t.Fatal("nil validator")
	}

	for name, raw := range map[string]map[string]any{
		"trivial_rate_limit": {
			"ingestion_rate_mb": 32,
		},
		"multiple_known_fields": {
			"ingestion_rate_mb":       32,
			"ingestion_burst_size_mb": 64,
			"max_streams_per_user":    10000,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := v.Validate(raw); err != nil {
				t.Errorf("expected valid override to pass, got: %v", err)
			}
		})
	}
}

func TestLoki_RejectsUnknownField(t *testing.T) {
	v := New(TargetLoki)
	err := v.Validate(map[string]any{"definitely_not_a_real_field": 42})
	if err == nil {
		t.Fatal("expected unknown field to be rejected under strict mode")
	}
	if !strings.Contains(err.Error(), "loki:") {
		t.Errorf("expected error to be tagged 'loki:', got: %v", err)
	}
}

func TestLoki_RejectsTypeMismatch(t *testing.T) {
	v := New(TargetLoki)
	// ingestion_rate_mb is a numeric field; a string is a type error.
	err := v.Validate(map[string]any{"ingestion_rate_mb": "eight"})
	if err == nil {
		t.Fatal("expected type mismatch to be rejected")
	}
}

func TestMimir_ValidPasses(t *testing.T) {
	v := New(TargetMimir)
	if v == nil {
		t.Fatal("nil validator")
	}

	for name, raw := range map[string]map[string]any{
		"trivial_rate_limit": {
			"ingestion_rate": 50000,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := v.Validate(raw); err != nil {
				t.Errorf("expected valid override to pass, got: %v", err)
			}
		})
	}
}

func TestMimir_RejectsUnknownField(t *testing.T) {
	v := New(TargetMimir)
	err := v.Validate(map[string]any{"definitely_not_a_real_field": 42})
	if err == nil {
		t.Fatal("expected unknown field to be rejected under strict mode")
	}
	if !strings.Contains(err.Error(), "mimir:") {
		t.Errorf("expected error to be tagged 'mimir:', got: %v", err)
	}
}

func TestMimir_RejectsTypeMismatch(t *testing.T) {
	v := New(TargetMimir)
	err := v.Validate(map[string]any{"ingestion_rate": "fast"})
	if err == nil {
		t.Fatal("expected type mismatch to be rejected")
	}
}

func TestNew_UnknownTargetReturnsNil(t *testing.T) {
	if v := New(Target("nonsense")); v != nil {
		t.Errorf("expected nil for unknown target, got %T", v)
	}
}

// TestConcurrentValidate confirms that after a single InitDefaults() the
// validators are safe under concurrent use. This is the operational
// contract the controller depends on.
func TestConcurrentValidate(t *testing.T) {
	loki := New(TargetLoki)
	mimir := New(TargetMimir)

	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := loki.Validate(map[string]any{"ingestion_rate_mb": 32}); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			if err := mimir.Validate(map[string]any{"ingestion_rate": 50000}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent validate failed: %v", err)
	}
}

// TestInitDefaults_Idempotent confirms calling InitDefaults a second time
// is a no-op (sync.Once guard). This is belt-and-suspenders — the actual
// contract is "once at startup."
func TestInitDefaults_Idempotent(t *testing.T) {
	// Already called once via TestMain. Call again; should not panic.
	InitDefaults()
	InitDefaults()
}
