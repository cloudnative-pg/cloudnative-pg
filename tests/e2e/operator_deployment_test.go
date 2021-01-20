/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var postgreSQLOperatorDeploymentName = "postgresql-operator-controller-manager"

var _ = Describe("PostgreSQL operator deployment", func() {
	It("sets up the operator", func() {
		By("having a pod for postgresql-operator-controller-manager in state ready", func() {
			timeout := 120
			Eventually(func() (int, error) {
				podList := &corev1.PodList{}
				err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace))
				return utils.CountReadyPods(podList.Items), err
			}, timeout).Should(BeEquivalentTo(1))
		})
		By("having a deployment for postgresql-operator-controller-manager in state ready", func() {
			namespacedName := types.NamespacedName{
				Namespace: operatorNamespace,
				Name:      postgreSQLOperatorDeploymentName,
			}
			deployment := &appsv1.Deployment{}

			err := env.Client.Get(env.Ctx, namespacedName, deployment)
			Expect(deployment.Status.ReadyReplicas, err).Should(BeEquivalentTo(1))
		})
	})
})
