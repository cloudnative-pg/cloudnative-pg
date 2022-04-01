/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/avast/retry-go/v4"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
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
	const operatorDeploymentName = "postgresql-operator-controller-manager"
	deploymentList := &appsv1.DeploymentList{}
	if err := GetObjectList(&env, deploymentList,
		ctrlclient.MatchingLabels{"app.kubernetes.io/name": "cloud-native-postgresql"},
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
		ctrlclient.HasLabels{"operators.coreos.com/cloud-native-postgresql.openshift-operators"},
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
		&env, podList, ctrlclient.MatchingLabels{"app.kubernetes.io/name": "cloud-native-postgresql"}); err != nil {
		return corev1.Pod{}, err
	}
	switch {
	case len(podList.Items) > 1:
		err := fmt.Errorf("number of running operator pods greater than 1: %v pods running", len(podList.Items))
		return corev1.Pod{}, err

	case len(podList.Items) == 1:
		return podList.Items[0], nil
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
	if len(podList.Items) != 1 {
		err := fmt.Errorf("number of running operator different than 1: %v pods running", len(podList.Items))
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
