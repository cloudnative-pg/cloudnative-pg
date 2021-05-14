/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package tests

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1beta1 "k8s.io/api/events/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	// Import the client auth plugin package to allow use gke or ake to run tests
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// TestingEnvironment struct for operator testing
type TestingEnvironment struct {
	RestClientConfig   *rest.Config
	Client             client.Client
	Interface          kubernetes.Interface
	Ctx                context.Context
	Scheme             *runtime.Scheme
	PreserveNamespaces []string
	Log                logr.Logger
}

// NewTestingEnvironment creates the environment for testing
func NewTestingEnvironment() (*TestingEnvironment, error) {
	var env TestingEnvironment
	env.RestClientConfig = ctrl.GetConfigOrDie()
	env.Interface = kubernetes.NewForConfigOrDie(env.RestClientConfig)
	env.Ctx = context.Background()
	env.Scheme = runtime.NewScheme()
	env.Log = ctrl.Log.WithName("e2e")

	var err error
	env.Client, err = client.New(env.RestClientConfig, client.Options{Scheme: env.Scheme})
	if err != nil {
		return nil, err
	}

	if preserveNamespaces := os.Getenv("PRESERVE_NAMESPACES"); preserveNamespaces != "" {
		env.PreserveNamespaces = strings.Fields(preserveNamespaces)
	}

	return &env, nil
}

// CreateNamespace creates a namespace
func (env TestingEnvironment) CreateNamespace(name string, opts ...client.CreateOption) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	return env.Client.Create(env.Ctx, u, opts...)
}

// DeleteNamespace deletes a namespace if existent
func (env TestingEnvironment) DeleteNamespace(name string, opts ...client.DeleteOption) error {
	// Exit immediately if if the namespace is listed in PreserveNamespaces
	for _, v := range env.PreserveNamespaces {
		if v == name {
			return nil
		}
	}

	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	return env.Client.Delete(env.Ctx, u, opts...)
}

// DeletePod deletes a pod if existent
func (env TestingEnvironment) DeletePod(namespace string, name string, opts ...client.DeleteOption) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	})

	return env.Client.Delete(env.Ctx, u, opts...)
}

// ExecCommand wraps the utils.ExecCommand pre-setting values constant during
// tests
func (env TestingEnvironment) ExecCommand(
	ctx context.Context,
	pod corev1.Pod,
	containerName string,
	timeout *time.Duration,
	command ...string) (string, string, error) {
	return utils.ExecCommand(ctx, env.Interface, env.RestClientConfig, pod, containerName, timeout, command...)
}

