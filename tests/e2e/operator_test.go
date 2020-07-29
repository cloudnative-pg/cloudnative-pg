/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package e2e

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var postgreSQLOperatorDeploymentName = "postgresql-operator-controller-manager"

var _ = Describe("PostgreSQL Operator", func() {
	Context("PostgreSQL Operator Setup", func() {
		It("sets up the operator", func() {
			By("having a pod for postgresql-operator-controller-manager in state ready", func() {
				timeout := 120
				Eventually(func() int {
					podList := &corev1.PodList{}
					if err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace)); err != nil {
						Fail(fmt.Sprintf("Unable to get %v pods", postgreSQLOperatorDeploymentName))
					}
					return utils.CountReadyPods(podList.Items)
				}, timeout).Should(BeEquivalentTo(1))
			})
			By("having a deployment for postgresql-operator-controller-manager in state ready", func() {
				namespacedName := types.NamespacedName{
					Namespace: operatorNamespace,
					Name:      postgreSQLOperatorDeploymentName,
				}
				cr := &appsv1.Deployment{}

				if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
					Fail(fmt.Sprintf("Unable to get %v deployment", postgreSQLOperatorDeploymentName))
				}
				Expect(cr.Status.ReadyReplicas).Should(BeEquivalentTo(1))
			})
		})
	})
})
