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

package resources

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// mergeMap transfers the content of a giver map to a receiver
func mergeMap(receiver, giver map[string]string) {
	for key, value := range giver {
		receiver[key] = value
	}
}

// inheritLabels puts into the object metadata the passed labels
func inheritLabels(
	object *metav1.ObjectMeta,
	labels map[string]string,
) {
	if object.Labels == nil {
		object.Labels = make(map[string]string)
	}

	mergeMap(object.Labels, labels)
}

// inheritAnnotations puts into the object metadata the passed annotations
func inheritAnnotations(
	object *metav1.ObjectMeta,
	annotations map[string]string,
) {
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}

	mergeMap(object.Annotations, annotations)
}

func setHash(meta *metav1.ObjectMeta, hashValue string) {
	meta.Annotations[utils.CNPGHashAnnotationName] = hashValue
}
