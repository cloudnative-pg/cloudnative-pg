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
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/cheynewallace/tabby"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
)

// AllClusterPodsHaveLabels verifies if the labels defined in a map are included
// in all the pods of a cluster
func AllClusterPodsHaveLabels(
	env *TestingEnvironment,
	namespace, clusterName string,
	labels map[string]string,
) (bool, error) {
	cluster, err := env.GetCluster(namespace, clusterName)
	if err != nil {
		return false, err
	}
	podList, err := env.GetClusterPodList(namespace, clusterName)
	if err != nil {
		return false, err
	}
	if len(podList.Items) != cluster.Spec.Instances {
		return false, fmt.Errorf("%v found instances, %v expected", len(podList.Items), cluster.Spec.Instances)
	}
	for _, pod := range podList.Items {
		if !PodHasLabels(pod, labels) {
			return false, fmt.Errorf("%v found labels, expected %v", pod.Labels, labels)
		}
	}
	return true, nil
}

// AllClusterPodsHaveAnnotations verifies if the annotations defined in a map are included
// in all the pods of a cluster
func AllClusterPodsHaveAnnotations(
	env *TestingEnvironment,
	namespace, clusterName string,
	annotations map[string]string,
) (bool, error) {
	cluster, err := env.GetCluster(namespace, clusterName)
	if err != nil {
		return false, err
	}
	podList, err := env.GetClusterPodList(namespace, clusterName)
	if err != nil {
		return false, err
	}
	if len(podList.Items) != cluster.Spec.Instances {
		return false, fmt.Errorf("%v found instances, %v expected", len(podList.Items), cluster.Spec.Instances)
	}
	for _, pod := range podList.Items {
		if !PodHasAnnotations(pod, annotations) {
			return false, fmt.Errorf("%v found annotations, %v expected", pod.Annotations, annotations)
		}
	}
	return true, nil
}

