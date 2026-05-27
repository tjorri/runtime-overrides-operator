// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package controller

import (
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
	"github.com/tjorri/runtime-overrides-operator/internal/render"
	"github.com/tjorri/runtime-overrides-operator/internal/validate"
)

func rawOverrides(v map[string]any) *runtime.RawExtension {
	b, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return &runtime.RawExtension{Raw: b}
}

// targetNs (and newLokiReconciler's CM coords) match the namespace the
// test Describe block ensures exists in BeforeEach.
const targetNs = "monitoring"

func newLokiReconciler() (*LokiReconciler, types.NamespacedName) {
	cmName := types.NamespacedName{Namespace: targetNs, Name: "loki-runtime-tenants"}
	return &LokiReconciler{
		Client:    k8sClient,
		OutputCM:  cmName,
		Validator: validate.New(validate.TargetLoki),
	}, cmName
}

func ensureNamespace(ns string) {
	GinkgoHelper()
	obj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
	err := k8sClient.Create(ctx, obj)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Fail(fmt.Sprintf("create namespace %s: %v", ns, err))
	}
}

func deleteAllLokiOverrides() {
	GinkgoHelper()
	var list v1alpha1.LokiTenantOverrideList
	Expect(k8sClient.List(ctx, &list)).To(Succeed())
	for i := range list.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &list.Items[i]))).To(Succeed())
	}
	// envtest doesn't run the namespace GC controller, so deletes are
	// effective immediately rather than via finalizers; still wait briefly
	// for the cache to settle.
	Eventually(func() int {
		var l v1alpha1.LokiTenantOverrideList
		_ = k8sClient.List(ctx, &l)
		return len(l.Items)
	}, "5s", "100ms").Should(Equal(0))
}

