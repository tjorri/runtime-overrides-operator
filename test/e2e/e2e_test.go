//go:build e2e
// +build e2e

// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

// The e2e suite is structured as independent Describe blocks rather than
// the previous Ordered narrative. Each propagation-bound scenario gets
// its own ephemeral tenant namespace (createTenantNS), applies its own
// CR(s) inline, verifies, and lets DeferCleanup tear the namespace
// down. With Ginkgo's --procs=N flag this lets the slow propagation-
// bound specs (which each wait up to ~10s for the operator → CM →
// kubelet → dskit chain) overlap rather than serialize.
//
// The only specs that cannot run in parallel are the ones that
// manipulate the operator's output ConfigMap directly (drift watch,
// CM-deletion recovery). Those are grouped under a Serial Describe so
// Ginkgo runs them in a single process while other processes drain
// parallel-safe specs.
package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/tjorri/runtime-overrides-operator/test/utils"
)

const (
	// propagationWait caps the operator → CM → kubelet → dskit chain.
	// With dskit's runtime_config.period tightened to 1s in the e2e
	// Loki/Mimir manifests, typical propagation is 5-10s; 180s gives
	// huge headroom for unusually slow CI runs.
	propagationWait = 180 * time.Second
	pollInterval    = 2 * time.Second
)

// lokiCM and mimirCM are the operator's output ConfigMap coordinates
// per the e2e Helm values.
var (
	lokiCM  = struct{ ns, name string }{"loki", "loki-runtime-tenants"}
	mimirCM = struct{ ns, name string }{"mimir", "mimir-runtime-tenants"}
)

// ---- per-spec tenant + CR helpers -----------------------------------------

// randSuffix returns a short hex suffix for unique resource names.
// Crypto/rand keeps two parallel specs colliding-namespace-free across
// procs without a shared seed.
func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// createTenantNS creates a uniquely-named namespace for one parallel
// spec, registers DeferCleanup to delete it after the spec, and returns
// the namespace name. Cleanup is non-blocking (`--wait=false`) — the
// operator's reconciler drops the tenant's stanza from the output CM
// as soon as the CRs become unwatchable, which is what later specs
// care about; the namespace's full cascade can complete in the
// background.
func createTenantNS(prefix string) string {
	ns := fmt.Sprintf("e2e-%s-%s", prefix, randSuffix())
	nsYAML := fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", ns)
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(nsYAML)
	_, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred(), "create tenant namespace %s", ns)
	DeferCleanup(func() {
		_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", ns,
			"--ignore-not-found", "--wait=false"))
	})
	return ns
}

// applyCR applies an inline-built CR YAML to the cluster. Returns the
// `kubectl apply` stdout so callers can inspect rejection messages from
// the webhook or VAP (for the deliberately-invalid CR scenarios).
func applyCR(yamlBody string) (string, error) {
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(yamlBody)
	out, err := utils.Run(cmd)
	return string(out), err
}

// applyCRBypass is applyCR with `--validate=false` so kubectl's
// client-side OpenAPI checks are skipped. Used to prove that the
// admission webhook still fires server-side.
func applyCRBypass(yamlBody string) (string, error) {
	cmd := exec.Command("kubectl", "apply", "-f", "-", "--validate=false")
	cmd.Stdin = strings.NewReader(yamlBody)
	out, err := utils.Run(cmd)
	return string(out), err
}

// lokiOverrideCR returns the YAML for a LokiTenantOverride with the
// given weight + ingestion_rate_mb. Each parallel spec calls this with
// its own ns and a self-explanatory name.
func lokiOverrideCR(ns, name string, weight int, ingestionRateMB int) string {
	return fmt.Sprintf(`apiVersion: runtimeoverrides.io/v1alpha1
kind: LokiTenantOverride
metadata:
  name: %s
  namespace: %s
spec:
  weight: %d
  overrides:
    ingestion_rate_mb: %d
`, name, ns, weight, ingestionRateMB)
}

// mimirOverrideCR returns the YAML for a MimirTenantOverride with the
// given ingestion_rate value.
func mimirOverrideCR(ns, name string, ingestionRate int) string {
	return fmt.Sprintf(`apiVersion: runtimeoverrides.io/v1alpha1
kind: MimirTenantOverride
metadata:
  name: %s
  namespace: %s
spec:
  overrides:
    ingestion_rate: %d
`, name, ns, ingestionRate)
}

// ---- backend probes -------------------------------------------------------

// readMergedOverrides reads the operator's output ConfigMap directly
// via kubectl. Fast operator-side check that proves the operator did
// its job, isolating a backend-side failure from an operator-side one.
func readMergedOverrides(namespace, cmName string) (string, error) {
	cmd := exec.Command("kubectl", "-n", namespace, "get", "cm", cmName,
		"-o", "jsonpath={.data.runtime-tenants\\.yaml}")
	out, err := utils.Run(cmd)
	return string(out), err
}

