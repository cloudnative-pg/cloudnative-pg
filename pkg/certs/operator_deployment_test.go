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

package certs

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func createFakeOperatorDeploymentByName(ctx context.Context,
	kubeClient client.Client,
	deploymentName string,
	labels map[string]string,
) error {
	operatorDep := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: operatorNamespaceName,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{},
	}

	return kubeClient.Create(ctx, &operatorDep)
}

func deleteFakeOperatorDeployment(ctx context.Context,
	kubeClient client.Client,
	deploymentName string,
) error {
	operatorDep := appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: operatorNamespaceName,
		},
		Spec: appsv1.DeploymentSpec{},
	}

	return kubeClient.Delete(ctx, &operatorDep)
}

var _ = Describe("Difference of values of maps", func() {
	It("will always set the app.kubernetes.io/name to cloudnative-pg", func(ctx SpecContext) {
		operatorLabelSelector := "app.kubernetes.io/name=cloudnative-pg"
		operatorLabels := map[string]string{
			"app.kubernetes.io/name": "cloudnative-pg",
		}
		kubeClient := generateFakeClient()
		err := createFakeOperatorDeploymentByName(ctx, kubeClient, operatorDeploymentName, operatorLabels)
		Expect(err).To(BeNil())
		labelMap, err := labels.ConvertSelectorToLabelsMap(operatorLabelSelector)
		Expect(err).To(BeNil())

		deployment, err := findOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).To(BeNil())
		Expect(deployment).ToNot(BeNil())

		err = deleteFakeOperatorDeployment(ctx, kubeClient, operatorDeploymentName)
		Expect(err).To(BeNil())

		operatorLabels = map[string]string{
			"app.kubernetes.io/name": "some-app",
		}
		err = createFakeOperatorDeploymentByName(ctx, kubeClient, "some-app", operatorLabels)
		Expect(err).To(BeNil())
		deployment, err = findOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).ToNot(BeNil())
		Expect(deployment).To(BeNil())

		operatorLabels = map[string]string{
			"app.kubernetes.io/name": "cloudnative-pg",
		}
		err = createFakeOperatorDeploymentByName(ctx, kubeClient, operatorNamespaceName, operatorLabels)
		Expect(err).To(BeNil())
		deployment, err = findOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).To(BeNil())
		Expect(deployment).ToNot(BeNil())
	})
})
