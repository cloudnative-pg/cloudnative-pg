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

// Package operator provide functions to handle operator install/uninstall process
package operator

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// CreateConfigMap creates the operator namespace and enables/disable the online upgrade for
// the instance manager
func CreateConfigMap(
	ctx context.Context,
	crudClient client.Client,
	pgOperatorNamespace, configName string,
	isOnline bool,
) {
	By("creating operator namespace", func() {
		// Create a upgradeNamespace for all the resources
		namespacedName := types.NamespacedName{
			Name: pgOperatorNamespace,
		}
		namespaceResource := &corev1.Namespace{}
		err := crudClient.Get(ctx, namespacedName, namespaceResource)
		if apierrors.IsNotFound(err) {
			err = namespaces.CreateNamespace(ctx, crudClient, pgOperatorNamespace)
			Expect(err).ToNot(HaveOccurred())
		} else if err != nil {
			Expect(err).ToNot(HaveOccurred())
		}
	})

	By(fmt.Sprintf("ensuring 'ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES' is set to %v", isOnline), func() {
		enable := "false"
		if isOnline {
			enable = "true"
		}
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pgOperatorNamespace,
				Name:      configName,
			},
			Data: map[string]string{"ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES": enable},
		}
		_, err := objects.Create(ctx, crudClient, configMap)
		Expect(err).NotTo(HaveOccurred())
	})
}

// operatorServiceAccount is the operator's ServiceAccount, used to probe the
// admission path that the operator itself exercises while bootstrapping a cluster.
const operatorServiceAccount = "system:serviceaccount:cnpg-system:cnpg-manager"

// InstallLatest installs an operator version with the most recent release tag
func InstallLatest(
	ctx context.Context,
	crudClient client.Client,
	restConfig *rest.Config,
	releaseTag string,
) {
	mostRecentReleasePath := "../../releases/cnpg-" + releaseTag + ".yaml"

	Eventually(func() error {
		GinkgoWriter.Printf("installing: %s\n", mostRecentReleasePath)

		_, stderr, err := run.Unchecked("kubectl apply --server-side --force-conflicts -f " + mostRecentReleasePath)
		if err != nil {
			GinkgoWriter.Printf("stderr: %s\n", stderr)
		}

		return err
	}, 60).ShouldNot(HaveOccurred())

	Eventually(func() error {
		_, _, err := run.Unchecked(
			"kubectl wait --for condition=established --timeout=60s " +
				"crd/clusters.postgresql.cnpg.io")
		return err
	}, 150).ShouldNot(HaveOccurred())

	Eventually(func() error {
		mapping, err := crudClient.RESTMapper().RESTMapping(
			schema.GroupKind{Group: apiv1.SchemeGroupVersion.Group, Kind: apiv1.ClusterKind},
			apiv1.SchemeGroupVersion.Version)
		if err != nil {
			return err
		}

		GinkgoWriter.Printf("found mapping REST endpoint: %s\n", mapping.GroupVersionKind.String())

		return nil
	}, 150).ShouldNot(HaveOccurred())

	Eventually(func() error {
		_, _, err := run.Unchecked(
			"kubectl wait --for=condition=Available --timeout=2m -n cnpg-system " +
				"deployments cnpg-controller-manager")
		return err
	}, 150).ShouldNot(HaveOccurred())

	// The OwnerReferencesPermissionEnforcement admission plugin (enabled on the
	// test apiservers) resolves the owner GroupVersionKind through the apiserver's
	// own RESTMapper, which is a different cache from the client RESTMapper checked
	// above and refreshes from discovery on an interval (~30s). Until it picks up
	// the freshly installed Cluster CRD, any object created with a blockOwnerDeletion
	// owner reference to a Cluster is rejected with "no matches for kind Cluster",
	// and the operator hits this the moment it creates the bootstrap Job.
	//
	// The check is only enforced for callers that are not cluster-admin, so probe
	// while impersonating the operator's ServiceAccount: a privileged client (like
	// the one used elsewhere in the suite) bypasses the plugin and would not gate
	// on anything.
	impersonatedConfig := rest.CopyConfig(restConfig)
	impersonatedConfig.Impersonate = rest.ImpersonationConfig{UserName: operatorServiceAccount}
	// Use a self-contained scheme: the probe only needs the core ConfigMap type,
	// and relying on the caller's scheme to have registered it would be brittle.
	probeClient, err := client.New(impersonatedConfig, client.Options{Scheme: k8sscheme.Scheme})
	Expect(err).NotTo(HaveOccurred())

	Eventually(func() error {
		probe := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "owner-ref-probe-",
				Namespace:    "cnpg-system",
				OwnerReferences: []metav1.OwnerReference{
					// BlockOwnerDeletion is what the admission plugin enforces; Controller
					// is set only to mirror ctrl.SetControllerReference on the real
					// bootstrap Job.
					{
						APIVersion:         apiv1.SchemeGroupVersion.String(),
						Kind:               apiv1.ClusterKind,
						Name:               "owner-ref-probe",
						UID:                types.UID("00000000-0000-0000-0000-000000000000"),
						BlockOwnerDeletion: ptr.To(true),
						Controller:         ptr.To(true),
					},
				},
			},
		}
		if err := probeClient.Create(ctx, probe); err != nil {
			return err
		}
		// The probe references a Cluster that does not exist, so the garbage
		// collector may delete it before we do; a NotFound here is success.
		return client.IgnoreNotFound(probeClient.Delete(ctx, probe))
	}, 90).ShouldNot(HaveOccurred())
}
