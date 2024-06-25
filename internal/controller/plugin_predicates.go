/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

var isPluginService = func(object client.Object) bool {
	if _, hasLabel := object.GetLabels()[utils.PluginNameLabelName]; !hasLabel {
		return false
	}

	if _, hasAnnotation := object.GetAnnotations()[utils.PluginClientSecretAnnotationName]; !hasAnnotation {
		return false
	}

	if _, hasAnnotation := object.GetAnnotations()[utils.PluginServerSecretAnnotationName]; !hasAnnotation {
		return false
	}

	if _, hasAnnotation := object.GetAnnotations()[utils.PluginPortAnnotationName]; !hasAnnotation {
		return false
	}

	return true
}
