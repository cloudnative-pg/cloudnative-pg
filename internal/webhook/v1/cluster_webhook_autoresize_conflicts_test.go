/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package v1

import (
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests cover cross-field configuration conflicts and degenerate
// inputs that the CRD schema validation (kubebuilder markers) cannot
// catch. CRD markers enforce per-field ranges (e.g., usageThreshold 1-99,
// maxActionsPerDay 0-10), but cannot enforce semantic relationships between
// fields (e.g., limit must be >= current size, minStep must be <= limit).
//
// For each test, we document:
//   - CURRENT: what happens today (accepted/rejected)
//   - EXPECTED: what should happen (may differ from current if webhook
//     should add a new check)
//
// Tests marked "documents current behavior" verify existing behavior without
// asserting it's correct — they serve as regression anchors if we add new
// validation later.

var _ = Describe("auto-resize configuration conflicts", func() {
	var v *ClusterCustomValidator

	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	// Helper to build a minimal valid single-volume cluster with resize enabled.
	makeCluster := func(mods ...func(*apiv1.Cluster)) *apiv1.Cluster {
		c := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
					Resize: &apiv1.ResizeConfiguration{
						Enabled: true,
						Strategy: &apiv1.ResizeStrategy{
							WALSafetyPolicy: &apiv1.WALSafetyPolicy{
								AcknowledgeWALRisk: ptr.To(true),
							},
						},
					},
				},
			},
		}
		for _, mod := range mods {
			mod(c)
		}
		return c
	}

	// Helper to build a multi-volume cluster (data + WAL).
	makeMultiVolumeCluster := func(mods ...func(*apiv1.Cluster)) *apiv1.Cluster {
		c := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
					Resize: &apiv1.ResizeConfiguration{
						Enabled: true,
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "5Gi",
				},
			},
		}
		for _, mod := range mods {
			mod(c)
		}
		return c
	}

	Context("expansion policy cross-field conflicts", func() {
		It("should accept minStep equal to maxStep", func() {
			// CURRENT: accepted (minStep <= maxStep is valid)
			// EXPECTED: accepted — equal values mean a fixed step size
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step:    intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
					MinStep: "5Gi",
					MaxStep: "5Gi",
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept 100% step (documents current behavior)", func() {
			// CURRENT: accepted — doubles the volume on each resize
			// EXPECTED: accepted — aggressive but valid; user knows what they want
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step: intstr.IntOrString{Type: intstr.String, StrVal: "100%"},
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should reject 101% step", func() {
			// CURRENT: rejected (percentage must be 1-100)
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step: intstr.IntOrString{Type: intstr.String, StrVal: "101%"},
				}
			})
			errs := v.validateAutoResize(cluster)
			Expect(errs).ToNot(BeEmpty())
			Expect(errs[0].Field).To(ContainSubstring("step"))
		})

		It("should accept 1% step (minimum valid percentage)", func() {
			// CURRENT: accepted (1% is the smallest valid percentage)
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step: intstr.IntOrString{Type: intstr.String, StrVal: "1%"},
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept limit smaller than size (documents current behavior — silent no-op at runtime)", func() {
			// CURRENT: accepted — the webhook does not compare limit to size.
			// EXPECTED: ideally this should be rejected or warned, since the
			// reconciler will never resize (newSize would always <= currentSize).
			// Documenting as a known gap.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Size = "10Gi"
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Limit: "5Gi",
				}
			})
			// Documents current behavior: this is accepted
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept step larger than limit (documents current behavior — capped at runtime)", func() {
			// CURRENT: accepted — the reconciler caps newSize to limit.
			// EXPECTED: acceptable — the clamping logic handles it correctly.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step:  intstr.IntOrString{Type: intstr.String, StrVal: "50Gi"},
					Limit: "15Gi",
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept minStep larger than limit (documents current behavior — capped at runtime)", func() {
			// CURRENT: accepted — the reconciler applies limit cap after minStep clamp.
			// EXPECTED: could warn, but capping logic handles it safely.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step:    intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
					MinStep: "20Gi",
					Limit:   "15Gi",
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should silently ignore minStep/maxStep with absolute step (documents current behavior)", func() {
			// CURRENT: accepted — minStep and maxStep are only applied to
			// percentage-based steps in the clamping code. With an absolute
			// step, they are silently ignored.
			// EXPECTED: could warn, but not an error.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step:    intstr.IntOrString{Type: intstr.String, StrVal: "5Gi"},
					MinStep: "10Gi",
					MaxStep: "3Gi",
				}
			})
			// Even though minStep > maxStep, this is accepted because
			// with an absolute step, both are ignored.
			// NOTE: the webhook DOES reject minStep > maxStep unconditionally
			// (it doesn't check step type). So this actually produces an error.
			errs := v.validateAutoResize(cluster)
			// This documents that the minStep > maxStep check fires regardless
			// of step type. This is conservative (rejects more) which is fine.
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Detail).To(ContainSubstring("minStep must not be greater than maxStep"))
		})

		It("should accept valid minStep/maxStep with absolute step (documents that they are ignored)", func() {
			// CURRENT: accepted — minStep and maxStep pass validation (valid
			// quantities, minStep <= maxStep) even though they have no effect
			// at runtime with an absolute step.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step:    intstr.IntOrString{Type: intstr.String, StrVal: "5Gi"},
					MinStep: "1Gi",
					MaxStep: "10Gi",
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept zero-value step (documents default behavior)", func() {
			// CURRENT: accepted — empty step defaults to "20%" at runtime
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Limit: "100Gi",
					// Step is zero-value (empty) — defaults to 20% at runtime
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})
	})

	Context("trigger configuration conflicts", func() {
		It("should accept both usageThreshold and minAvailable set (OR logic at runtime)", func() {
			// CURRENT: accepted — both triggers are OR-ed at runtime.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Triggers = &apiv1.ResizeTriggers{
					UsageThreshold: ptr.To(80),
					MinAvailable:   "5Gi",
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept large minAvailable relative to volume size (documents current behavior)", func() {
			// CURRENT: accepted — minAvailable is validated as a quantity but
			// not compared to the volume size. With a 2Gi volume and 100Gi
			// minAvailable, the trigger fires immediately and perpetually.
			// EXPECTED: could warn, but the user may intend this to force
			// immediate resize on first probe cycle.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Size = "2Gi"
				c.Spec.StorageConfiguration.Resize.Triggers = &apiv1.ResizeTriggers{
					MinAvailable: "100Gi",
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept very low usageThreshold (documents current behavior)", func() {
			// CURRENT: accepted — usageThreshold=1 means trigger at 1% usage,
			// which will fire almost immediately.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Triggers = &apiv1.ResizeTriggers{
					UsageThreshold: ptr.To(1),
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})
	})

	Context("multiple errors accumulated", func() {
		It("should report all errors when multiple fields are invalid", func() {
			// Verify that the webhook collects ALL errors, not just the first.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Triggers = &apiv1.ResizeTriggers{
					MinAvailable: "not-valid",
				}
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step:    intstr.IntOrString{Type: intstr.String, StrVal: "bad%"},
					MinStep: "also-bad",
					MaxStep: "still-bad",
					Limit:   "nope",
				}
				c.Spec.StorageConfiguration.Resize.Strategy = &apiv1.ResizeStrategy{
					WALSafetyPolicy: &apiv1.WALSafetyPolicy{
						AcknowledgeWALRisk: ptr.To(true),
						MaxPendingWALFiles: ptr.To(-5),
					},
				}
			})
			errs := v.validateAutoResize(cluster)
			// Should have errors for: minAvailable, step, minStep, maxStep, limit, maxPendingWALFiles
			Expect(len(errs)).To(BeNumerically(">=", 5),
				"webhook should collect all validation errors, not stop at the first")
		})

		It("should report errors across data, WAL, and tablespace simultaneously", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Limit: "not-valid-data",
							},
						},
					},
					WalStorage: &apiv1.StorageConfiguration{
						Size: "5Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Limit: "not-valid-wal",
							},
						},
					},
					Tablespaces: []apiv1.TablespaceConfiguration{
						{
							Name: "tbs1",
							Storage: apiv1.StorageConfiguration{
								Size: "20Gi",
								Resize: &apiv1.ResizeConfiguration{
									Enabled: true,
									Expansion: &apiv1.ExpansionPolicy{
										Limit: "not-valid-tbs",
									},
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			// Should have errors from all three: data, WAL, tablespace
			// Plus the acknowledgeWALRisk error for single-volume (no WAL resize
			// required, but data volume requires ack when no separate WAL).
			// Actually this cluster HAS walStorage so it's multi-volume.
			Expect(len(errs)).To(BeNumerically(">=", 3),
				"should validate all storage types independently")

			// Verify errors reference different paths
			fieldPaths := make([]string, 0, len(errs))
			for _, e := range errs {
				fieldPaths = append(fieldPaths, e.Field)
			}
			Expect(fieldPaths).To(ContainElement(ContainSubstring("storage")))
			Expect(fieldPaths).To(ContainElement(ContainSubstring("walStorage")))
			Expect(fieldPaths).To(ContainElement(ContainSubstring("tablespaces")))
		})
	})

	Context("WAL safety policy edge cases", func() {
		It("should accept zero maxPendingWALFiles (disables pending WAL check)", func() {
			// CURRENT: accepted — 0 means "don't check pending WAL files"
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Strategy = &apiv1.ResizeStrategy{
					WALSafetyPolicy: &apiv1.WALSafetyPolicy{
						AcknowledgeWALRisk: ptr.To(true),
						MaxPendingWALFiles: ptr.To(0),
					},
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept zero maxSlotRetentionBytes (disables slot check)", func() {
			// CURRENT: accepted — 0 means "don't check slot retention"
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Strategy = &apiv1.ResizeStrategy{
					WALSafetyPolicy: &apiv1.WALSafetyPolicy{
						AcknowledgeWALRisk:    ptr.To(true),
						MaxSlotRetentionBytes: ptr.To(int64(0)),
					},
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should require acknowledgeWALRisk even when all WAL safety checks are disabled", func() {
			// CURRENT: rejected — acknowledgeWALRisk is required for single-volume
			// regardless of other WAL safety settings.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Strategy = &apiv1.ResizeStrategy{
					WALSafetyPolicy: &apiv1.WALSafetyPolicy{
						AcknowledgeWALRisk:    ptr.To(false), // explicitly false
						RequireArchiveHealthy: ptr.To(false),
						MaxPendingWALFiles:    ptr.To(0),
						MaxSlotRetentionBytes: ptr.To(int64(0)),
					},
				}
			})
			errs := v.validateAutoResize(cluster)
			Expect(errs).ToNot(BeEmpty())
			Expect(errs[0].Detail).To(ContainSubstring("acknowledgeWALRisk"))
		})

		It("should not require acknowledgeWALRisk on WAL volume resize config", func() {
			// WAL volumes are never "single-volume" — the check is for the data
			// volume in clusters without separate WAL storage.
			cluster := makeMultiVolumeCluster(func(c *apiv1.Cluster) {
				c.Spec.WalStorage.Resize = &apiv1.ResizeConfiguration{
					Enabled: true,
					// No acknowledgeWALRisk — should be fine for WAL volume
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should not require acknowledgeWALRisk on tablespace resize config", func() {
			cluster := makeMultiVolumeCluster(func(c *apiv1.Cluster) {
				c.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
					{
						Name: "tbs1",
						Storage: apiv1.StorageConfiguration{
							Size: "20Gi",
							Resize: &apiv1.ResizeConfiguration{
								Enabled: true,
								// No acknowledgeWALRisk — should be fine for tablespace
							},
						},
					},
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})
	})

	Context("minimal configuration (all defaults)", func() {
		It("should accept resize with only enabled=true on multi-volume cluster", func() {
			// All fields default: usageThreshold=80, step=20%, minStep=2Gi,
			// maxStep=500Gi, maxActionsPerDay=3, no limit.
			cluster := makeMultiVolumeCluster()
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept resize with only enabled=true and acknowledgeWALRisk on single-volume", func() {
			cluster := makeCluster()
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should accept disabled resize with invalid sub-fields (documents current behavior)", func() {
			// CURRENT: accepted — when enabled=false, validation is skipped entirely.
			// This is intentional: users may configure resize before enabling it.
			cluster := makeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize.Enabled = false
				c.Spec.StorageConfiguration.Resize.Expansion = &apiv1.ExpansionPolicy{
					Step:  intstr.IntOrString{Type: intstr.String, StrVal: "garbage"},
					Limit: "also-garbage",
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})
	})

	Context("data + WAL + tablespace combined", func() {
		It("should accept independent resize configs on all three volume types", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "100Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Triggers: &apiv1.ResizeTriggers{
								UsageThreshold: ptr.To(85),
							},
							Expansion: &apiv1.ExpansionPolicy{
								Step:  intstr.IntOrString{Type: intstr.String, StrVal: "10%"},
								Limit: "500Gi",
							},
							Strategy: &apiv1.ResizeStrategy{
								MaxActionsPerDay: ptr.To(5),
							},
						},
					},
					WalStorage: &apiv1.StorageConfiguration{
						Size: "50Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Triggers: &apiv1.ResizeTriggers{
								UsageThreshold: ptr.To(90),
								MinAvailable:   "2Gi",
							},
							Expansion: &apiv1.ExpansionPolicy{
								Step: intstr.IntOrString{Type: intstr.String, StrVal: "5Gi"},
							},
							Strategy: &apiv1.ResizeStrategy{
								MaxActionsPerDay: ptr.To(2),
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									RequireArchiveHealthy: ptr.To(true),
									MaxPendingWALFiles:    ptr.To(50),
								},
							},
						},
					},
					Tablespaces: []apiv1.TablespaceConfiguration{
						{
							Name: "fast",
							Storage: apiv1.StorageConfiguration{
								Size: "200Gi",
								Resize: &apiv1.ResizeConfiguration{
									Enabled: true,
									Expansion: &apiv1.ExpansionPolicy{
										Step:    intstr.IntOrString{Type: intstr.String, StrVal: "30%"},
										MinStep: "10Gi",
										MaxStep: "100Gi",
										Limit:   "1Ti",
									},
								},
							},
						},
						{
							Name: "archive",
							Storage: apiv1.StorageConfiguration{
								Size: "500Gi",
								Resize: &apiv1.ResizeConfiguration{
									Enabled: true,
									Expansion: &apiv1.ExpansionPolicy{
										Step: intstr.IntOrString{Type: intstr.String, StrVal: "50Gi"},
									},
								},
							},
						},
					},
				},
			}
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should enable resize on data only, not WAL or tablespaces", func() {
			cluster := makeMultiVolumeCluster(func(c *apiv1.Cluster) {
				c.Spec.StorageConfiguration.Resize = &apiv1.ResizeConfiguration{
					Enabled: true,
				}
				// WAL and tablespace resize left nil — should be fine
				c.Spec.Tablespaces = []apiv1.TablespaceConfiguration{
					{
						Name: "tbs1",
						Storage: apiv1.StorageConfiguration{
							Size: "20Gi",
							// No resize config
						},
					},
				}
			})
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})
	})
})