// GetOperatorDeployment returns the operator deployment if there is a single one running, error otherwise
func (env TestingEnvironment) GetOperatorDeployment() (appsv1.Deployment, error) {
	const operatorDeploymentName = "postgresql-operator-controller-manager"
	deploymentList := &appsv1.DeploymentList{}

	if err := env.Client.List(
		env.Ctx, deploymentList, client.MatchingLabels{"app.kubernetes.io/name": "cloud-native-postgresql"},
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

	// Until we support 4.5 we cannot use the labels to find the operator deployment directly.
	// When 4.5 will be out of games, we can switch to
	//   HasLabels{"operators.coreos.com/cloud-native-postgresql.openshift-operators"}
	if err := env.Client.List(
		env.Ctx,
		deploymentList,
		client.MatchingLabels{"olm.owner.kind": "ClusterServiceVersion"},
	); err != nil {
		return appsv1.Deployment{}, err
	}
	var filteredDeploymentList []appsv1.Deployment
	for _, deployment := range deploymentList.Items {
		owner, ok := deployment.Labels["olm.owner"]
		if !ok {
			continue
		}
		if strings.HasPrefix(owner, "cloud-native-postgresql.") {
			filteredDeploymentList = append(filteredDeploymentList, deployment)
		}
	}
	// We check if we have one or more deployments
	switch {
	case len(filteredDeploymentList) > 1:
		err := fmt.Errorf("number of operator deployments != 1")
		return appsv1.Deployment{}, err
	case len(filteredDeploymentList) == 1:
		return filteredDeploymentList[0], nil
	}

	// This is for deployments created before 1.4.0
	if err := env.Client.List(
		env.Ctx, deploymentList, client.MatchingFields{"metadata.name": operatorDeploymentName},
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
	if err := env.Client.List(
		env.Ctx, podList, client.MatchingLabels{"app.kubernetes.io/name": "cloud-native-postgresql"}); err != nil {
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
	if err := env.Client.List(
		env.Ctx, podList,
		client.MatchingLabels{"control-plane": "controller-manager"},
		client.InNamespace(operatorNamespace)); err != nil {
		return corev1.Pod{}, err
	}
	if len(podList.Items) != 1 {
		err := fmt.Errorf("number of running operator different than 1: %v pods running", len(podList.Items))
		return corev1.Pod{}, err
	}

	return podList.Items[0], nil
}

// GetOperatorNamespaceName returns the namespace the operator deployment is running in
func (env TestingEnvironment) GetOperatorNamespaceName() (string, error) {
	deployment, err := env.GetOperatorDeployment()
	if err != nil {
		return "", err
	}
	return deployment.GetNamespace(), err
}

// GetClusterPodList gathers the current list of pods for a cluster in a namespace
func (env TestingEnvironment) GetClusterPodList(namespace string, clusterName string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := env.Client.List(
		env.Ctx, podList, client.InNamespace(namespace),
		client.MatchingLabels{"postgresql": clusterName},
	)
	return podList, err
}

// GetClusterPrimary gets the primary pod of a cluster
func (env TestingEnvironment) GetClusterPrimary(namespace string, clusterName string) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	err := env.Client.List(
		env.Ctx, podList, client.InNamespace(namespace),
		client.MatchingLabels{"postgresql": clusterName, "role": "primary"},
	)
	return &(podList.Items[0]), err
}

// GetPodList gathers the current list of pods in a namespace
func (env TestingEnvironment) GetPodList(namespace string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := env.Client.List(
		env.Ctx, podList, client.InNamespace(namespace),
	)
	return podList, err
}

// GetPVCList gathers the current list of PVCs in a namespace
func (env TestingEnvironment) GetPVCList(namespace string) (*corev1.PersistentVolumeClaimList, error) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	err := env.Client.List(
		env.Ctx, pvcList, client.InNamespace(namespace),
	)
	return pvcList, err
}

// GetJobList gathers the current list of jobs in a namespace
func (env TestingEnvironment) GetJobList(namespace string) (*batchv1.JobList, error) {
	jobList := &batchv1.JobList{}
	err := env.Client.List(
		env.Ctx, jobList, client.InNamespace(namespace),
	)
	return jobList, err
}

// GetEventList gathers the current list of events in a namespace
func (env TestingEnvironment) GetEventList(namespace string) (*eventsv1beta1.EventList, error) {
	eventList := &eventsv1beta1.EventList{}
	err := env.Client.List(
		env.Ctx, eventList, client.InNamespace(namespace),
	)
	return eventList, err
}

// DumpClusterEnv logs the JSON for the a cluster in a namespace, its pods and endpoints
func (env TestingEnvironment) DumpClusterEnv(namespace string, clusterName string, filename string) {
	f, err := os.Create(filename)
	if err != nil {
		fmt.Println(err)
		return
	}
	w := bufio.NewWriter(f)
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}
	cluster := &apiv1.Cluster{}
	_ = env.Client.Get(env.Ctx, namespacedName, cluster)
	out, _ := json.MarshalIndent(cluster, "", "    ")
	fmt.Fprintf(w, "Dumping %v/%v cluster\n", namespace, clusterName)
	fmt.Fprintln(w, string(out))

	podList, _ := env.GetPodList(namespace)
	for _, pod := range podList.Items {
		out, _ := json.MarshalIndent(pod, "", "    ")
		fmt.Fprintf(w, "Dumping %v/%v pod\n", namespace, pod.Name)
		fmt.Fprintln(w, string(out))
	}

	pvcList, _ := env.GetPVCList(namespace)
	for _, pvc := range pvcList.Items {
		out, _ := json.MarshalIndent(pvc, "", "    ")
		fmt.Fprintf(w, "Dumping %v/%v PVC\n", namespace, pvc.Name)
		fmt.Fprintln(w, string(out))
	}

	jobList, _ := env.GetJobList(namespace)
	for _, job := range jobList.Items {
		out, _ := json.MarshalIndent(job, "", "    ")
		fmt.Fprintf(w, "Dumping %v/%v job\n", namespace, job.Name)
		fmt.Fprintln(w, string(out))
	}

	eventList, _ := env.GetEventList(namespace)
	out, _ = json.MarshalIndent(eventList.Items, "", "    ")
	fmt.Fprintf(w, "Dumping events for namespace %v\n", namespace)
	fmt.Fprintln(w, string(out))

	suffixes := []string{"-r", "-rw", "-any"}
	for _, suffix := range suffixes {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName + suffix,
		}
		endpoint := &corev1.Endpoints{}
		_ = env.Client.Get(env.Ctx, namespacedName, endpoint)
		out, _ := json.MarshalIndent(endpoint, "", "    ")
		fmt.Fprintf(w, "Dumping %v/%v endpoint\n", namespace, endpoint.Name)
		fmt.Fprintln(w, string(out))
	}
	err = w.Flush()
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// GetNodeList gathers the current list of Nodes
func (env TestingEnvironment) GetNodeList() (*corev1.NodeList, error) {
	nodeList := &corev1.NodeList{}
	err := env.Client.List(env.Ctx, nodeList, client.InNamespace(""))
	return nodeList, err
}

// IsGKE returns true if we run on Google Kubernetes Engine. We check that
// by verifying if all the node names start with "gke-"
func (env TestingEnvironment) IsGKE() (bool, error) {
	nodeList := &corev1.NodeList{}
	if err := env.Client.List(env.Ctx, nodeList, client.InNamespace("")); err != nil {
		return false, err
	}
	for _, node := range nodeList.Items {
		re := regexp.MustCompile("^gke-")
		if len(re.FindAllString(node.Name, -1)) == 0 {
			return false, nil
		}
	}
	return true, nil
}

// GetPodLogs gathers pod logs
func (env TestingEnvironment) GetPodLogs(namespace string, podName string) (string, error) {
	req := env.Interface.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	podLogs, err := req.Stream(env.Ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		innerErr := podLogs.Close()
		if err == nil && innerErr != nil {
			err = innerErr
		}
	}()

	// Create a buffer to hold JSON data
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
