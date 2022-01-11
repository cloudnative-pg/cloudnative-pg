/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1beta1 "k8s.io/api/events/v1beta1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	// Import the client auth plugin package to allow use gke or ake to run tests
	_ "k8s.io/client-go/plugin/pkg/client/auth"
)

// TestingEnvironment struct for operator testing
type TestingEnvironment struct {
	RestClientConfig   *rest.Config
	Client             client.Client
	Interface          kubernetes.Interface
	APIExtensionClient apiextensionsclientset.Interface
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
	env.APIExtensionClient = apiextensionsclientset.NewForConfigOrDie(env.RestClientConfig)
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

// DeleteNamespaceAndWait deletes a namespace if existent and returns when deletion is completed
func (env TestingEnvironment) DeleteNamespaceAndWait(name string, timeoutSeconds int) error {
	// Exit immediately if if the namespace is listed in PreserveNamespaces
	for _, v := range env.PreserveNamespaces {
		if v == name {
			return nil
		}
	}

	_, _, err := Run(fmt.Sprintf("kubectl delete namespace %v --wait=true --timeout %vs", name, timeoutSeconds))

	return err
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

// GetOperatorDeployment returns the operator Deployment if there is a single one running, error otherwise
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

	if err := env.Client.List(
		env.Ctx,
		deploymentList,
		client.HasLabels{"operators.coreos.com/cloud-native-postgresql.openshift-operators"},
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

// GetOperatorNamespaceName returns the namespace the operator Deployment is running in
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
	if err != nil {
		return &corev1.Pod{}, err
	}
	if len(podList.Items) > 0 {
		return &(podList.Items[0]), nil
	}
	err = fmt.Errorf("no primary found")
	return &corev1.Pod{}, err
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

// GetServiceAccountList gathers the current list of jobs in a namespace
func (env TestingEnvironment) GetServiceAccountList(namespace string) (*corev1.ServiceAccountList, error) {
	serviceAccountList := &corev1.ServiceAccountList{}
	err := env.Client.List(
		env.Ctx, serviceAccountList, client.InNamespace(namespace),
	)
	return serviceAccountList, err
}

// GetEventList gathers the current list of events in a namespace
func (env TestingEnvironment) GetEventList(namespace string) (*eventsv1beta1.EventList, error) {
	eventList := &eventsv1beta1.EventList{}
	err := env.Client.List(
		env.Ctx, eventList, client.InNamespace(namespace),
	)
	return eventList, err
}

// GetCluster gets a cluster given name and namespace
func (env TestingEnvironment) GetCluster(namespace string, name string) (*apiv1.Cluster, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	cluster := &apiv1.Cluster{}
	err := env.Client.Get(env.Ctx, namespacedName, cluster)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

// DumpOperator logs the JSON for the deployment in an operator namespace, its pods and endpoints
func (env TestingEnvironment) DumpOperator(namespace string, filename string) {
	f, err := os.Create(filename)
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

// DumpClusterEnv logs the JSON for the a cluster in a namespace, its pods and endpoints
func (env TestingEnvironment) DumpClusterEnv(namespace string, clusterName string, filename string) {
	f, err := os.Create(filename)
	if err != nil {
		fmt.Println(err)
		return
	}
	w := bufio.NewWriter(f)
	cluster, _ := env.GetCluster(namespace, clusterName)

	out, _ := json.MarshalIndent(cluster, "", "    ")
	_, _ = fmt.Fprintf(w, "Dumping %v/%v cluster\n", namespace, clusterName)
	_, _ = fmt.Fprintln(w, string(out))

	podList, _ := env.GetPodList(namespace)
	for _, pod := range podList.Items {
		out, _ := json.MarshalIndent(pod, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v pod\n", namespace, pod.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	pvcList, _ := env.GetPVCList(namespace)
	for _, pvc := range pvcList.Items {
		out, _ := json.MarshalIndent(pvc, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v PVC\n", namespace, pvc.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	jobList, _ := env.GetJobList(namespace)
	for _, job := range jobList.Items {
		out, _ := json.MarshalIndent(job, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v job\n", namespace, job.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	eventList, _ := env.GetEventList(namespace)
	out, _ = json.MarshalIndent(eventList.Items, "", "    ")
	_, _ = fmt.Fprintf(w, "Dumping events for namespace %v\n", namespace)
	_, _ = fmt.Fprintln(w, string(out))

	serviceAccountList, _ := env.GetServiceAccountList(namespace)
	for _, sa := range serviceAccountList.Items {
		out, _ := json.MarshalIndent(sa, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v serviceaccount\n", namespace, sa.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	suffixes := []string{"-r", "-rw", "-any"}
	for _, suffix := range suffixes {
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName + suffix,
		}
		endpoint := &corev1.Endpoints{}
		_ = env.Client.Get(env.Ctx, namespacedName, endpoint)
		out, _ := json.MarshalIndent(endpoint, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v endpoint\n", namespace, endpoint.Name)
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

// IsAKS returns true if we run on Azure Kubernetes Service. We check that
// by verifying if all the node names start with "aks-"
func (env TestingEnvironment) IsAKS() (bool, error) {
	nodeList := &corev1.NodeList{}
	if err := env.Client.List(env.Ctx, nodeList, client.InNamespace("")); err != nil {
		return false, err
	}
	for _, node := range nodeList.Items {
		re := regexp.MustCompile("^aks-")
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
	err = env.Client.Create(env.Ctx, testCluster, &client.CreateOptions{DryRun: []string{metav1.DryRunAll}})
	if err != nil {
		return false, err
	}

	return true, err
}

// GetCNPsMutatingWebhookConf get the MutatingWebhook linked to the operator
func (env TestingEnvironment) GetCNPsMutatingWebhookConf() (
	*admissionregistrationv1.MutatingWebhookConfiguration, error) {
	ctx := context.Background()
	mutatingWebhookConfig, err := env.Interface.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(
		ctx, controller.MutatingWebhookConfigurationName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return mutatingWebhookConfig, nil
}

// GetResourceNamespacedNameFromYAML returns the NamespacedName representing a resource in a YAML file
func (env TestingEnvironment) GetResourceNamespacedNameFromYAML(path string) (types.NamespacedName, error) {
	data, err := ioutil.ReadFile(filepath.Clean(path))
	if err != nil {
		return types.NamespacedName{}, err
	}
	decoder := serializer.NewCodecFactory(env.Scheme).UniversalDeserializer()
	obj, _, err := decoder.Decode(data, nil, nil)
	if err != nil {
		return types.NamespacedName{}, err
	}
	objectMeta, err := meta.Accessor(obj)
	if err != nil {
		return types.NamespacedName{}, err
	}
	return types.NamespacedName{Namespace: objectMeta.GetNamespace(), Name: objectMeta.GetName()}, nil
}

// GetResourceNameFromYAML returns the name of a resource in a YAML file
func (env TestingEnvironment) GetResourceNameFromYAML(path string) (string, error) {
	namespacedName, err := env.GetResourceNamespacedNameFromYAML(path)
	if err != nil {
		return "", err
	}
	return namespacedName.Name, err
}

// GetResourceNamespaceFromYAML returns the namespace of a resource in a YAML file
func (env TestingEnvironment) GetResourceNamespaceFromYAML(path string) (string, error) {
	namespacedName, err := env.GetResourceNamespacedNameFromYAML(path)
	if err != nil {
		return "", err
	}
	return namespacedName.Namespace, err
}

// GetPoolerList gathers the current list of poolers in a namespace
func (env TestingEnvironment) GetPoolerList(namespace string) (*apiv1.PoolerList, error) {
	poolerList := &apiv1.PoolerList{}

	err := env.Client.List(
		env.Ctx, poolerList, client.InNamespace(namespace))

	return poolerList, err
}

// DumpPoolerResourcesInfo logs the JSON for the pooler resources in a namespace, its pods, Deployment,
// services and endpoints
func (env TestingEnvironment) DumpPoolerResourcesInfo(namespace, currentTestName string) {
	poolerList, err := env.GetPoolerList(namespace)
	if err != nil {
		return
	}
	if len(poolerList.Items) > 0 {
		for _, pooler := range poolerList.Items {
			// it will create a filename along with pooler name and currentTest name
			fileName := "out/" + fmt.Sprintf("%v-%v.log", currentTestName, pooler.GetName())
			f, err := os.Create(fileName)
			if err != nil {
				fmt.Println(err)
				return
			}
			w := bufio.NewWriter(f)

			// dump pooler info
			out, _ := json.MarshalIndent(pooler, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v pooler\n", namespace, pooler.Name)
			_, _ = fmt.Fprintln(w, string(out))

			// pooler name used as resources name like Service, Deployment, EndPoints name info
			poolerName := pooler.GetName()
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      poolerName,
			}

			// dump pooler endpoints info
			endpoint := &corev1.Endpoints{}
			_ = env.Client.Get(env.Ctx, namespacedName, endpoint)
			out, _ = json.MarshalIndent(endpoint, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v endpoint\n", namespace, endpoint.Name)
			_, _ = fmt.Fprintln(w, string(out))

			// dump pooler Service info
			service := &corev1.Service{}
			_ = env.Client.Get(env.Ctx, namespacedName, service)
			out, _ = json.MarshalIndent(service, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v Service\n", namespace, service.Name)
			_, _ = fmt.Fprintln(w, string(out))

			// dump pooler pods info
			podList := &corev1.PodList{}
			_ = env.Client.List(env.Ctx, podList, client.InNamespace(namespace),
				client.MatchingLabels{pgbouncer.PgbouncerNameLabel: poolerName})
			for _, pod := range podList.Items {
				out, _ = json.MarshalIndent(pod, "", "    ")
				_, _ = fmt.Fprintf(w, "Dumping %v/%v pod\n", namespace, pod.Name)
				_, _ = fmt.Fprintln(w, string(out))
			}

			// dump Deployment info
			deployment := &appsv1.Deployment{}
			_ = env.Client.Get(env.Ctx, namespacedName, deployment)
			out, _ = json.MarshalIndent(deployment, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v Deployment\n", namespace, deployment.Name)
			_, _ = fmt.Fprintln(w, string(out))
		}
	} else {
		return
	}
}
