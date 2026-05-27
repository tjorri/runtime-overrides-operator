//go:build e2e
// +build e2e

// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/tjorri/runtime-overrides-operator/test/utils"
)

const (
	testTenantNs = "test-tenant-a"
	// propagationWait is the upper bound on end-to-end propagation:
	// operator writes the output ConfigMap → kubelet ConfigMap sync (up
	// to ~60s on Kubernetes default) → dskit runtime_config poll (every
	// 10s). On a quiet cluster this stacks to ~70-90s; under GitHub-
	// hosted-runner load we've seen it stretch closer to that ceiling.
	// 180s gives 2× headroom over the p99 we measured locally.
	propagationWait = 180 * time.Second
	pollInterval    = 2 * time.Second
)

// readMergedOverrides reads the operator's output ConfigMap directly
// via kubectl and returns the YAML body the operator wrote. This is the
// fast operator-side check — proves the operator did its job, but says
// nothing about whether Loki/Mimir reloaded the file.
func readMergedOverrides(namespace, cmName string) (string, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", "cm", cmName,
		"-o", "jsonpath={.data.runtime-tenants\\.yaml}")
	out, err := utils.Run(cmd)
	return string(out), err
}

// queryLokiOverride scrapes Loki's /metrics for the value of a specific
// limit override for a tenant, as observed by the overrides-exporter
// module (Loki must be running with `-target=...,overrides-exporter`
// and `runtime_config.file` configured). Returns "" if no override is
// loaded for that tenant/field combination — which is dskit's behaviour
// when the runtime file is empty for that tenant.
func queryLokiOverride(tenant, field string) (string, error) {
	cmd := exec.Command("kubectl", "-n", "loki", "exec", "deploy/loki", "--",
		"wget", "-qO-", "http://localhost:3100/metrics")
	out, err := utils.Run(cmd)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "loki_overrides{") {
			continue
		}
		if !strings.Contains(line, fmt.Sprintf(`limit_name="%s"`, field)) {
			continue
		}
		if !strings.Contains(line, fmt.Sprintf(`user="%s"`, tenant)) {
			continue
		}
		// Format is `metric{labels} value [timestamp]`; the value is the
		// last whitespace-separated field before any optional timestamp.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		return fields[len(fields)-1], nil
	}
	return "", nil
}

// queryMimirRuntimeConfig fetches Mimir's /runtime_config endpoint and
// returns the loaded runtime-config YAML. Unlike Loki, Mimir exposes the
// merged runtime config over HTTP natively. Mimir's container image is
// distroless (no shell/wget/curl) so we can't kubectl exec into it —
// instead we pop a one-shot port-forward via `kubectl port-forward` to
// a free local port and HTTP from the test process.
func queryMimirRuntimeConfig() (string, error) {
	port, err := freeLocalPort()
	if err != nil {
		return "", fmt.Errorf("allocate local port: %w", err)
	}
	pf := exec.Command("kubectl", "-n", "mimir", "port-forward",
		"deploy/mimir", fmt.Sprintf("%d:8080", port))
	if err := pf.Start(); err != nil {
		return "", fmt.Errorf("start port-forward: %w", err)
	}
	defer func() {
		_ = pf.Process.Kill()
		_ = pf.Wait()
	}()

	// Wait for the local port to start accepting connections (port-forward
	// prints "Forwarding from 127.0.0.1:<port> -> 8080" once it's ready).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, derr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if derr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/runtime_config", port))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// freeLocalPort asks the OS for an ephemeral port and immediately closes
// the listener — the port number is then ours to claim moments later for
// kubectl port-forward. Racy in principle, fine in practice for tests.
func freeLocalPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	return port, l.Close()
}

// lokiCM and mimirCM are the operator's output ConfigMap coordinates
// per the e2e Helm values.
var (
	lokiCM  = struct{ ns, name string }{"loki", "loki-runtime-tenants"}
	mimirCM = struct{ ns, name string }{"mimir", "mimir-runtime-tenants"}
)

// fixturePath returns the absolute path to a test/e2e/fixtures/*.yaml.
// utils.Run sets cmd.Dir to the repo root, so a `fixtures/foo.yaml`
// relative path would resolve there and miss; we need the full path.
func fixturePath(name string) string {
	// runtime.Caller could be used too, but the manifestsDir setup in
	// BeforeSuite already records the test dir. We resolve via that.
	return filepath.Join(filepath.Dir(manifestsDir), "fixtures", name)
}

// applyFixture applies one of the test/e2e/fixtures/*.yaml files.
func applyFixture(name string) (string, error) {
	cmd := exec.Command("kubectl", "apply", "-f", fixturePath(name))
	out, err := utils.Run(cmd)
	return string(out), err
}

