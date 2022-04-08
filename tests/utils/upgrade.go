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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2" // nolint
	. "github.com/onsi/gomega"    // nolint
)

// EnableOnlineUpgradeForInstanceManager creates the operator namespace and enables tho online upgrade for
// the instance manager
func EnableOnlineUpgradeForInstanceManager(pgOperatorNamespace, configName string, env *TestingEnvironment) {
	By("creating operator namespace", func() {
		// Create a upgradeNamespace for all the resources
		namespacedName := types.NamespacedName{
			Name: pgOperatorNamespace,
		}
		namespaceResource := &corev1.Namespace{}
		err := env.Client.Get(env.Ctx, namespacedName, namespaceResource)
		if apierrors.IsNotFound(err) {
			err = env.CreateNamespace(pgOperatorNamespace)
			Expect(err).ToNot(HaveOccurred())
		} else if err != nil {
			Expect(err).ToNot(HaveOccurred())
		}
	})

	By("ensuring 'ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES' is set to true", func() {
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pgOperatorNamespace,
				Name:      configName,
			},
			Data: map[string]string{"ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES": "true"},
		}
		err := CreateObject(env, configMap)
		Expect(err).NotTo(HaveOccurred())
	})
}

// InstallLatestCNPGOperator installs an operator version with the most recent release tag
func InstallLatestCNPGOperator(releaseTag string, env *TestingEnvironment) {
	mostRecentReleasePath := "../../releases/cnpg-" + releaseTag + ".yaml"

	Eventually(func() error {
		GinkgoWriter.Printf("installing: %s\n", mostRecentReleasePath)

		_, stderr, err := RunUnchecked("kubectl apply -f " + mostRecentReleasePath)
		if err != nil {
			GinkgoWriter.Printf("stderr: %s\n", stderr)
		}

		return err
	}, 60).ShouldNot(HaveOccurred())

	Eventually(func() error {
		_, _, err := RunUnchecked(
			"kubectl wait --for condition=established --timeout=60s " +
				"crd/clusters.postgresql.cnpg.io")
		return err
	}, 150).ShouldNot(HaveOccurred())

	Eventually(func() error {
		mapping, err := env.Client.RESTMapper().RESTMapping(
			schema.GroupKind{Group: apiv1.GroupVersion.Group, Kind: apiv1.ClusterKind},
			apiv1.GroupVersion.Version)
		if err != nil {
			return err
		}

		GinkgoWriter.Printf("found mapping REST endpoint: %s\n", mapping.GroupVersionKind.String())

		return nil
	}, 150).ShouldNot(HaveOccurred())

	Eventually(func() error {
		_, _, err := RunUnchecked(
			"kubectl wait --for=condition=Available --timeout=2m -n cnpg-system " +
				"deployments cnpg-controller-manager")
		return err
	}, 150).ShouldNot(HaveOccurred())
}
