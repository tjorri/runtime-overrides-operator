// SPDX-License-Identifier: AGPL-3.0-only
// Copyright (c) 2026 tjorri

package v1alpha1

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	rov1alpha1 "github.com/tjorri/runtime-overrides-operator/api/v1alpha1"
	"github.com/tjorri/runtime-overrides-operator/internal/validate"
)

var _ = Describe("MimirTenantOverride Webhook", func() {
	var validator MimirTenantOverrideCustomValidator

	BeforeEach(func() {
		validator = MimirTenantOverrideCustomValidator{
			Validator: validate.New(validate.TargetMimir),
		}
	})

	It("admits a valid override", func() {
		obj := &rov1alpha1.MimirTenantOverride{
			Spec: rov1alpha1.MimirTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{"ingestion_rate": 50000}),
			},
		}
		warnings, err := validator.ValidateCreate(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeNil())
	})

	It("rejects an unknown field", func() {
		obj := &rov1alpha1.MimirTenantOverride{
			Spec: rov1alpha1.MimirTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{"definitely_not_a_real_field": 42}),
			},
		}
		_, err := validator.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(strings.ToLower(err.Error())).To(ContainSubstring("mimir"))
	})

	It("rejects a type mismatch", func() {
		obj := &rov1alpha1.MimirTenantOverride{
			Spec: rov1alpha1.MimirTenantOverrideSpec{
				Overrides: rawJSON(map[string]any{"ingestion_rate": "fast"}),
			},
		}
		_, err := validator.ValidateCreate(ctx, obj)
		Expect(err).To(HaveOccurred())
	})

	It("admits deletions unconditionally", func() {
		obj := &rov1alpha1.MimirTenantOverride{}
		warnings, err := validator.ValidateDelete(ctx, obj)
		Expect(err).NotTo(HaveOccurred())
		Expect(warnings).To(BeNil())
	})
})
