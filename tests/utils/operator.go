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
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/avast/retry-go/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ReloadOperatorDeployment finds and deletes the operator pod. Returns
// error if the new pod is not ready within a defined timeout
func ReloadOperatorDeployment(env *TestingEnvironment, timeoutSeconds uint) error {
	operatorPod, err := env.GetOperatorPod()
	if err != nil {
		return err
	}
	zero := int64(0)
	err = env.Client.Delete(env.Ctx, &operatorPod,
		&ctrlclient.DeleteOptions{GracePeriodSeconds: &zero},
	)
	if err != nil {
		return err
	}
	err = retry.Do(
		func() error {
			ready, err := env.IsOperatorReady()
			if err != nil {
				return err
			}
			if !ready {
				return fmt.Errorf("operator pod %v is not ready", operatorPod.Name)
			}
			return nil
		},
		retry.Delay(time.Second),
		retry.Attempts(timeoutSeconds),
	)
	return err
}

// DumpOperator logs the JSON for the deployment in an operator namespace, its pods and endpoints
func (env TestingEnvironment) DumpOperator(namespace string, filename string) {
	f, err := os.Create(filepath.Clean(filename))
	if err != nil {
		fmt.Println(err)
		return
	}
	w := bufio.NewWriter(f)

	deployment, _ := env.GetOperatorDeployment()
	out, _ := json.MarshalIndent(deployment, "", "    ")
	_, _ = fmt.Fprintf(w, "Dumping %v/%v deployment\n", namespace, deployment.Name)
	_, _ = fmt.Fprintln(w, string(out))

	podList, _ := env.GetPodList(namespace)
	for _, pod := range podList.Items {
		out, _ := json.MarshalIndent(pod, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v pod\n", namespace, pod.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	err = w.Flush()
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// GetOperatorDeployment returns the operator Deployment if there is a single one running, error otherwise
func (env TestingEnvironment) GetOperatorDeployment() (appsv1.Deployment, error) {
	const operatorDeploymentName = "cnpg-controller-manager"
	deploymentList := &appsv1.DeploymentList{}
	if err := GetObjectList(&env, deploymentList,
		ctrlclient.MatchingLabels{"app.kubernetes.io/name": "cloudnative-pg"},
	); err != nil {
		return appsv1.Deployment{}, err
	}
	// We check if we have one or more deployments
	switch {
	case len(deploymentList.Items) > 1:
		err := fmt.Errorf("number of operator deployments != 1")
		return appsv1.Deployment{}, err
	case len(deploymentList.Items) == 1:
		return deploymentList.Items[0], nil
	}

	if err := GetObjectList(
		&env,
		deploymentList,
		ctrlclient.HasLabels{"operators.coreos.com/cloudnative-pg.openshift-operators"},
	); err != nil {
		return appsv1.Deployment{}, err
	}

	// We check if we have one or more deployments
	switch {
	case len(deploymentList.Items) > 1:
		err := fmt.Errorf("number of operator deployments != 1")
		return appsv1.Deployment{}, err
	case len(deploymentList.Items) == 1:
		return deploymentList.Items[0], nil
	}

	// This is for deployments created before 1.4.0

	if err := GetObjectList(
		&env, deploymentList, ctrlclient.MatchingFields{"metadata.name": operatorDeploymentName},
	); err != nil {
		return appsv1.Deployment{}, err
	}

	if len(deploymentList.Items) != 1 {
		err := fmt.Errorf("number of %v deployments != 1", operatorDeploymentName)
		return appsv1.Deployment{}, err
	}
	return deploymentList.Items[0], nil
}

// GetOperatorPod returns the operator pod if there is a single one running, error otherwise
func (env TestingEnvironment) GetOperatorPod() (corev1.Pod, error) {
	podList := &corev1.PodList{}

	// This will work for newer version of the operator, which are using
	// our custom label
	if err := GetObjectList(
		&env, podList, ctrlclient.MatchingLabels{"app.kubernetes.io/name": "cloudnative-pg"}); err != nil {
		return corev1.Pod{}, err
	}
	activePods := utils.FilterActivePods(podList.Items)
	switch {
	case len(activePods) > 1:
		err := fmt.Errorf("number of running operator pods greater than 1: %v pods running", len(activePods))
		return corev1.Pod{}, err

	case len(activePods) == 1:
		return activePods[0], nil
	}

	operatorNamespace, err := env.GetOperatorNamespaceName()
	if err != nil {
		return corev1.Pod{}, err
	}

	// This will work for older version of the operator, which are using
	// the default label from kube-builder
	if err := GetObjectList(
		&env, podList,
		ctrlclient.MatchingLabels{"control-plane": "controller-manager"},
		ctrlclient.InNamespace(operatorNamespace)); err != nil {
		return corev1.Pod{}, err
	}
	activePods = utils.FilterActivePods(podList.Items)
	if len(activePods) != 1 {
		err := fmt.Errorf("number of running operator different than 1: %v pods running", len(activePods))
		return corev1.Pod{}, err
	}

	return podList.Items[0], nil
}

// GetOperatorNamespaceName returns the namespace the operator Deployment is running in
func (env TestingEnvironment) GetOperatorNamespaceName() (string, error) {
	deployment, err := env.GetOperatorDeployment()
	if err != nil {
		return "", err
	}
	return deployment.GetNamespace(), err
}

// IsOperatorReady ensures that the operator will be ready.
func (env TestingEnvironment) IsOperatorReady() (bool, error) {
	pod, err := env.GetOperatorPod()
	if err != nil {
		return false, err
	}

	isPodReady := utils.IsPodReady(pod)
	if !isPodReady {
		return false, err
	}

	namespace := pod.Namespace

	// Detect if we are running under OLM
	var webhookManagedByOLM bool
	for _, envVar := range pod.Spec.Containers[0].Env {
		if envVar.Name == "WEBHOOK_CERT_DIR" {
			webhookManagedByOLM = true
		}
	}

	// If the operator is managing certificates for webhooks, check that the setup is completed
	if !webhookManagedByOLM {
		err = CheckWebhookReady(&env, namespace)
		if err != nil {
			return false, err
		}
	}

	// Dry run object creation to check that webhook Service is correctly running
	testCluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "readiness-check-" + rand.String(5),
			Namespace: "default",
		},
		Spec: apiv1.ClusterSpec{
			Instances: 3,
			StorageConfiguration: apiv1.StorageConfiguration{
				Size: "1Gi",
			},
		},
	}
	err = CreateObject(&env, testCluster, &ctrlclient.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return false, err
	}

	return true, err
}

// OperatorPodRenamed checks if the operator pod was renamed
func OperatorPodRenamed(operatorPod corev1.Pod, expectedOperatorPodName string) bool {
	return operatorPod.GetName() != expectedOperatorPodName
}

// OperatorPodRestarted checks if the operator pod was restarted
func OperatorPodRestarted(operatorPod corev1.Pod) bool {
	restartCount := 0
	for _, containerStatus := range operatorPod.Status.ContainerStatuses {
		if containerStatus.Name == "manager" {
			restartCount = int(containerStatus.RestartCount)
		}
	}
	return restartCount != 0
}