// ClusterHasLabels verifies that the labels of a cluster contain a specified
// labels map
func ClusterHasLabels(
	cluster *apiv1.Cluster,
	labels map[string]string,
) bool {
	clusterLabels := cluster.Labels
	for k, v := range labels {
		val, ok := clusterLabels[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// ClusterHasAnnotations verifies that the annotations of a cluster contain a specified
// annotations map
func ClusterHasAnnotations(
	cluster *apiv1.Cluster,
	annotations map[string]string,
) bool {
	clusterAnnotations := cluster.Annotations
	for k, v := range annotations {
		val, ok := clusterAnnotations[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// DumpOperatorLogs dumps the operator logs to a file, and returns the log lines
// as a slice.
func (env TestingEnvironment) DumpOperatorLogs(getPrevious bool, requestedLineLength int) ([]string, error) {
	pod, err := env.GetOperatorPod()
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	filename := "out/operator_report_" + pod.Name + ".log"
	if getPrevious {
		filename = "out/operator_report_previous_" + pod.Name + ".log"
	}
	f, err := os.Create(filepath.Clean(filename))
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	defer func() {
		syncErr := f.Sync()
		if err == nil && syncErr != nil {
			err = syncErr
		}
		closeErr := f.Close()
		if err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	_, _ = fmt.Fprintf(f, "Dumping operator pod %v log\n", pod.Name)
	conf := ctrl.GetConfigOrDie()
	client := kubernetes.NewForConfigOrDie(conf)
	return logs.GetPodLogs(env.Ctx, client, pod, getPrevious, f, requestedLineLength)
}

// DumpNamespaceObjects logs the clusters, pods, pvcs etc. found in a namespace as JSON sections
func (env TestingEnvironment) DumpNamespaceObjects(namespace string, filename string) {
	f, err := os.Create(filepath.Clean(filename))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		_ = f.Sync()
		_ = f.Close()
	}()
	w := bufio.NewWriter(f)
	clusterList := &apiv1.ClusterList{}
	_ = GetObjectList(&env, clusterList, client.InNamespace(namespace))

	for _, cluster := range clusterList.Items {
		out, _ := json.MarshalIndent(cluster, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v cluster\n", namespace, cluster.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

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
	out, _ := json.MarshalIndent(eventList.Items, "", "    ")
	_, _ = fmt.Fprintf(w, "Dumping events for namespace %v\n", namespace)
	_, _ = fmt.Fprintln(w, string(out))

	serviceAccountList, _ := env.GetServiceAccountList(namespace)
	for _, sa := range serviceAccountList.Items {
		out, _ := json.MarshalIndent(sa, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v serviceaccount\n", namespace, sa.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	suffixes := []string{"-r", "-rw", "-any"}
	for _, cluster := range clusterList.Items {
		for _, suffix := range suffixes {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      cluster.Name + suffix,
			}
			endpoint := &corev1.Endpoints{}
			_ = env.Client.Get(env.Ctx, namespacedName, endpoint)
			out, _ := json.MarshalIndent(endpoint, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v endpoint\n", namespace, endpoint.Name)
			_, _ = fmt.Fprintln(w, string(out))
		}
	}
	// dump backup info
	backupList, _ := env.GetBackupList(namespace)
	// dump backup object info if it's configure
	for _, backup := range backupList.Items {
		out, _ := json.MarshalIndent(backup, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v backup\n", namespace, backup.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}
	// dump scheduledbackup info
	scheduledBackupList, _ := env.GetScheduledBackupList(namespace)
	// dump backup object info if it's configure
	for _, scheduledBackup := range scheduledBackupList.Items {
		out, _ := json.MarshalIndent(scheduledBackup, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v scheduledbackup\n", namespace, scheduledBackup.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	err = w.Flush()
	if err != nil {
		fmt.Println(err)
		return
	}
}

// GetCluster gets a cluster given name and namespace
func (env TestingEnvironment) GetCluster(namespace string, name string) (*apiv1.Cluster, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	cluster := &apiv1.Cluster{}
	err := GetObject(&env, namespacedName, cluster)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

// GetClusterPodList gathers the current list of instance pods for a cluster in a namespace
func (env TestingEnvironment) GetClusterPodList(namespace string, clusterName string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := GetObjectList(&env, podList, client.InNamespace(namespace),
		client.MatchingLabels{
			utils.ClusterLabelName: clusterName,
			utils.PodRoleLabelName: "instance", // this ensures we are getting instance pods only
		},
	)
	return podList, err
}

// GetClusterPrimary gets the primary pod of a cluster
func (env TestingEnvironment) GetClusterPrimary(namespace string, clusterName string) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	err := GetObjectList(&env, podList, client.InNamespace(namespace),
		client.MatchingLabels{utils.ClusterLabelName: clusterName, "role": "primary"},
	)
	if err != nil {
		return &corev1.Pod{}, err
	}
	if len(podList.Items) > 0 {
		// if there are multiple, get the one without deletion timestamp
		for _, pod := range podList.Items {
			if pod.DeletionTimestamp == nil {
				return &pod, nil
			}
		}
		err = fmt.Errorf("all pod with primary role has deletion timestamp")
		return &(podList.Items[0]), err
	}
	err = fmt.Errorf("no primary found")
	return &corev1.Pod{}, err
}

// GetClusterReplicas gets a slice containing all the replica pods of a cluster
func (env TestingEnvironment) GetClusterReplicas(namespace string, clusterName string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := GetObjectList(&env, podList, client.InNamespace(namespace),
		client.MatchingLabels{utils.ClusterLabelName: clusterName, "role": "replica"},
	)
	if err != nil {
		return podList, err
	}
	if len(podList.Items) > 0 {
		return podList, nil
	}
	err = fmt.Errorf("no replicas found")
	return podList, err
}

// ScaleClusterSize scales a cluster to the requested size
func (env TestingEnvironment) ScaleClusterSize(namespace, clusterName string, newClusterSize int) error {
	cluster, err := env.GetCluster(namespace, clusterName)
	if err != nil {
		return err
	}
	originalCluster := cluster.DeepCopy()
	cluster.Spec.Instances = newClusterSize
	err = env.Client.Patch(env.Ctx, cluster, client.MergeFrom(originalCluster))
	if err != nil {
		return err
	}
	return nil
}

// PrintClusterResources prints a summary of the cluster pods, jobs, pvcs etc.
func PrintClusterResources(namespace, clusterName string, env *TestingEnvironment) string {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      clusterName,
	}
	cluster := &apiv1.Cluster{}
	err := GetObject(env, namespacedName, cluster)
	if err != nil {
		return fmt.Sprintf("Error while Getting Object %v", err)
	}

	buffer := &bytes.Buffer{}
	w := tabwriter.NewWriter(buffer, 0, 0, 4, ' ', 0)
	clusterInfo := tabby.NewCustom(w)
	clusterInfo.AddLine("Timeout while waiting for cluster ready, dumping more cluster information for analysis...")
	clusterInfo.AddLine()
	clusterInfo.AddLine()
	clusterInfo.AddLine("Cluster information:")
	clusterInfo.AddLine("Name", cluster.GetName())
	clusterInfo.AddLine("Namespace", cluster.GetNamespace())
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	clusterInfo.AddLine("Spec.Instances", cluster.Spec.Instances)
	clusterInfo.AddLine("Wal storage", cluster.ShouldCreateWalArchiveVolume())
	clusterInfo.AddLine("Cluster phase", cluster.Status.Phase)
	clusterInfo.AddLine("Phase reason", cluster.Status.PhaseReason)
	clusterInfo.AddLine("Cluster target primary", cluster.Status.TargetPrimary)
	clusterInfo.AddLine("Cluster current primary", cluster.Status.CurrentPrimary)
	clusterInfo.AddLine()

	podList, _ := env.GetClusterPodList(cluster.GetNamespace(), cluster.GetName())

	clusterInfo.AddLine("Cluster Pods information:")
	clusterInfo.AddLine("Ready pod number: ", utils.CountReadyPods(podList.Items))
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	for _, pod := range podList.Items {
		clusterInfo.AddLine("Pod name", pod.Name)
		clusterInfo.AddLine("Pod phase", pod.Status.Phase)
		if cluster.Status.InstancesReportedState != nil {
			if instanceReportState, ok := cluster.Status.InstancesReportedState[apiv1.PodName(pod.Name)]; ok {
				clusterInfo.AddLine("Is Primary", instanceReportState.IsPrimary)
				clusterInfo.AddLine("TimeLineID", instanceReportState.TimeLineID)
				clusterInfo.AddLine("---", "---")
			}
		} else {
			clusterInfo.AddLine("InstanceReportState not reported", "")
		}
	}

	clusterInfo.AddLine("Jobs information:")
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	jobList, _ := env.GetJobList(cluster.GetNamespace())
	for _, job := range jobList.Items {
		clusterInfo.AddLine("Job name", job.Name)
		clusterInfo.AddLine("Job status", fmt.Sprintf("%#v", job.Status))
	}

	pvcList, _ := env.GetPVCList(cluster.GetNamespace())
	clusterInfo.AddLine()
	clusterInfo.AddLine("Cluster PVC information: (dumping all pvc under the namespace)")
	clusterInfo.AddLine("Available PVCCount", cluster.Status.PVCCount)
	clusterInfo.AddLine()
	clusterInfo.AddHeader("Items", "Values")
	for _, pvc := range pvcList.Items {
		clusterInfo.AddLine("PVC name", pvc.Name)
		clusterInfo.AddLine("PVC phase", pvc.Status.Phase)
		clusterInfo.AddLine("---", "---")
	}

	// do not remove, this is needed to ensure that the writer cache is always flushed.
	clusterInfo.Print()

	return buffer.String()
}

// DescribeKubernetesNodes prints the `describe node` for each node in the
// kubernetes cluster
func (env TestingEnvironment) DescribeKubernetesNodes() (string, error) {
	nodeList, err := env.GetNodeList()
	if err != nil {
		return "", err
	}
	var report strings.Builder
	for _, node := range nodeList.Items {
		command := fmt.Sprintf("kubectl describe node %v", node.Name)
		stdout, _, err := Run(command)
		if err != nil {
			return "", err
		}
		report.WriteString("================================================\n")
		report.WriteString(stdout)
		report.WriteString("================================================\n")
	}
	return report.String(), nil
}
