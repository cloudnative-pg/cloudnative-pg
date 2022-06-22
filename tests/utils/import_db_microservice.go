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

package utils

import (
	"fmt"
	"os"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateClusterFromExternalCluster creates a cluster, starting from an external cluster
// using cnp microservice
func CreateClusterFromExternalCluster(namespace, importedClusterName, sourceClusterName,
	databaseName string, env *TestingEnvironment, imageName string,
) error {
	if imageName == "" {
		imageName = os.Getenv("POSTGRES_IMG")
	}
	storageClassName := os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
	host := fmt.Sprintf("cluster-microservice-rw.%v.svc", namespace)
	restoreCluster := &apiv1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name:      importedClusterName,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,
			ImageName: imageName,

			StorageConfiguration: apiv1.StorageConfiguration{
				Size:         "1Gi",
				StorageClass: &storageClassName,
			},

			Bootstrap: &apiv1.BootstrapConfiguration{
				InitDB: &apiv1.BootstrapInitDB{
					Import: &apiv1.LogicalSnapshot{
						Type:      "microservice",
						Databases: []string{databaseName},
						Source: apiv1.LogicalSnapshotSource{
							ExternalCluster: sourceClusterName,
						},
						PostImportApplicationSQL: []string{"SELECT 1"},
					},
				},
			},

			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name:                 sourceClusterName,
					ConnectionParameters: map[string]string{"host": host, "user": "postgres", "dbname": "postgres"},
					Password: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "cluster-microservice-superuser",
						},
						Key: "password",
					},
				},
			},
		},
	}

	return env.Client.Create(env.Ctx, restoreCluster)
}
