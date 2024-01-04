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
	"strings"

	"github.com/blang/semver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// GetSubscription returns an unstructured subscription object
func GetSubscription(env *TestingEnvironment) (*unstructured.Unstructured, error) {
	subscription := &unstructured.Unstructured{}
	subscription.SetName("cloudnative-pg")
	subscription.SetNamespace("openshift-operators")
	subscription.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "Subscription",
	})
	err := env.Client.Get(env.Ctx, ctrlclient.ObjectKeyFromObject(subscription), subscription)
	return subscription, err
}

// GetSubscriptionVersion retrieves the current ClusterServiceVersion version of the operator
func GetSubscriptionVersion(env *TestingEnvironment) (string, error) {
	subscription, err := GetSubscription(env)
	if err != nil {
		return "", err
	}
	version, found, err := unstructured.NestedString(subscription.Object, "status", "currentCSV")
	if !found {
		return "", fmt.Errorf("currentCSV not found")
	}
	if err != nil {
		return "", err
	}
	ver := strings.TrimPrefix(version, "cloudnative-pg.v")
	return ver, nil
}

// PatchStatusCondition Removes status conditions on a given Cluster
func PatchStatusCondition(namespace, clusterName string, env *TestingEnvironment) error {
	cluster := &apiv1.Cluster{}
	var err error
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cluster, err = env.GetCluster(namespace, clusterName)
		if err != nil {
			return err
		}
		clusterNoConditions := cluster.DeepCopy()
		clusterNoConditions.Status.Conditions = nil
		return env.Client.Patch(env.Ctx, clusterNoConditions, ctrlclient.MergeFrom(cluster))
	})
	if err != nil {
		return err
	}
	return nil
}

// GetOpenshiftVersion returns the current OCP version taken from env variables
func GetOpenshiftVersion(env *TestingEnvironment) (semver.Version, error) {
	client, err := dynamic.NewForConfig(env.RestClientConfig)
	if err != nil {
		return semver.Version{}, err
	}

	clusterController, err := client.Resource(schema.GroupVersionResource{
		Group:    "operator.openshift.io",
		Version:  "v1",
		Resource: "openshiftcontrollermanagers",
	}).Get(env.Ctx, "cluster", v1.GetOptions{})
	if err != nil {
		return semver.Version{}, err
	}

	version, found, err := unstructured.NestedString(clusterController.Object, "status", "version")
	if !found || err != nil {
		return semver.Version{}, err
	}

	return semver.Make(version)
}

// CreateSubscription creates a subscription object inside openshift with a fixed name
func CreateSubscription(env *TestingEnvironment, channel string) error {
	u := &unstructured.Unstructured{}
	u.SetName("cloudnative-pg")
	u.SetNamespace("openshift-operators")
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "Subscription",
	})

	spec := map[string]string{
		"channel":             channel,
		"installPlanApproval": "Automatic",
		"name":                "cloudnative-pg",
		"source":              "cloudnative-pg-manifests",
		"sourceNamespace":     "openshift-marketplace",
	}

	err := unstructured.SetNestedStringMap(u.Object, spec, "spec")
	if err != nil {
		return err
	}

	_, err = CreateObject(env, u)
	return err
}

// DeleteSubscription deletes the cloud-native-postgresql subscription
func DeleteSubscription(env *TestingEnvironment) error {
	u := &unstructured.Unstructured{}
	u.SetName("cloudnative-pg")
	u.SetNamespace("openshift-operators")
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "Subscription",
	})

	err := DeleteObject(env, u)
	if apierrors.IsNotFound(err) {
		return nil
	}

	return err
}

// DeleteCNPCRDs deletes the CRD's associated with cloud-native-postgresql
func DeleteCNPCRDs(env *TestingEnvironment) error {
	u := &unstructured.Unstructured{}
	u.SetName("clusters.postgresql.cnpg.io")
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "apiextensions.k8s.io",
		Version: "v1",
		Kind:    "CustomResourceDefinition",
	})
	err := DeleteObject(env, u)
	if !apierrors.IsNotFound(err) {
		return err
	}
	u.SetName("backups.postgresql.cnpg.io")
	err = DeleteObject(env, u)
	if !apierrors.IsNotFound(err) {
		return err
	}
	u.SetName("poolers.postgresql.cnpg.io")
	err = DeleteObject(env, u)
	if !apierrors.IsNotFound(err) {
		return err
	}
	u.SetName("scheduledbackups.postgresql.cnpg.io")
	err = DeleteObject(env, u)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// DeleteCSV will delete all cloud-native-postgresql CSVs
func DeleteCSV(env *TestingEnvironment) error {
	ol := &unstructured.UnstructuredList{}
	ol.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "operators.coreos.com",
		Version: "v1alpha1",
		Kind:    "ClusterServiceVersion",
	})
	labelSelector := labels.SelectorFromSet(map[string]string{
		"operators.coreos.com/cloudnative-pg.openshift-operators": "",
	})
	err := GetObjectList(env, ol, ctrlclient.MatchingLabelsSelector{Selector: labelSelector})
	if err != nil {
		return err
	}
	for _, o := range ol.Items {
		o := o
		err = DeleteObject(env, &o)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
	}
	return err
}

// UpgradeSubscription patch an unstructured subscription object with target channel
func UpgradeSubscription(env *TestingEnvironment, channel string) error {
	subscription, err := GetSubscription(env)
	if err != nil {
		return err
	}

	newSubscription := subscription.DeepCopy()
	err = unstructured.SetNestedField(newSubscription.Object, channel, "spec", "channel")
	if err != nil {
		return err
	}

	return env.Client.Patch(env.Ctx, newSubscription, ctrlclient.MergeFrom(subscription))
}