// fetchViaPortForward starts a one-shot `kubectl port-forward` to a
// free local port, fetches the given HTTP path, and tears the
// forwarder down. Used for both Loki and Mimir scrapes — each call
// runs its own forwarder so parallel specs don't contend on a shared
// channel (unlike the previous `kubectl exec deploy/loki -- wget ...`
// path which serialized through the apiserver's exec subprotocol).
//
// Defers process Kill() + Wait() so the kubectl process exits before
// the function returns; the local port is then free for the next
// caller. Each parallel spec gets its own ephemeral port via
// freeLocalPort.
func fetchViaPortForward(ns, deploy string, targetPort int, path string) (string, error) {
	port, err := freeLocalPort()
	if err != nil {
		return "", fmt.Errorf("allocate local port: %w", err)
	}
	pf := exec.Command("kubectl", "-n", ns, "port-forward",
		"deploy/"+deploy, fmt.Sprintf("%d:%d", port, targetPort))
	if err := pf.Start(); err != nil {
		return "", fmt.Errorf("start port-forward: %w", err)
	}
	defer func() {
		_ = pf.Process.Kill()
		_ = pf.Wait()
	}()

	// Wait for the local port to start accepting (kubectl port-forward
	// prints `Forwarding from 127.0.0.1:<port> -> <target>` once it's
	// ready; we just dial-poll instead of parsing).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, derr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if derr == nil {
			_ = c.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

// queryLokiOverride scrapes Loki's /metrics for the value of a specific
// limit override for a tenant, as observed by the overrides-exporter
// module. Returns "" if no override is loaded for that tenant/field.
//
// Uses port-forward + HTTP rather than `kubectl exec wget` for two
// reasons: (1) Loki's official image is distroless from v3.x onward
// (no wget/sh, the exec would fail on a fresh upstream bump), and
// (2) the apiserver's exec subprotocol serializes calls to the same
// pod, so multiple parallel specs hitting the Loki pod via exec
// would queue up; port-forward + HTTP runs each scrape on its own
// independent channel and unlocks real parallel-spec speedup.
func queryLokiOverride(tenant, field string) (string, error) {
	body, err := fetchViaPortForward("loki", "loki", 3100, "/metrics")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "loki_overrides{") {
			continue
		}
		if !strings.Contains(line, fmt.Sprintf(`limit_name="%s"`, field)) {
			continue
		}
		if !strings.Contains(line, fmt.Sprintf(`user="%s"`, tenant)) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		return fields[len(fields)-1], nil
	}
	return "", nil
}

// queryMimirRuntimeConfig fetches Mimir's /runtime_config endpoint.
// Mimir's image is distroless so this path was already required; now
// shared with Loki via fetchViaPortForward.
func queryMimirRuntimeConfig() (string, error) {
	return fetchViaPortForward("mimir", "mimir", 8080, "/runtime_config")
}

func freeLocalPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	return port, l.Close()
}

// ---- status-introspection helpers -----------------------------------------

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

// =========================================================================
// PARALLEL-SAFE specs. Each creates its own tenant namespace.
// =========================================================================