// applyFixtureBypass forces server-side apply, then sets --validate=false
// so the API server's client-side OpenAPI checks are skipped (used for
// "what if the webhook is bypassed" scenarios).
func applyFixtureBypass(name string) (string, error) {
	cmd := exec.Command("kubectl", "apply", "-f", fixturePath(name), "--validate=false")
	out, err := utils.Run(cmd)
	return string(out), err
}

// crStatusJSON returns the .status of a CR as a parsed map[string]any.
func crStatusJSON(kind, namespace, name string) (map[string]any, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", kind, name, "-o", "jsonpath={.status}")
	out, err := utils.Run(cmd)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if jerr := json.Unmarshal([]byte(s), &m); jerr != nil {
		return nil, fmt.Errorf("parse status JSON %q: %w", s, jerr)
	}
	return m, nil
}

// findCondition extracts a condition by Type from a status map.
func findCondition(status map[string]any, condType string) map[string]any {
	conds, ok := status["conditions"].([]any)
	if !ok {
		return nil
	}
	for _, c := range conds {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if cm["type"] == condType {
			return cm
		}
	}
	return nil
}

var _ = Describe("runtime-overrides-operator", Ordered, func() {
	BeforeAll(func() {
		// Server-side apply for idempotency — re-runs against a kept-alive
		// cluster (E2E_SKIP_TEARDOWN from a prior pass) shouldn't fail just
		// because the namespace exists.
		nsYAML := fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", testTenantNs)
		applyCmd := exec.Command("kubectl", "apply", "-f", "-")
		applyCmd.Stdin = strings.NewReader(nsYAML)
		_, err := utils.Run(applyCmd)
		Expect(err).NotTo(HaveOccurred(), "create test tenant namespace")
	})

	AfterAll(func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", testTenantNs, "--ignore-not-found"))
		// Leave operator+target backends around for AfterSuite to clean up.
	})

	It("01: Loki actually loads the base CR (overrides-exporter metric)", func() {
		_, err := applyFixture("loki-base.yaml")
		Expect(err).NotTo(HaveOccurred())

		// Fast check first: the operator's output ConfigMap reflects the CR.
		// This proves the operator did its job and isolates a backend-side
		// failure from an operator-side failure if the next Eventually fails.
		Eventually(func() string {
			body, _ := readMergedOverrides(lokiCM.ns, lokiCM.name)
			return body
		}, 30*time.Second, pollInterval).Should(ContainSubstring("ingestion_rate_mb: 8"))

		// Backend check: Loki's overrides-exporter scrapes the loaded
		// runtime config every dskit poll (~10s). The metric appearing
		// proves Loki picked up the file from the directory mount.
		Eventually(func() string {
			v, _ := queryLokiOverride(testTenantNs, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(Equal("8"))
	})

	It("02: Applied condition message references CM generation + sha256 hash", func() {
		Eventually(func() string {
			st, err := crStatusJSON("lokitenantoverride.runtimeoverrides.io", testTenantNs, "base")
			if err != nil {
				return ""
			}
			applied := findCondition(st, "Applied")
			if applied == nil {
				return ""
			}
			return fmt.Sprintf("%v", applied["message"])
		}, 30*time.Second, pollInterval).Should(And(
			ContainSubstring("loki-runtime-tenants"),
			ContainSubstring("sha256:"),
		))
	})

	It("03: higher-weight CR overrides base on conflict (backend confirms)", func() {
		_, err := applyFixture("loki-boost.yaml")
		Expect(err).NotTo(HaveOccurred())

		// Loki's overrides-exporter eventually reports the higher value.
		Eventually(func() string {
			v, _ := queryLokiOverride(testTenantNs, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(Equal("32"))
	})

	It("04: base CR has ContributingPeers including boost", func() {
		Eventually(func() string {
			st, err := crStatusJSON("lokitenantoverride.runtimeoverrides.io", testTenantNs, "base")
			if err != nil {
				return ""
			}
			peers, _ := st["contributingPeers"].([]any)
			if len(peers) == 0 {
				return ""
			}
			b, _ := json.Marshal(peers)
			return string(b)
		}, 30*time.Second, pollInterval).Should(ContainSubstring(`"name":"boost"`))
	})

	It("05: Mimir actually loads the MimirTenantOverride (/runtime_config)", func() {
		_, err := applyFixture("mimir-default.yaml")
		Expect(err).NotTo(HaveOccurred())

		// Operator-side: CM has the value.
		Eventually(func() string {
			body, _ := readMergedOverrides(mimirCM.ns, mimirCM.name)
			return body
		}, 30*time.Second, pollInterval).Should(ContainSubstring("ingestion_rate: 50000"))

		// Backend-side: Mimir's /runtime_config endpoint (native) reports
		// the override after dskit's poll interval.
		Eventually(func() string {
			body, _ := queryMimirRuntimeConfig()
			return body
		}, propagationWait, pollInterval).Should(And(
			ContainSubstring(testTenantNs),
			ContainSubstring("50000"),
		))
	})

	It("06: deleting the high-weight CR reverts Loki's loaded override", func() {
		_, err := utils.Run(exec.Command("kubectl", "-n", testTenantNs, "delete",
			"lokitenantoverride.runtimeoverrides.io", "boost"))
		Expect(err).NotTo(HaveOccurred())

		// Loki's overrides-exporter eventually shows the baseline value
		// (8) again, NOT the boost value (32).
		Eventually(func() string {
			v, _ := queryLokiOverride(testTenantNs, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(Equal("8"))
	})

	It("07: webhook rejects a typed-mismatch CR with the upstream error", func() {
		out, err := applyFixture("loki-invalid-typed.yaml")
		Expect(err).To(HaveOccurred(), "expected webhook rejection")
		Expect(out).To(ContainSubstring("loki"))
	})

	It("08: webhook rejects an upstream-semantic violation (retention_stream < 24h)", func() {
		out, err := applyFixture("loki-invalid-semantic.yaml")
		Expect(err).To(HaveOccurred(), "expected webhook rejection")
		Expect(out).To(ContainSubstring("retention period"))
	})

	It("09: --validate=false does NOT bypass the admission webhook (server-side)", func() {
		// `kubectl --validate=false` only disables CLIENT-side OpenAPI checks.
		// Admission webhooks fire on the API server, so the operator's
		// webhook still rejects bad CRs even when the client validation is
		// disabled. To exercise the "webhook unavailable + Layer 3 catches
		// it" path, you'd scale the operator deployment to 0 first — that
		// case is covered by the envtest in M5 rather than e2e, because
		// envtest doesn't need the scale dance.
		out, err := applyFixtureBypass("loki-invalid-typed.yaml")
		Expect(err).To(HaveOccurred(),
			"webhook still fires server-side even with --validate=false; got out=%q", out)
	})

	It("10: bundled VAP rejects a cross-namespace tenantId from an unauthorized ns", func() {
		out, err := applyFixture("cross-namespace-override.yaml")
		Expect(err).To(HaveOccurred(), "expected VAP rejection")
		Expect(out).To(Or(
			ContainSubstring("tenant ownership policy"),
			ContainSubstring("denied"),
		))
	})

	It("11: third-party write to the output ConfigMap is reverted within seconds", func() {
		// Clobber the live CM via kubectl patch.
		patch := `{"data":{"runtime-tenants.yaml":"overrides:\n  test-tenant-a:\n    clobbered: true\n"}}`
		_, err := utils.Run(exec.Command("kubectl", "-n", "loki", "patch", "configmap",
			"loki-runtime-tenants", "--type=merge", "-p", patch))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			out, _ := utils.Run(exec.Command("kubectl", "-n", "loki", "get", "cm",
				"loki-runtime-tenants", "-o", "jsonpath={.data.runtime-tenants\\.yaml}"))
			return string(out)
		}, 30*time.Second, pollInterval).Should(And(
			Not(ContainSubstring("clobbered")),
			ContainSubstring("ingestion_rate_mb: 8"),
		))

		Eventually(func() string {
			out, _ := utils.Run(exec.Command("kubectl", "-n", "loki", "get", "events",
				"--field-selector=reason=ConfigMapDrifted", "-o", "jsonpath={.items[0].message}"))
			return string(out)
		}, 30*time.Second, pollInterval).Should(ContainSubstring("reverted"))
	})

	It("12: deleting the output ConfigMap directly causes the operator to recreate it", func() {
		_, err := utils.Run(exec.Command("kubectl", "-n", "loki", "delete", "configmap",
			"loki-runtime-tenants"))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			_, gerr := utils.Run(exec.Command("kubectl", "-n", "loki", "get", "cm",
				"loki-runtime-tenants"))
			return gerr
		}, 30*time.Second, pollInterval).Should(Succeed())
	})

	It("13: deleting the test namespace drops the tenant from the merged output and from Loki", func() {
		_, err := utils.Run(exec.Command("kubectl", "delete", "ns", testTenantNs, "--wait=true"))
		Expect(err).NotTo(HaveOccurred())

		// Operator-side: CM no longer mentions the tenant.
		Eventually(func() string {
			body, _ := readMergedOverrides(lokiCM.ns, lokiCM.name)
			return body
		}, 30*time.Second, pollInterval).ShouldNot(ContainSubstring(testTenantNs))

		// Backend-side: Loki's overrides-exporter no longer reports the
		// tenant in any limit metric.
		Eventually(func() string {
			v, _ := queryLokiOverride(testTenantNs, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(BeEmpty())
	})
})