var _ = Describe("LokiReconciler", func() {
	const ns = "test-tenant-a"

	BeforeEach(func() {
		ensureNamespace(ns)
		ensureNamespace(targetNs)
		deleteAllLokiOverrides()
	})

	It("renders a single valid CR into the output ConfigMap", func() {
		r, cmName := newLokiReconciler()

		Expect(k8sClient.Create(ctx, &v1alpha1.LokiTenantOverride{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: ns},
			Spec: v1alpha1.LokiTenantOverrideSpec{
				Weight:    0,
				Overrides: rawOverrides(map[string]any{"ingestion_rate_mb": 8}),
			},
		})).To(Succeed())

		_, err := r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		body := cm.Data[render.DataKey]
		Expect(body).To(ContainSubstring("ingestion_rate_mb: 8"))
		Expect(body).To(ContainSubstring(ns))

		// Re-fetch and assert Applied=True with hash + CM ref in message.
		var got v1alpha1.LokiTenantOverride
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "base"}, &got)).To(Succeed())
		applied := findCondition(got.Status.Conditions, v1alpha1.ConditionApplied)
		Expect(applied).NotTo(BeNil())
		Expect(applied.Status).To(Equal(metav1.ConditionTrue))
		Expect(applied.Reason).To(Equal(v1alpha1.ReasonWrittenToConfigMap))
		Expect(applied.Message).To(ContainSubstring(cmName.Name))
		Expect(applied.Message).To(ContainSubstring("sha256:"))
		Expect(got.Status.EffectiveTenantId).To(Equal(ns))
	})

	It("excludes invalid CRs from the merge with Validated=False", func() {
		r, cmName := newLokiReconciler()

		Expect(k8sClient.Create(ctx, &v1alpha1.LokiTenantOverride{
			ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: ns},
			Spec: v1alpha1.LokiTenantOverrideSpec{
				// stream_retention period < 24h is rejected by upstream Validate().
				Overrides: rawOverrides(map[string]any{
					"retention_stream": []any{
						map[string]any{"selector": `{foo="bar"}`, "period": "12h"},
					},
				}),
			},
		})).To(Succeed())

		_, err := r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		// CM should exist as overrides:{} since the only CR failed.
		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		Expect(cm.Data[render.DataKey]).To(Equal("overrides: {}\n"))

		var got v1alpha1.LokiTenantOverride
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "bad"}, &got)).To(Succeed())
		validated := findCondition(got.Status.Conditions, v1alpha1.ConditionValidated)
		Expect(validated).NotTo(BeNil())
		Expect(validated.Status).To(Equal(metav1.ConditionFalse))
		Expect(validated.Reason).To(Equal(v1alpha1.ReasonValidationFailed))
	})

	It("layers two CRs: higher weight wins on conflict", func() {
		r, cmName := newLokiReconciler()

		Expect(k8sClient.Create(ctx, &v1alpha1.LokiTenantOverride{
			ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: ns},
			Spec: v1alpha1.LokiTenantOverrideSpec{
				Weight:    0,
				Overrides: rawOverrides(map[string]any{"ingestion_rate_mb": 8}),
			},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &v1alpha1.LokiTenantOverride{
			ObjectMeta: metav1.ObjectMeta{Name: "boost", Namespace: ns},
			Spec: v1alpha1.LokiTenantOverrideSpec{
				Weight:    100,
				Overrides: rawOverrides(map[string]any{"ingestion_rate_mb": 32}),
			},
		})).To(Succeed())

		_, err := r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		Expect(cm.Data[render.DataKey]).To(ContainSubstring("ingestion_rate_mb: 32"))

		// ContributingPeers should be populated on both CRs.
		var base v1alpha1.LokiTenantOverride
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "base"}, &base)).To(Succeed())
		Expect(base.Status.ContributingPeers).To(HaveLen(1))
		Expect(base.Status.ContributingPeers[0].Name).To(Equal("boost"))
	})

	It("drops a tenant block when its only CR is deleted", func() {
		r, cmName := newLokiReconciler()

		cr := &v1alpha1.LokiTenantOverride{
			ObjectMeta: metav1.ObjectMeta{Name: "only", Namespace: ns},
			Spec: v1alpha1.LokiTenantOverrideSpec{
				Overrides: rawOverrides(map[string]any{"ingestion_rate_mb": 8}),
			},
		}
		Expect(k8sClient.Create(ctx, cr)).To(Succeed())

		_, err := r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		cm := &corev1.ConfigMap{}
		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		Expect(cm.Data[render.DataKey]).To(ContainSubstring(ns))

		Expect(k8sClient.Delete(ctx, cr)).To(Succeed())
		// Wait briefly for the informer cache to reflect deletion.
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "only"}, cr)
		}, 5*time.Second).Should(HaveOccurred())

		_, err = r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, cmName, cm)).To(Succeed())
		Expect(cm.Data[render.DataKey]).To(Equal("overrides: {}\n"))
	})

	It("only updates status when conditions actually transition", func() {
		r, _ := newLokiReconciler()

		Expect(k8sClient.Create(ctx, &v1alpha1.LokiTenantOverride{
			ObjectMeta: metav1.ObjectMeta{Name: "stable", Namespace: ns},
			Spec: v1alpha1.LokiTenantOverrideSpec{
				Overrides: rawOverrides(map[string]any{"ingestion_rate_mb": 8}),
			},
		})).To(Succeed())

		_, err := r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		var first v1alpha1.LokiTenantOverride
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "stable"}, &first)).To(Succeed())
		firstApplied := findCondition(first.Status.Conditions, v1alpha1.ConditionApplied)
		Expect(firstApplied).NotTo(BeNil())
		firstTransition := firstApplied.LastTransitionTime

		// Reconcile again with no CR changes. LastTransitionTime should
		// remain stable — that's the transition-gated status contract.
		time.Sleep(1 * time.Second)
		_, err = r.Reconcile(ctx, reconcile.Request{})
		Expect(err).NotTo(HaveOccurred())

		var second v1alpha1.LokiTenantOverride
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: "stable"}, &second)).To(Succeed())
		secondApplied := findCondition(second.Status.Conditions, v1alpha1.ConditionApplied)
		Expect(secondApplied).NotTo(BeNil())
		Expect(secondApplied.LastTransitionTime).To(Equal(firstTransition))
	})
})

func findCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}
