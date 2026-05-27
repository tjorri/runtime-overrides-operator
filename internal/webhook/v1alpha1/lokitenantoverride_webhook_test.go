// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package v1alpha1

import (
	"encoding/json"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"

	rov1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
	"github.com/tjorri/runtime-overrides-operator/internal/validate"
)

func rawJSON(v any) *runtime.RawExtension {
	b, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	return &runtime.RawExtension{Raw: b}
}

var _ = Describe("LokiTenantOverride Webhook", func() {
	var validator LokiTenantOverrideCustomValidator

	BeforeEach(func() {
		validator = LokiTenantOverrideCustomValidator{
			Validator: validate.New(validate.TargetLoki),
		}
	})

	It("admits a valid override", func() {
		obj := &rov1alpha1.LokiTenantOverride{
			Spec: rov1alpha1.LokiTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{"ingestion_rate_mb": 32}),
			},
		}
		warnings, err := validator.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeNil())
	})

	It("rejects an unknown field with the upstream error verbatim", func() {
		obj := &rov1alpha1.LokiTenantOverride{
			Spec: rov1alpha1.LokiTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{"definitely_not_a_real_field": 42}),
			},
		}
		_, err := validator.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(strings.ToLower(err.Error())).To(ContainSubstring("loki"))
	})

	It("rejects a numeric field given as a string (type mismatch)", func() {
		obj := &rov1alpha1.LokiTenantOverride{
			Spec: rov1alpha1.LokiTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{"ingestion_rate_mb": "eight"}),
			},
		}
		_, err := validator.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
	})

	It("rejects an upstream-semantic violation (retention_stream period < 24h)", func() {
		obj := &rov1alpha1.LokiTenantOverride{
			Spec: rov1alpha1.LokiTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{
					"retention_stream": []any{
						map[string]any{"selector": `{foo="bar"}`, "period": "12h"},
					},
				}),
			},
		}
		_, err := validator.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("retention period"))
	})

	It("validates updates the same way as creates", func() {
		oldObj := &rov1alpha1.LokiTenantOverride{
			Spec: rov1alpha1.LokiTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{"ingestion_rate_mb": 8}),
			},
		}
		newObj := &rov1alpha1.LokiTenantOverride{
			Spec: rov1alpha1.LokiTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{"definitely_not_a_real_field": 42}),
			},
		}
		_, err := validator.ValidateUpdate(ctx, oldObj, newObj)
		Expect(err).To(HaveOccurred())
	})

	It("admits deletions unconditionally", func() {
		obj := &rov1alpha1.LokiTenantOverride{}
		warnings, err := validator.ValidateDelete(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeNil())
	})
})
