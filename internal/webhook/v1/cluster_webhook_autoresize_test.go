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
	"strings"

	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("auto-resize validation", func() {
	var v *ClusterCustomValidator

	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	Context("validateAutoResize", func() {
		It("should succeed with no resize configuration", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
					},
				},
			}
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should succeed with valid resize configuration", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Triggers: &apiv1.ResizeTriggers{
								UsageThreshold: ptr.To(80),
							},
							Expansion: &apiv1.ExpansionPolicy{
								Step:  intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
								Limit: "100Gi",
							},
							Strategy: &apiv1.ResizeStrategy{
								MaxActionsPerDay: ptr.To(3),
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should require acknowledgeWALRisk for single-volume clusters", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
						},
					},
					// No WalStorage → single-volume cluster
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Detail).To(ContainSubstring("acknowledgeWALRisk"))
		})

		It("should not require acknowledgeWALRisk for multi-volume clusters", func() {
			cluster := &apiv1.Cluster{
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
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should skip validation for disabled resize", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: false,
						},
					},
				},
			}
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})
	})

	Context("validateResizeTriggers", func() {
		It("should succeed with valid triggers", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Triggers: &apiv1.ResizeTriggers{
								UsageThreshold: ptr.To(85),
								MinAvailable:   "5Gi",
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should reject invalid minAvailable", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Triggers: &apiv1.ResizeTriggers{
								MinAvailable: "invalid",
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("minAvailable"))
		})
	})

	Context("validateExpansionPolicy", func() {
		It("should succeed with valid percentage step", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Step: intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should succeed with valid absolute step", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Step: intstr.IntOrString{Type: intstr.String, StrVal: "10Gi"},
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			Expect(v.validateAutoResize(cluster)).To(BeEmpty())
		})

		It("should reject invalid percentage step", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Step: intstr.IntOrString{Type: intstr.String, StrVal: "0%"},
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("step"))
		})

		It("should reject invalid absolute step", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Step: intstr.IntOrString{Type: intstr.String, StrVal: "invalid"},
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("step"))
		})

		It("should reject integer step values", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Step: intstr.FromInt(20),
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("step"))
		})

		It("should reject invalid limit", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Limit: "not-a-quantity",
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("limit"))
		})

		It("should reject minStep > maxStep", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								MinStep: "100Gi",
								MaxStep: "10Gi",
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Detail).To(ContainSubstring("minStep must not be greater than maxStep"))
		})
	})

	Context("validateWALSafetyPolicy", func() {
		It("should reject negative maxPendingWALFiles", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
									MaxPendingWALFiles: ptr.To(-1),
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("maxPendingWALFiles"))
		})

		It("should reject negative maxSlotRetentionBytes", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk:    true,
									MaxSlotRetentionBytes: ptr.To(int64(-1)),
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("maxSlotRetentionBytes"))
		})
	})

	Context("WAL storage and tablespace validation", func() {
		It("should validate WAL storage resize configuration", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
					},
					WalStorage: &apiv1.StorageConfiguration{
						Size: "5Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Limit: "invalid",
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("walStorage"))
		})

		It("should validate tablespace resize configuration", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
					},
					WalStorage: &apiv1.StorageConfiguration{
						Size: "5Gi",
					},
					Tablespaces: []apiv1.TablespaceConfiguration{
						{
							Name: "tbs1",
							Storage: apiv1.StorageConfiguration{
								Size: "20Gi",
								Resize: &apiv1.ResizeConfiguration{
									Enabled: true,
									Expansion: &apiv1.ExpansionPolicy{
										Step: intstr.IntOrString{Type: intstr.String, StrVal: "invalid%"},
									},
								},
							},
						},
					},
				},
			}
			errs := v.validateAutoResize(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Field).To(ContainSubstring("tablespaces"))
		})
	})
})

