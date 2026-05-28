//go:build e2e
// +build e2e

// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/tjorri/runtime-overrides-operator/test/utils"
)

// projectImage is the manager image built by `make e2e-image` and pushed
// to the local OCI registry sidecar that the kind cluster mirrors. The
// host pushes to `localhost:5001/...`; containerd inside the cluster
// rewrites that to `kind-registry:5000/...` (see test/e2e/kind-config.yaml).
// The Helm values file pins the chart's image.repository to this name.
const projectImage = "localhost:5001/runtime-overrides-operator:e2e"

// operatorNamespace is where the chart is installed.
const operatorNamespace = "runtime-overrides-operator-system"

// helmReleaseName is the operator's Helm release name.
const helmReleaseName = "ro-op"

// manifestsDir locates the supporting Loki/Mimir manifests and the
// Helm values file relative to the test package.
var manifestsDir string

// repoRoot is the absolute path to the repository root. Set in BeforeSuite;
// AfterSuite uses it to write cluster diagnostic artifacts before teardown
// (artifacts/{operator,loki,mimir}.log + events.txt + pods.txt).
var repoRoot string

// Optional environment variables:
//   - CERT_MANAGER_INSTALL_SKIP=true: assume cert-manager is already there.
//   - E2E_SKIP_IMAGE_BUILD=true:      assume the operator image is already
//     pushed to the local registry. Useful for fast local iteration.
var (
	skipCertManagerInstall        = os.Getenv("CERT_MANAGER_INSTALL_SKIP") == "true"
	skipImageBuild                = os.Getenv("E2E_SKIP_IMAGE_BUILD") == "true"
	isCertManagerAlreadyInstalled = false
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintln(GinkgoWriter, "Starting runtime-overrides-operator e2e suite")
	RunSpecs(t, "e2e suite")
}

// One-time suite setup runs in process 1 via the first
// SynchronizedBeforeSuite callback; every parallel process then runs
// the second callback to initialize per-process globals (paths). With
// `ginkgo --procs=N`, specs are distributed across N processes that
// share the kind cluster but each have their own Go runtime.
var _ = SynchronizedBeforeSuite(
	func() []byte {
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		manifestsDir = filepath.Join(wd, "manifests")
		repoRoot, err = filepath.Abs(filepath.Join(wd, "..", ".."))
		Expect(err).NotTo(HaveOccurred())

		if !skipImageBuild {
			By("building and pushing the manager image to the local e2e registry")
			_, err := utils.Run(exec.Command("make", "e2e-image"))
			ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build/push the manager image")
		}

		if !skipCertManagerInstall {
			isCertManagerAlreadyInstalled = utils.IsCertManagerCRDsInstalled()
			if !isCertManagerAlreadyInstalled {
				_, _ = fmt.Fprintln(GinkgoWriter, "Installing cert-manager...")
				Expect(utils.InstallCertManager()).To(Succeed(), "Failed to install cert-manager")
			} else {
				_, _ = fmt.Fprintln(GinkgoWriter, "cert-manager already installed; skipping")
			}
		}

		By("deploying Loki + Mimir single-binaries (parallel)")
		_, err = utils.Run(exec.Command("kubectl", "apply", "-f", filepath.Join(manifestsDir, "loki.yaml")))
		Expect(err).NotTo(HaveOccurred())
		_, err = utils.Run(exec.Command("kubectl", "apply", "-f", filepath.Join(manifestsDir, "mimir.yaml")))
		Expect(err).NotTo(HaveOccurred())

		rolloutReady := func(ns, deploy string) <-chan error {
			ch := make(chan error, 1)
			go func() {
				cmd := exec.Command("kubectl", "-n", ns, "rollout", "status",
					"deploy/"+deploy, "--timeout=180s")
				out, err := cmd.CombinedOutput()
				if err != nil {
					ch <- fmt.Errorf("rollout %s/%s: %w\n%s", ns, deploy, err, out)
					return
				}
				ch <- nil
			}()
			return ch
		}
		lokiReady := rolloutReady("loki", "loki")
		mimirReady := rolloutReady("mimir", "mimir")
		Expect(<-lokiReady).NotTo(HaveOccurred(), "Loki failed to become ready")
		Expect(<-mimirReady).NotTo(HaveOccurred(), "Mimir failed to become ready")

		By("installing the operator Helm chart")
		nsYAML := fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", operatorNamespace)
		applyCmd := exec.Command("kubectl", "apply", "-f", "-")
		applyCmd.Stdin = strings.NewReader(nsYAML)
		_, err = utils.Run(applyCmd)
		Expect(err).NotTo(HaveOccurred())

		chartPath := filepath.Join(repoRoot, "deploy", "charts", "runtime-overrides-operator")
		valuesPath := filepath.Join(manifestsDir, "operator-values.yaml")
		_, err = utils.Run(exec.Command("helm", "upgrade", "--install", helmReleaseName, chartPath,
			"--namespace", operatorNamespace,
			"--values", valuesPath,
			"--wait", "--timeout=180s",
		))
		Expect(err).NotTo(HaveOccurred(), "helm install failed")

		By("waiting for the operator deployment to be ready")
		_, err = utils.Run(exec.Command("kubectl", "-n", operatorNamespace,
			"rollout", "status", fmt.Sprintf("deploy/%s-runtime-overrides-operator", helmReleaseName),
			"--timeout=180s"))
		Expect(err).NotTo(HaveOccurred(), "operator failed to become ready")

		return nil
	},
	func(_ []byte) {
		// Per-process initialization: every parallel proc needs its own
		// copy of the path globals (helpers call into manifestsDir +
		// repoRoot in AfterSuite's diagnostic dump).
		wd, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		manifestsDir = filepath.Join(wd, "manifests")
		repoRoot, err = filepath.Abs(filepath.Join(wd, "..", ".."))
		Expect(err).NotTo(HaveOccurred())
	},
)

