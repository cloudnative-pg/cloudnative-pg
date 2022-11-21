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

package utils_test

import (
	"context"
	controllerScheme "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	operatorDeploymentName = "cnpg-controller-manager"
	operatorNamespaceName  = "operator-namespace"
)

func createFakeOperatorDeployment(ctx context.Context, kubeClient client.Client, deploymentName string, labels map[string]string) error {
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

func deleteFakeOperatorDeployment(ctx context.Context, kubeClient client.Client, deploymentName string) error {
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

func generateFakeClient() client.Client {
	scheme := controllerScheme.BuildWithAllKnownScheme()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		Build()
}

var _ = Describe("Difference of values of maps", func() {
	It("fuck", func(ctx SpecContext) {
		operatorLabelSelector := "app.kubernetes.io/name=cloudnative-pg"
		operatorLabels := map[string]string{
			"app.kubernetes.io/name": "cloudnative-pg",
		}
		kubeClient := generateFakeClient()
		err := createFakeOperatorDeployment(ctx, kubeClient, operatorDeploymentName, operatorLabels)
		Expect(err).To(BeNil())
		labelMap, err := labels.ConvertSelectorToLabelsMap(operatorLabelSelector)
		Expect(err).To(BeNil())

		deployment, err := utils.FindOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).To(BeNil())
		Expect(deployment).ToNot(BeNil())

		err = deleteFakeOperatorDeployment(ctx, kubeClient, operatorDeploymentName)

		operatorLabels = map[string]string{
			"app.kubernetes.io/name": "some-app",
		}
		err = createFakeOperatorDeployment(ctx, kubeClient, "some-app", operatorLabels)
		Expect(err).To(BeNil())
		deployment, err = utils.FindOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).ToNot(BeNil())
		Expect(deployment).To(BeNil())

		operatorLabels = map[string]string{
			"app.kubernetes.io/name": "cloudnative-pg",
		}
		err = createFakeOperatorDeployment(ctx, kubeClient, operatorNamespaceName, operatorLabels)
		Expect(err).To(BeNil())
		deployment, err = utils.FindOperatorDeploymentByFilter(ctx,
			kubeClient,
			operatorNamespaceName,
			client.MatchingLabelsSelector{Selector: labelMap.AsSelector()})
		Expect(err).To(BeNil())
		Expect(deployment).ToNot(BeNil())
	})
})
