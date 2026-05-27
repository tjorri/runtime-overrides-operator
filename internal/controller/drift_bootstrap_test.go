// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
	"github.com/tjorri/runtime-overrides-operator/internal/render"
	"github.com/tjorri/runtime-overrides-operator/internal/validate"
)

var _ = Describe("BootstrapRunnable", func() {
	const targetNs = "monitoring"

	BeforeEach(func() {
		ensureNamespace(targetNs)
	})

	It("writes overrides: {} at startup before any CR exists", func() {
		cmName := types.NamespacedName{Namespace: targetNs, Name: "loki-runtime-tenants-bootstrap-test"}
		hashCache := NewHashCache()
		b := &BootstrapRunnable{
			Client:    k8sClient,
			HashCache: hashCache,
			Targets:   []BootstrapTarget{{Name: "loki-test", OutputCM: cmName}},
		}

		Expect(b.Start(ctx)).To(Succeed())

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		Expect(cm.Data[render.DataKey]).To(Equal("overrides: {}\n"))

		// HashCache populated.
		_, ok := hashCache.Get("loki-test")
		Expect(ok).To(BeTrue())

		// Idempotent re-run.
		Expect(b.Start(ctx)).To(Succeed())
	})
})

var _ = Describe("LokiReconciler drift watch", func() {
	const ns = "drift-tenant"
	const targetNs = "monitoring"

	BeforeEach(func() {
		ensureNamespace(ns)
		ensureNamespace(targetNs)
		deleteAllLokiOverrides()
	})

	It("reverts a third-party write to the output ConfigMap", func() {
		cmName := types.NamespacedName{Namespace: targetNs, Name: "loki-drift-test"}
		hashCache := NewHashCache()
		r := &LokiReconciler{
			Client:    k8sClient,
			OutputCM:  cmName,
			Validator: nil,
			HashCache: hashCache,
		}
		r.Validator = validatorOrPanic("loki")

		// Apply a CR so the CM has real content.
		Expect(k8sClient.Create(ctx, &v1alpha1.LokiTenantOverride{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: ns},
			Spec: v1alpha1.LokiTenantOverrideSpec{
				Overrides: rawOverrides(map[string]any{"ingestion_rate_mb": 8}),
			},
		})).To(Succeed())

		_, err := r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		// Confirm CM has the right body, capture its first hash.
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		firstBody := cm.Data[render.DataKey]
		firstHash := render.Hash([]byte(firstBody))
		Expect(firstHash).NotTo(BeEmpty())

		// Simulate a third-party stomp: clobber data via the API server.
		clobbered := cm.DeepCopy()
		clobbered.Data[render.DataKey] = "overrides:\n  acme:\n    clobbered: true\n"
		Expect(k8sClient.Update(ctx, clobbered)).To(Succeed())

		// Reconcile again — the drift check should fire and the CM should
		// be reverted to the operator's view.
		_, err = r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		reverted := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, reverted)).To(Succeed())
		Expect(render.Hash([]byte(reverted.Data[render.DataKey]))).To(Equal(firstHash))
		Expect(reverted.Data[render.DataKey]).NotTo(ContainSubstring("clobbered"))
	})
})

// validatorOrPanic returns a no-op validator. The drift test isn't about
// validation; the actual Loki validator is exercised in loki_controller_test.go.
func validatorOrPanic(_ string) validate.Validator {
	return noopValidator{}
}

type noopValidator struct{}

func (noopValidator) Validate(_ map[string]any) error { return nil }
func (noopValidator) Target() validate.Target         { return validate.TargetLoki }