var _ = Describe("getAutoResizeWarnings", func() {
	Context("maxActionsPerDay: 0 warning", func() {
		It("should warn when maxActionsPerDay is 0", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Strategy: &apiv1.ResizeStrategy{
								MaxActionsPerDay: ptr.To(0),
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			warnings := getAutoResizeWarnings(cluster)
			Expect(warnings).ToNot(BeEmpty())
			Expect(warnings[0]).To(ContainSubstring("effectively disables auto-resize"))
		})
	})

	Context("minAvailable > size warning", func() {
		It("should warn when minAvailable exceeds volume size", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "1Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Triggers: &apiv1.ResizeTriggers{
								MinAvailable: "5Gi",
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			warnings := getAutoResizeWarnings(cluster)
			Expect(warnings).ToNot(BeEmpty())
			Expect(warnings[0]).To(ContainSubstring("resize will trigger immediately"))
		})
	})

	Context("limit <= size warning", func() {
		It("should warn when limit is less than or equal to current size", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Limit: "5Gi",
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			warnings := getAutoResizeWarnings(cluster)
			Expect(warnings).ToNot(BeEmpty())
			Expect(warnings[0]).To(ContainSubstring("auto-resize will never increase"))
		})
	})

	Context("minStep/maxStep with absolute step warning", func() {
		It("should warn when minStep/maxStep used with absolute step", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Expansion: &apiv1.ExpansionPolicy{
								Step:    intstr.IntOrString{Type: intstr.String, StrVal: "10Gi"},
								MinStep: "2Gi",
							},
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
				},
			}
			warnings := getAutoResizeWarnings(cluster)
			Expect(warnings).ToNot(BeEmpty())
			Expect(warnings[0]).To(ContainSubstring("apply only to percentage-based steps"))
		})
	})

	Context("acknowledgeWALRisk on dual-volume warning", func() {
		It("should warn when acknowledgeWALRisk is set on a cluster with separate WAL volume", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk: true,
								},
							},
						},
					},
					WalStorage: &apiv1.StorageConfiguration{
						Size: "5Gi",
					},
				},
			}
			warnings := getAutoResizeWarnings(cluster)
			Expect(warnings).ToNot(BeEmpty())
			Expect(warnings[0]).To(ContainSubstring("has no effect"))
		})
	})

	Context("requireArchiveHealthy without backup warning", func() {
		It("should warn when requireArchiveHealthy is enabled but no backup configured", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Strategy: &apiv1.ResizeStrategy{
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk:    true,
									RequireArchiveHealthy: ptr.To(true),
								},
							},
						},
					},
					// No Backup configured
				},
			}
			warnings := getAutoResizeWarnings(cluster)
			Expect(warnings).ToNot(BeEmpty())
			// Check that one of the warnings contains "no backup configuration"
			found := false
			for _, w := range warnings {
				if strings.Contains(w, "no backup configuration") {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue(), "expected a warning about no backup configuration")
		})
	})

	Context("valid configuration produces no warnings", func() {
		It("should return no warnings for a valid resize configuration", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: true,
							Triggers: &apiv1.ResizeTriggers{
								UsageThreshold: ptr.To(80),
							},
							Expansion: &apiv1.ExpansionPolicy{
								Step:  intstr.IntOrString{Type: intstr.String, StrVal: "20%"},
								Limit: "100Gi",
							},
							Strategy: &apiv1.ResizeStrategy{
								MaxActionsPerDay: ptr.To(3),
								WALSafetyPolicy: &apiv1.WALSafetyPolicy{
									AcknowledgeWALRisk:    true,
									RequireArchiveHealthy: ptr.To(false),
								},
							},
						},
					},
				},
			}
			warnings := getAutoResizeWarnings(cluster)
			Expect(warnings).To(BeEmpty())
		})
	})

	Context("disabled resize produces no warnings", func() {
		It("should return no warnings when resize is disabled", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "10Gi",
						Resize: &apiv1.ResizeConfiguration{
							Enabled: false,
							// Even with bad config, disabled should skip warnings
							Expansion: &apiv1.ExpansionPolicy{
								Limit: "1Gi", // Would warn if enabled
							},
						},
					},
				},
			}
			warnings := getAutoResizeWarnings(cluster)
			Expect(warnings).To(BeEmpty())
		})
	})
})
