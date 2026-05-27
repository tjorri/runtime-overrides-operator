// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package webhook

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWithSourceCodeHeader_SetsHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})

	srv := httptest.NewServer(WithSourceCodeHeader(inner))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	got := resp.Header.Get(SourceCodeHeader)
	if got != SourceURL {
		t.Errorf("Source-Code header = %q; want %q", got, SourceURL)
	}
	if !strings.HasPrefix(SourceURL, "https://github.com/") {
		t.Errorf("source URL should be the GitHub repo, got %q", SourceURL)
	}
}

func TestWithSourceCodeHeader_SetBeforeInnerRuns(t *testing.T) {
	// The middleware must set the header BEFORE delegating to the inner
	// handler, so even handlers that never write a body (or that error
	// out) still emit the header. Use an in-memory ResponseRecorder so
	// the assertion is straightforward; the inner handler asserts the
	// header is already present when it runs.
	var headerSeenByInner string
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		headerSeenByInner = w.Header().Get(SourceCodeHeader)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test", strings.NewReader("{}"))
	WithSourceCodeHeader(inner).ServeHTTP(rec, req)

	if headerSeenByInner != SourceURL {
		t.Errorf("inner handler saw Source-Code = %q; want %q",
			headerSeenByInner, SourceURL)
	}
	if got := rec.Header().Get(SourceCodeHeader); got != SourceURL {
		t.Errorf("final response Source-Code = %q; want %q", got, SourceURL)
	}
}
