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

	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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
	cluster *v1.Cluster,
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
	cluster *v1.Cluster,
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

// DumpNamespaceObjects logs the clusters, pods, pvcs etc. found in a namespace as JSON sections
func (env TestingEnvironment) DumpNamespaceObjects(namespace string, filename string) {
	f, err := os.Create(filepath.Clean(filename))
	if err != nil {
		fmt.Println(err)
		return
	}
	w := bufio.NewWriter(f)
	clusterList := &v1.ClusterList{}
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
	err = w.Flush()
	if err != nil {
		fmt.Println(err)
		return
	}
	_ = f.Sync()
	_ = f.Close()
}

// GetCluster gets a cluster given name and namespace
func (env TestingEnvironment) GetCluster(namespace string, name string) (*v1.Cluster, error) {
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	cluster := &v1.Cluster{}
	err := GetObject(&env, namespacedName, cluster)
	if err != nil {
		return nil, err
	}
	return cluster, nil
}

// GetClusterPodList gathers the current list of pods for a cluster in a namespace
func (env TestingEnvironment) GetClusterPodList(namespace string, clusterName string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := GetObjectList(&env, podList, client.InNamespace(namespace),
		client.MatchingLabels{"postgresql": clusterName},
	)
	return podList, err
}

// GetClusterPrimary gets the primary pod of a cluster
func (env TestingEnvironment) GetClusterPrimary(namespace string, clusterName string) (*corev1.Pod, error) {
	podList := &corev1.PodList{}
	err := GetObjectList(&env, podList, client.InNamespace(namespace),
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
