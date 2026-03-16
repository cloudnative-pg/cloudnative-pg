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

package servicespec

import (
	"encoding/json"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// GetLastApplied reads the last-applied service spec from a service's annotations.
// Returns nil if the annotation is absent or cannot be parsed.
func GetLastApplied(annotations map[string]string) *corev1.ServiceSpec {
	raw, ok := annotations[utils.LastAppliedSpecAnnotationName]
	if !ok {
		return nil
	}

	spec := &corev1.ServiceSpec{}
	if err := json.Unmarshal([]byte(raw), spec); err != nil {
		return nil
	}

	return spec
}

// SetLastApplied stores the proposed service spec as a JSON annotation,
// enabling three-way merge on future reconciliations.
func SetLastApplied(object *metav1.ObjectMeta, spec *corev1.ServiceSpec) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		return
	}

	object.Annotations[utils.LastAppliedSpecAnnotationName] = string(raw)
}

// ApplyProposedChanges performs a three-way merge of service specs:
//   - target: copy of the living service spec (base for the merge)
//   - proposed: the desired spec built by the operator
//   - lastApplied: the spec we last applied (nil on first reconciliation)
//
// For each field:
//   - proposed non-zero → apply it (user/operator wants this value)
//   - proposed zero, lastApplied non-zero → intentional removal → clear from target
//   - proposed zero, lastApplied zero (or nil) → never set by us → preserve from target
//   - bool fields are always copied from proposed (Go cannot distinguish unset from false)
func ApplyProposedChanges(target, proposed, lastApplied *corev1.ServiceSpec) {
	// Save original ports before overlay so we can preserve
	// Kubernetes-assigned values (NodePort, Protocol, TargetPort)
	livingPorts := target.Ports

	tv := reflect.ValueOf(target).Elem()
	pv := reflect.ValueOf(proposed).Elem()

	hasLastApplied := lastApplied != nil
	var lv reflect.Value
	if hasLastApplied {
		lv = reflect.ValueOf(lastApplied).Elem()
	}

	st := reflect.TypeOf(proposed).Elem()
	for i := range pv.NumField() {
		// Skip unexported fields to avoid reflect panics
		if !st.Field(i).IsExported() {
			continue
		}

		pf := pv.Field(i)

		// Bool fields are always copied because Go cannot distinguish
		// "not set" (false) from "explicitly set to false"
		if pf.Kind() == reflect.Bool {
			tv.Field(i).Set(pf)
			continue
		}

		if !pf.IsZero() {
			// Proposed has a value → apply it
			tv.Field(i).Set(pf)
		} else if hasLastApplied && !lv.Field(i).IsZero() {
			// Proposed is zero but we previously applied a non-zero value
			// → intentional removal → clear from target
			tv.Field(i).Set(reflect.Zero(tv.Field(i).Type()))
		}
		// else: both proposed and lastApplied are zero → never set by us → preserve living
	}

	// Restore Kubernetes-assigned port defaults for matching ports
	preservePortDefaults(target.Ports, livingPorts)
}

// preservePortDefaults preserves Kubernetes-defaulted and Kubernetes-assigned
// fields in service ports, matching by the strategic merge key (Port, Protocol).
func preservePortDefaults(proposed, living []corev1.ServicePort) {
	type portKey struct {
		port     int32
		protocol corev1.Protocol
	}
	key := func(p corev1.ServicePort) portKey {
		protocol := p.Protocol
		if protocol == "" {
			protocol = corev1.ProtocolTCP
		}
		return portKey{port: p.Port, protocol: protocol}
	}

	livingPorts := make(map[portKey]*corev1.ServicePort, len(living))
	for i := range living {
		livingPorts[key(living[i])] = &living[i]
	}

	for i := range proposed {
		lp, ok := livingPorts[key(proposed[i])]
		if !ok {
			continue
		}
		if proposed[i].Protocol == "" {
			proposed[i].Protocol = lp.Protocol
		}
		if proposed[i].TargetPort == (intstr.IntOrString{}) {
			proposed[i].TargetPort = lp.TargetPort
		}
		if proposed[i].NodePort == 0 {
			proposed[i].NodePort = lp.NodePort
		}
	}
}
