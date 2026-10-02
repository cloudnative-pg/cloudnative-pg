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

	jsonpatch "github.com/evanphx/json-patch/v5"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

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

// ApplyProposedChanges computes an RFC 7386 JSON Merge Patch between the
// last-applied spec (from annotations) and proposed, then applies it to target.
// Kubernetes-assigned port defaults (NodePort, Protocol, TargetPort) are
// restored after the merge.
func ApplyProposedChanges(target, proposed *corev1.ServiceSpec, annotations map[string]string) error {
	livingPorts := make([]corev1.ServicePort, len(target.Ports))
	copy(livingPorts, target.Ports)

	proposedJSON, err := json.Marshal(proposed)
	if err != nil {
		return err
	}

	targetJSON, err := json.Marshal(target)
	if err != nil {
		return err
	}

	patchJSON, err := buildPatchJSON(annotations[utils.LastAppliedSpecAnnotationName], proposedJSON)
	if err != nil {
		return err
	}

	mergedJSON, err := jsonpatch.MergePatch(targetJSON, patchJSON)
	if err != nil {
		return err
	}

	var merged corev1.ServiceSpec
	if err := json.Unmarshal(mergedJSON, &merged); err != nil {
		return err
	}
	*target = merged

	// Restore Kubernetes-assigned port defaults (NodePort, Protocol,
	// TargetPort) that are not controlled by the operator.
	preservePortDefaults(target.Ports, livingPorts)

	return nil
}

func buildPatchJSON(lastAppliedJSON string, proposedJSON []byte) ([]byte, error) {
	if lastAppliedJSON == "" || !json.Valid([]byte(lastAppliedJSON)) {
		return proposedJSON, nil
	}

	return jsonpatch.CreateMergePatch([]byte(lastAppliedJSON), proposedJSON)
}

// preservePortDefaults preserves Kubernetes-defaulted and Kubernetes-assigned
// fields in service ports, matching by port number and protocol.
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