// SynchronizedAfterSuite's callbacks run in opposite order from
// BeforeSuite's: the per-process callback runs first in every proc;
// the once-only callback runs last, in process 1, after all procs
// have completed their per-process teardown.
var _ = SynchronizedAfterSuite(
	func() {
		// Per-process teardown — nothing today.
	},
	func() {
		// Once: dump diagnostics, then helm uninstall.
		dumpClusterArtifacts()

		if os.Getenv("E2E_SKIP_TEARDOWN") == "true" {
			_, _ = fmt.Fprintln(GinkgoWriter, "E2E_SKIP_TEARDOWN=true; leaving cluster state in place")
			return
		}

		By("uninstalling the operator chart")
		_, _ = utils.Run(exec.Command("helm", "uninstall", helmReleaseName, "-n", operatorNamespace))
		_, _ = utils.Run(exec.Command("kubectl", "delete", "ns", operatorNamespace, "--ignore-not-found"))

		By("tearing down Loki + Mimir")
		_, _ = utils.Run(exec.Command("kubectl", "delete", "-f", filepath.Join(manifestsDir, "loki.yaml"), "--ignore-not-found"))
		_, _ = utils.Run(exec.Command("kubectl", "delete", "-f", filepath.Join(manifestsDir, "mimir.yaml"), "--ignore-not-found"))

		if !skipCertManagerInstall && !isCertManagerAlreadyInstalled {
			_, _ = fmt.Fprintln(GinkgoWriter, "Uninstalling cert-manager...")
			utils.UninstallCertManager()
		}
	},
)

// dumpClusterArtifacts captures the operator, Loki, and Mimir pod logs
// plus cluster-wide events and pod listing into <repoRoot>/artifacts/.
// Best-effort: any individual command failure (typically because the
// resource doesn't exist on early-bootstrap suite failures) is logged
// but doesn't propagate. Files are overwritten on each run.
func dumpClusterArtifacts() {
	if repoRoot == "" {
		_, _ = fmt.Fprintln(GinkgoWriter, "dumpClusterArtifacts: repoRoot not set; skipping")
		return
	}
	dir := filepath.Join(repoRoot, "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_, _ = fmt.Fprintf(GinkgoWriter, "dumpClusterArtifacts: mkdir %s: %v\n", dir, err)
		return
	}

	dumps := []struct {
		path string
		args []string
	}{
		{"pods.txt", []string{"get", "pods", "-A", "-o", "wide"}},
		{"events.txt", []string{"get", "events", "-A", "--sort-by=.lastTimestamp"}},
		{"operator.log", []string{
			"-n", operatorNamespace, "logs",
			"-l", "app.kubernetes.io/name=runtime-overrides-operator",
			"--all-containers", "--tail=-1",
		}},
		{"loki.log", []string{"-n", "loki", "logs", "deploy/loki", "--tail=-1"}},
		{"mimir.log", []string{"-n", "mimir", "logs", "deploy/mimir", "--tail=-1"}},
	}
	for _, d := range dumps {
		out, err := utils.Run(exec.Command("kubectl", d.args...))
		if err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "dumpClusterArtifacts: kubectl %v: %v\n", d.args, err)
			continue
		}
		path := filepath.Join(dir, d.path)
		if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
			_, _ = fmt.Fprintf(GinkgoWriter, "dumpClusterArtifacts: write %s: %v\n", path, err)
		}
	}
}
