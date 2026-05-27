// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteBanner_ContainsSourceAndLicense(t *testing.T) {
	var buf bytes.Buffer
	writeBanner(&buf, "v0.1.0-test")
	s := buf.String()

	if !strings.Contains(s, sourceURL) {
		t.Errorf("banner missing source URL: %q", s)
	}
	if !strings.Contains(s, licenseID) {
		t.Errorf("banner missing license ID: %q", s)
	}
	if !strings.Contains(s, "v0.1.0-test") {
		t.Errorf("banner missing supplied version: %q", s)
	}
}

func TestModuleVersion_UnknownForMissingModule(t *testing.T) {
	got := moduleVersion("github.com/this/does/not/exist")
	if got != "unknown" {
		t.Errorf("expected 'unknown' for missing module, got %q", got)
	}
}
