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

package fencing

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ApplyFenceFunc applies a given fencing function to a cluster in a namespace
func ApplyFenceFunc(
	ctx context.Context,
	cli client.Client,
	clusterName string,
	namespace string,
	serverName string,
	fenceFunc func(string, *v1.ObjectMeta) error,
) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: clusterName}, &cluster)
	if err != nil {
		return err
	}

	if serverName != utils.FenceAllServers {
		// Check if the Pod exist
		var pod corev1.Pod
		err = cli.Get(ctx, client.ObjectKey{Namespace: namespace, Name: serverName}, &pod)
		if err != nil {
			return fmt.Errorf("node %s not found in namespace %s", serverName, namespace)
		}
	}

	fencedCluster := cluster.DeepCopy()
	if err = fenceFunc(serverName, &fencedCluster.ObjectMeta); err != nil {
		return err
	}

	err = cli.Patch(ctx, fencedCluster, client.MergeFrom(&cluster))
	if err != nil {
		return err
	}

	return nil
}
