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

// Package hibernate provides functions to manage the hibernate feature on a cnpg cluster
package hibernate

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
)

// HibernationMethod will be one of the supported ways to trigger an instance fencing
type HibernationMethod string

const (
	// HibernateDeclaratively it is a keyword to use while fencing on/off the instances using annotation method
	HibernateDeclaratively HibernationMethod = "annotation"
	// HibernateImperatively it is a keyword to use while fencing on/off the instances using plugin method
	HibernateImperatively HibernationMethod = "plugin"
)

// On hibernate on a cluster
func On(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	clusterName string,
	method HibernationMethod,
) error {
	switch method {
	case HibernateImperatively:
		_, _, err := run.Run(fmt.Sprintf("kubectl cnpg hibernate on %v -n %v",
			clusterName, namespace))
		if err != nil {
			return err
		}
		return nil
	case HibernateDeclaratively:
		cluster, err := clusterutils.GetCluster(ctx, crudClient, namespace, clusterName)
		if err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		originCluster := cluster.DeepCopy()
		cluster.Annotations[utils.HibernationAnnotationName] = hibernation.HibernationOn

		err = crudClient.Patch(context.Background(), cluster, client.MergeFrom(originCluster))
		return err
	default:
		return fmt.Errorf("unknown method: %v", method)
	}
}

// Off hibernate off a cluster
func Off(
	ctx context.Context,
	crudClient client.Client,
	namespace,
	clusterName string,
	method HibernationMethod,
) error {
	switch method {
	case HibernateImperatively:
		_, _, err := run.Run(fmt.Sprintf("kubectl cnpg hibernate off %v -n %v",
			clusterName, namespace))
		if err != nil {
			return err
		}
		return nil
	case HibernateDeclaratively:
		cluster, err := clusterutils.GetCluster(ctx, crudClient, namespace, clusterName)
		if err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		originCluster := cluster.DeepCopy()
		cluster.Annotations[utils.HibernationAnnotationName] = hibernation.HibernationOff

		err = crudClient.Patch(context.Background(), cluster, client.MergeFrom(originCluster))
		return err
	default:
		return fmt.Errorf("unknown method: %v", method)
	}
}
