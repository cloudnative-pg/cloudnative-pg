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

package operator

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/avast/retry-go/v5"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/controller"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/deployments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
)

// ReloadDeployment finds and deletes the operator pod. Returns
// error if the new pod is not ready within a defined timeout
func ReloadDeployment(
	ctx context.Context,
	crudClient client.Client,
	timeoutSeconds uint,
) error {
	operatorPod, err := GetPod(ctx, crudClient)
	if err != nil {
		return err
	}

	err = crudClient.Delete(ctx, &operatorPod,
		&client.DeleteOptions{GracePeriodSeconds: ptr.To(int64(1))},
	)
	if err != nil {
		return err
	}
	// Wait for the operator pod to be ready
	return WaitForReady(ctx, crudClient, timeoutSeconds, true)
}

// Dump logs the JSON for the deployment in an operator namespace, its pods and endpoints
func Dump(ctx context.Context, crudClient client.Client, namespace, filename string) {
	f, err := os.Create(filepath.Clean(filename))
	if err != nil {
		fmt.Println(err)
		return
	}
	w := bufio.NewWriter(f)

	deployment, _ := GetDeployment(ctx, crudClient)
	out, _ := json.MarshalIndent(deployment, "", "    ")
	_, _ = fmt.Fprintf(w, "Dumping %v/%v deployment\n", namespace, deployment.Name)
	_, _ = fmt.Fprintln(w, string(out))

	podList, _ := pods.List(ctx, crudClient, namespace)
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

// GetDeployment returns the operator Deployment if there is a single one running, error otherwise
func GetDeployment(ctx context.Context, crudClient client.Client) (appsv1.Deployment, error) {
	deploymentList := &appsv1.DeploymentList{}
	if err := objects.List(ctx, crudClient, deploymentList,
		client.MatchingLabels{"app.kubernetes.io/name": "cloudnative-pg"},
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

	if err := objects.List(
		ctx,
		crudClient,
		deploymentList,
		client.HasLabels{"operators.coreos.com/cloudnative-pg.openshift-operators"},
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

	return deploymentList.Items[0], nil
}

// GetPod returns the operator pod if there is a single one running, error otherwise
func GetPod(ctx context.Context, crudClient client.Client) (corev1.Pod, error) {
	podList := &corev1.PodList{}

	// This will work for newer versions of the operator, which are using
	// our custom label
	if err := objects.List(
		ctx, crudClient,
		podList, client.MatchingLabels{"app.kubernetes.io/name": "cloudnative-pg"}); err != nil {
		return corev1.Pod{}, err
	}
	activePods := utils.FilterActivePods(podList.Items)
	if len(activePods) != 1 {
		err := fmt.Errorf("number of running operator different than 1: %v pods running", len(activePods))
		return corev1.Pod{}, err
	}

	return activePods[0], nil
}

// NamespaceName returns the namespace the operator Deployment is running in
func NamespaceName(ctx context.Context, crudClient client.Client) (string, error) {
	deployment, err := GetDeployment(ctx, crudClient)
	if err != nil {
		return "", err
	}
	return deployment.GetNamespace(), err
}

// IsReady ensures that the operator will be ready.
func IsReady(
	ctx context.Context,
	crudClient client.Client,
	checkWebhook bool,
) (bool, error) {
	if ready, err := isDeploymentReady(ctx, crudClient); err != nil || !ready {
		return ready, err
	}

	// If the operator is not managing webhooks, we don't need to check. Exit early
	if !checkWebhook {
		return true, nil
	}

	deploy, err := GetDeployment(ctx, crudClient)
	if err != nil {
		return false, err
	}
	namespace := deploy.GetNamespace()

	// Detect if we are running under OLM
	var webhookManagedByOLM bool
	for _, envVar := range deploy.Spec.Template.Spec.Containers[0].Env {
		if envVar.Name == "WEBHOOK_CERT_DIR" {
			webhookManagedByOLM = true
		}
	}

	// If the operator is managing certificates for webhooks, check that the setup is completed
	if !webhookManagedByOLM {
		err = checkWebhookSetup(ctx, crudClient, namespace)
		if err != nil {
			return false, err
		}
	}

	return isWebhookWorking(ctx, crudClient)
}

// WaitForReady waits for the operator deployment to be ready.
// If checkWebhook is true, it will also check that the webhook is replying
func WaitForReady(
	ctx context.Context,
	crudClient client.Client,
	timeoutSeconds uint,
	checkWebhook bool,
) error {
	return retry.New(retry.Delay(time.Second),
		retry.Attempts(timeoutSeconds)).
		Do(
			func() error {
				ready, err := IsReady(ctx, crudClient, checkWebhook)
				if err != nil || !ready {
					return fmt.Errorf("operator deployment is not ready")
				}
				return nil
			},
		)
}

// isDeploymentReady returns true if the operator deployment has the expected number
// of ready pods.
// It returns an error if there was a problem getting the operator deployment
func isDeploymentReady(ctx context.Context, crudClient client.Client) (bool, error) {
	operatorDeployment, err := GetDeployment(ctx, crudClient)
	if err != nil {
		return false, err
	}

	return deployments.IsReady(operatorDeployment), nil
}

// ScaleOperatorDeployment will scale the operator to n replicas and return an error in case of failure
func ScaleOperatorDeployment(
	ctx context.Context, crudClient client.Client, replicas int32,
) error {
	operatorDeployment, err := GetDeployment(ctx, crudClient)
	if err != nil {
		return err
	}

	updatedOperatorDeployment := *operatorDeployment.DeepCopy()
	updatedOperatorDeployment.Spec.Replicas = ptr.To(replicas)

	err = crudClient.Patch(ctx, &updatedOperatorDeployment, client.MergeFrom(&operatorDeployment))
	if err != nil {
		return err
	}

	// Wait for the operator deployment to be ready
	return WaitForReady(ctx, crudClient, 120, replicas > 0)
}

// PodRenamed checks if the operator pod was renamed
func PodRenamed(operatorPod corev1.Pod, expectedOperatorPodName string) bool {
	return operatorPod.GetName() != expectedOperatorPodName
}

// PodRestarted checks if the operator pod was restarted
func PodRestarted(operatorPod corev1.Pod) bool {
	restartCount := 0
	for _, containerStatus := range operatorPod.Status.ContainerStatuses {
		if containerStatus.Name == "manager" {
			restartCount = int(containerStatus.RestartCount)
		}
	}
	return restartCount != 0
}

// GetPodName returns the name of the current operator pod
// NOTE: will return an error if the pod is being deleted
func GetPodName(ctx context.Context, crudClient client.Client) (string, error) {
	pod, err := GetPod(ctx, crudClient)
	if err != nil {
		return "", err
	}

	if pod.GetDeletionTimestamp() != nil {
		return "", fmt.Errorf("pod is being deleted")
	}
	return pod.GetName(), nil
}

// HasBeenUpgraded determines if the operator has been upgraded by checking
// if there is a deletion timestamp. If there isn't, it returns true
func HasBeenUpgraded(ctx context.Context, crudClient client.Client) bool {
	_, err := GetPodName(ctx, crudClient)
	return err == nil
}

// Version returns the current operator version
func Version(namespace, podName string) (string, error) {
	out, _, err := run.Unchecked(fmt.Sprintf(
		"kubectl -n %v exec %v -c manager -- /manager version",
		namespace,
		podName,
	))
	if err != nil {
		return "", err
	}
	versionRegexp := regexp.MustCompile(`^Build: {Version:(\d+.*) Commit.*}$`)
	ver := versionRegexp.FindStringSubmatch(strings.TrimSpace(out))[1]
	return ver, nil
}

// Architectures returns all the supported operator architectures
func Architectures(operatorPod *corev1.Pod) ([]string, error) {
	out, _, err := run.Unchecked(fmt.Sprintf(
		"kubectl -n %v exec %v -c manager -- /manager debug show-architectures",
		operatorPod.Namespace,
		operatorPod.Name,
	))
	if err != nil {
		return nil, err
	}

	// `debug show-architectures` will print a JSON object
	var res []string
	err = json.Unmarshal([]byte(out), &res)
	if err != nil {
		return nil, err
	}

	return res, err
}

// GetLeaderInfoFromLease gathers leader holderIdentity from the lease
func GetLeaderInfoFromLease(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	operatorNamespace string,
) (string, error) {
	leaseInterface := kubeInterface.CoordinationV1().Leases(operatorNamespace)
	lease, err := leaseInterface.Get(ctx, controller.LeaderElectionID, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return *lease.Spec.HolderIdentity, nil
}
