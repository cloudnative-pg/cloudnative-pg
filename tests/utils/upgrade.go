/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

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
		err := env.Client.Create(env.Ctx, configMap)
		Expect(err).NotTo(HaveOccurred())
	})
}

// InstallLatestCNPOperator installs an operator version with the most recent release tag
func InstallLatestCNPOperator(releaseTag string, env *TestingEnvironment) {
	mostRecentReleasePath := "../../releases/postgresql-operator-" + releaseTag + ".yaml"

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
				"crd/clusters.postgresql.k8s.enterprisedb.io")
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
			"kubectl wait --for=condition=Available --timeout=2m -n postgresql-operator-system " +
				"deployments postgresql-operator-controller-manager")
		return err
	}, 150).ShouldNot(HaveOccurred())
}