var _ = Describe("Loki: base CR propagates to the backend", func() {
	It("operator writes CM, Loki reports the override via /metrics", func() {
		ns := createTenantNS("loki-base")

		_, err := applyCR(lokiOverrideCR(ns, "base", 0, 8))
		Expect(err).NotTo(HaveOccurred())

		// Fast operator-side check first — proves the operator did its
		// job, isolates backend-side failures.
		Eventually(func() string {
			body, _ := readMergedOverrides(lokiCM.ns, lokiCM.name)
			return body
		}, 30*time.Second, pollInterval).Should(ContainSubstring(ns))

		// Backend-side: Loki's overrides-exporter reports the metric.
		Eventually(func() string {
			v, _ := queryLokiOverride(ns, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(Equal("8"))
	})
})

var _ = Describe("Loki: Applied condition message references CM coords + sha256", func() {
	It("status.conditions[Applied].message contains the CM name and hash", func() {
		ns := createTenantNS("loki-applied-msg")

		_, err := applyCR(lokiOverrideCR(ns, "base", 0, 8))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			st, err := crStatusJSON("lokitenantoverride.runtimeoverrides.io", ns, "base")
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
})

var _ = Describe("Loki: higher-weight CR overrides base on conflict", func() {
	It("base + boost in the same tenant → backend sees boost value", func() {
		ns := createTenantNS("loki-boost")

		_, err := applyCR(lokiOverrideCR(ns, "base", 0, 8))
		Expect(err).NotTo(HaveOccurred())
		_, err = applyCR(lokiOverrideCR(ns, "boost", 100, 32))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			v, _ := queryLokiOverride(ns, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(Equal("32"))
	})
})

var _ = Describe("Loki: ContributingPeers reflects same-tenant siblings", func() {
	It("base's status.contributingPeers lists boost", func() {
		ns := createTenantNS("loki-peers")

		_, err := applyCR(lokiOverrideCR(ns, "base", 0, 8))
		Expect(err).NotTo(HaveOccurred())
		_, err = applyCR(lokiOverrideCR(ns, "boost", 100, 32))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			st, err := crStatusJSON("lokitenantoverride.runtimeoverrides.io", ns, "base")
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
})

var _ = Describe("Loki: deleting the high-weight CR reverts the loaded override", func() {
	It("delete boost → backend reverts to base value", func() {
		ns := createTenantNS("loki-revert")

		_, err := applyCR(lokiOverrideCR(ns, "base", 0, 8))
		Expect(err).NotTo(HaveOccurred())
		_, err = applyCR(lokiOverrideCR(ns, "boost", 100, 32))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			v, _ := queryLokiOverride(ns, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(Equal("32"))

		_, err = utils.Run(exec.Command("kubectl", "-n", ns, "delete",
			"lokitenantoverride.runtimeoverrides.io", "boost"))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			v, _ := queryLokiOverride(ns, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(Equal("8"))
	})
})

var _ = Describe("Mimir: CR propagates to the backend via /runtime_config", func() {
	It("operator writes CM, Mimir's /runtime_config reports the override", func() {
		ns := createTenantNS("mimir-base")

		_, err := applyCR(mimirOverrideCR(ns, "default", 50000))
		Expect(err).NotTo(HaveOccurred())

		// Operator-side fast check.
		Eventually(func() string {
			body, _ := readMergedOverrides(mimirCM.ns, mimirCM.name)
			return body
		}, 30*time.Second, pollInterval).Should(ContainSubstring(ns))

		// Backend-side via /runtime_config.
		Eventually(func() string {
			body, _ := queryMimirRuntimeConfig()
			return body
		}, propagationWait, pollInterval).Should(And(
			ContainSubstring(ns),
			ContainSubstring("50000"),
		))
	})
})

var _ = Describe("Loki: deleting the tenant namespace drops the tenant from the backend", func() {
	It("kubectl delete ns → backend stops reporting the tenant", func() {
		ns := createTenantNS("loki-ns-delete")

		_, err := applyCR(lokiOverrideCR(ns, "base", 0, 8))
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			v, _ := queryLokiOverride(ns, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(Equal("8"))

		// Explicit delete here rather than DeferCleanup so the assertion
		// below blocks on the delete.
		_, err = utils.Run(exec.Command("kubectl", "delete", "ns", ns, "--wait=true"))
		Expect(err).NotTo(HaveOccurred())

		// Operator-side: tenant's stanza disappears from the merged CM.
		Eventually(func() string {
			body, _ := readMergedOverrides(lokiCM.ns, lokiCM.name)
			return body
		}, 30*time.Second, pollInterval).ShouldNot(ContainSubstring(ns))

		// Backend-side: Loki's overrides-exporter no longer reports the
		// tenant in any limit metric.
		Eventually(func() string {
			v, _ := queryLokiOverride(ns, "ingestion_rate_mb")
			return v
		}, propagationWait, pollInterval).Should(BeEmpty())
	})
})

// ---- admission + VAP: fast, transient CRs, no real propagation -------------

var _ = Describe("Loki webhook: rejects a typed-mismatch CR", func() {
	It("upstream Limits.UnmarshalYAML error propagates verbatim", func() {
		ns := createTenantNS("loki-webhook-typed")
		out, err := applyCR(fmt.Sprintf(`apiVersion: runtimeoverrides.io/v1alpha1
kind: LokiTenantOverride
metadata:
  name: bad-type
  namespace: %s
spec:
  overrides:
    ingestion_rate_mb: "eight"
`, ns))
		Expect(err).To(HaveOccurred(), "expected webhook rejection")
		Expect(out).To(ContainSubstring("loki"))
	})
})

var _ = Describe("Loki webhook: rejects an upstream-semantic violation", func() {
	It("retention_stream < 24h is rejected by upstream Limits.Validate()", func() {
		ns := createTenantNS("loki-webhook-semantic")
		out, err := applyCR(fmt.Sprintf(`apiVersion: runtimeoverrides.io/v1alpha1
kind: LokiTenantOverride
metadata:
  name: bad-semantic
  namespace: %s
spec:
  overrides:
    retention_stream:
      - selector: '{foo="bar"}'
        period: 12h
`, ns))
		Expect(err).To(HaveOccurred(), "expected webhook rejection")
		Expect(out).To(ContainSubstring("retention period"))
	})
})

var _ = Describe("Loki webhook: --validate=false does not bypass server-side admission", func() {
	It("kubectl --validate=false only skips CLIENT-side checks; webhook still fires", func() {
		ns := createTenantNS("loki-webhook-bypass")
		out, err := applyCRBypass(fmt.Sprintf(`apiVersion: runtimeoverrides.io/v1alpha1
kind: LokiTenantOverride
metadata:
  name: bad-type
  namespace: %s
spec:
  overrides:
    ingestion_rate_mb: "eight"
`, ns))
		Expect(err).To(HaveOccurred(),
			"webhook still fires server-side even with --validate=false; got out=%q", out)
	})
})

var _ = Describe("VAP: cross-namespace tenantId is rejected", func() {
	It("CR in ns A with spec.tenantId=B is rejected by the bundled VAP", func() {
		ns := createTenantNS("vap-cross-ns")
		out, err := applyCR(fmt.Sprintf(`apiVersion: runtimeoverrides.io/v1alpha1
kind: LokiTenantOverride
metadata:
  name: cross-ns-attempt
  namespace: %s
spec:
  tenantId: some-other-tenant
  overrides:
    ingestion_rate_mb: 99
`, ns))
		Expect(err).To(HaveOccurred(), "expected VAP rejection")
		Expect(out).To(Or(
			ContainSubstring("tenant ownership policy"),
			ContainSubstring("denied"),
		))
	})
})

// =========================================================================
// SERIAL specs — manipulate the operator's output ConfigMap directly.
// Must not run in parallel with other specs writing to the same CM.
// =========================================================================

var _ = Describe("Operator output CM lifecycle", Serial, func() {
	It("third-party write is reverted within seconds", func() {
		ns := createTenantNS("drift")
		_, err := applyCR(lokiOverrideCR(ns, "base", 0, 8))
		Expect(err).NotTo(HaveOccurred())

		// Wait for the operator to fold this tenant into the CM so we
		// know the "expected" content before clobbering.
		Eventually(func() string {
			body, _ := readMergedOverrides(lokiCM.ns, lokiCM.name)
			return body
		}, 30*time.Second, pollInterval).Should(ContainSubstring(ns))

		// Clobber the live CM.
		patch := `{"data":{"runtime-tenants.yaml":"overrides:\n  drift-clobbered:\n    sentinel: true\n"}}`
		_, err = utils.Run(exec.Command("kubectl", "-n", lokiCM.ns, "patch", "configmap",
			lokiCM.name, "--type=merge", "-p", patch))
		Expect(err).NotTo(HaveOccurred())

		// Operator reverts: our tenant stanza reappears, clobber sentinel
		// disappears.
		Eventually(func() string {
			body, _ := readMergedOverrides(lokiCM.ns, lokiCM.name)
			return body
		}, 30*time.Second, pollInterval).Should(And(
			Not(ContainSubstring("drift-clobbered")),
			ContainSubstring(ns),
		))

		// Operator emitted the ConfigMapDrifted event.
		Eventually(func() string {
			out, _ := utils.Run(exec.Command("kubectl", "-n", lokiCM.ns, "get", "events",
				"--field-selector=reason=ConfigMapDrifted",
				"-o", "jsonpath={.items[*].message}"))
			return string(out)
		}, 30*time.Second, pollInterval).Should(ContainSubstring("reverted"))
	})

	It("deleting the output ConfigMap directly causes the operator to recreate it", func() {
		// We need at least one CR to keep the CM populated after recreate.
		ns := createTenantNS("cm-delete")
		_, err := applyCR(lokiOverrideCR(ns, "base", 0, 8))
		Expect(err).NotTo(HaveOccurred())

		// Wait for first write.
		Eventually(func() error {
			_, gerr := utils.Run(exec.Command("kubectl", "-n", lokiCM.ns, "get", "cm", lokiCM.name))
			return gerr
		}, 30*time.Second, pollInterval).Should(Succeed())

		// Delete the CM directly.
		_, err = utils.Run(exec.Command("kubectl", "-n", lokiCM.ns, "delete", "configmap", lokiCM.name))
		Expect(err).NotTo(HaveOccurred())

		// Operator recreates it.
		Eventually(func() error {
			_, gerr := utils.Run(exec.Command("kubectl", "-n", lokiCM.ns, "get", "cm", lokiCM.name))
			return gerr
		}, 30*time.Second, pollInterval).Should(Succeed())
	})
})

