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

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

// SetInheritedDataAndOwnership sets the cluster as owner of the passed object and then
// sets all the needed annotations and labels
func SetInheritedDataAndOwnership(cluster *apiv1.Cluster, obj *metav1.ObjectMeta) {
	SetInheritedData(cluster, obj)
	utils.SetAsOwnedBy(obj, cluster.ObjectMeta, cluster.TypeMeta)
}

// SetInheritedData sets all the needed annotations and labels
func SetInheritedData(cluster *apiv1.Cluster, obj *metav1.ObjectMeta) {
	utils.InheritAnnotations(obj, cluster.Annotations, cluster.GetFixedInheritedAnnotations(), configuration.Current)
	utils.InheritLabels(obj, cluster.Labels, cluster.GetFixedInheritedLabels(), configuration.Current)
	utils.LabelClusterName(obj, cluster.GetName())
	utils.SetOperatorVersion(obj, versions.Version)
}
