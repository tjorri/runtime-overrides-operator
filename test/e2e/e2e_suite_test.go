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

// projectImage is the manager image built by `make docker-build` and loaded
// into the KinD cluster ahead of the suite. The Helm chart's image.repository
// is overridden to point at this tag.
const projectImage = "example.com/runtime-overrides-operator:e2e"

// operatorNamespace is where the chart is installed.
const operatorNamespace = "runtime-overrides-operator-system"

// helmReleaseName is the operator's Helm release name.
const helmReleaseName = "ro-op"

// manifestsDir locates the supporting Loki/Mimir manifests and the
// Helm values file relative to the test package.
var manifestsDir string

// Optional environment variables:
//   - CERT_MANAGER_INSTALL_SKIP=true: assume cert-manager is already there.
//   - E2E_SKIP_IMAGE_BUILD=true:      assume the operator image is already
//     built and loaded into KinD. Useful for fast local iteration.
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

var _ = BeforeSuite(func() {
	wd, err := os.Getwd()
	Expect(err).NotTo(HaveOccurred())
	manifestsDir = filepath.Join(wd, "manifests")

	if !skipImageBuild {
		By("building the manager image")
		cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectImage))
		_, err := utils.Run(cmd)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to build the manager image")

		By("loading the manager image into KinD")
		err = utils.LoadImageToKindClusterWithName(projectImage)
		ExpectWithOffset(1, err).NotTo(HaveOccurred(), "Failed to load the manager image into KinD")
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

	By("deploying Loki single-binary")
	_, err = utils.Run(exec.Command("kubectl", "apply", "-f", filepath.Join(manifestsDir, "loki.yaml")))
	Expect(err).NotTo(HaveOccurred())
	_, err = utils.Run(exec.Command("kubectl", "-n", "loki", "rollout", "status", "deploy/loki", "--timeout=180s"))
	Expect(err).NotTo(HaveOccurred(), "Loki failed to become ready")

	By("deploying Mimir single-binary")
	_, err = utils.Run(exec.Command("kubectl", "apply", "-f", filepath.Join(manifestsDir, "mimir.yaml")))
	Expect(err).NotTo(HaveOccurred())
	_, err = utils.Run(exec.Command("kubectl", "-n", "mimir", "rollout", "status", "deploy/mimir", "--timeout=180s"))
	Expect(err).NotTo(HaveOccurred(), "Mimir failed to become ready")

	By("installing the operator Helm chart")
	// Use server-side apply for idempotency — re-runs (E2E_SKIP_TEARDOWN
	// from a prior pass) shouldn't fail just because the namespace exists.
	nsYAML := fmt.Sprintf("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: %s\n", operatorNamespace)
	applyCmd := exec.Command("kubectl", "apply", "-f", "-")
	applyCmd.Stdin = strings.NewReader(nsYAML)
	_, err = utils.Run(applyCmd)
	Expect(err).NotTo(HaveOccurred())

	repoRoot, err := filepath.Abs(filepath.Join(wd, "..", ".."))
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
})

var _ = AfterSuite(func() {
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
})
