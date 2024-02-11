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
	"context"
	"fmt"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/api/v1/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
)

// HibernationMethod will be one of the supported ways to trigger an instance fencing
type HibernationMethod string

const (
	// HibernateDeclaratively it is a keyword to use while fencing on/off the instances using annotation method
	HibernateDeclaratively HibernationMethod = "annotation"
	// HibernateImperatively it is a keyword to use while fencing on/off the instances using plugin method
	HibernateImperatively HibernationMethod = "plugin"
)

// HibernateOn hibernate on a cluster
func HibernateOn(
	env *TestingEnvironment,
	namespace,
	clusterName string,
	method HibernationMethod,
) error {
	switch method {
	case HibernateImperatively:
		_, _, err := Run(fmt.Sprintf("kubectl cnpg hibernate on %v -n %v",
			clusterName, namespace))
		if err != nil {
			return err
		}
		return nil
	case HibernateDeclaratively:
		cluster, err := env.GetCluster(namespace, clusterName)
		if err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		originCluster := cluster.DeepCopy()
		cluster.Annotations[resources.HibernationAnnotationName] = hibernation.HibernationOn

		err = env.Client.Patch(context.Background(), cluster, ctrlclient.MergeFrom(originCluster))
		return err
	default:
		return fmt.Errorf("unknown method: %v", method)
	}
}

// HibernateOff hibernate off a cluster
func HibernateOff(
	env *TestingEnvironment,
	namespace,
	clusterName string,
	method HibernationMethod,
) error {
	switch method {
	case HibernateImperatively:
		_, _, err := Run(fmt.Sprintf("kubectl cnpg hibernate off %v -n %v",
			clusterName, namespace))
		if err != nil {
			return err
		}
		return nil
	case HibernateDeclaratively:
		cluster, err := env.GetCluster(namespace, clusterName)
		if err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		originCluster := cluster.DeepCopy()
		cluster.Annotations[resources.HibernationAnnotationName] = hibernation.HibernationOff

		err = env.Client.Patch(context.Background(), cluster, ctrlclient.MergeFrom(originCluster))
		return err
	default:
		return fmt.Errorf("unknown method: %v", method)
	}
}
