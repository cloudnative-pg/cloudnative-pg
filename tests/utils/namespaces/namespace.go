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

// Package namespaces provides utilities to manage namespaces
package namespaces

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/onsi/ginkgo/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	eventsv1 "k8s.io/api/events/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/backups"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"
)

// SternLogDirectory contains the fixed path to store the cluster logs
const SternLogDirectory = "cluster_logs/"

func getPreserveNamespaces() []string {
	var preserveNamespacesList []string
	_, ok := os.LookupEnv("PRESERVE_NAMESPACES")
	if ok {
		preserveNamespacesList = strings.Fields(os.Getenv("PRESERVE_NAMESPACES"))
	}

	return preserveNamespacesList
}

// CleanupClusterLogs cleans up the cluster logs of a given namespace
func CleanupClusterLogs(namespace string, testFailed bool) error {
	exists, _ := fileutils.FileExists(path.Join(SternLogDirectory, namespace))
	if exists && !testFailed {
		if err := fileutils.RemoveDirectory(path.Join(SternLogDirectory, namespace)); err != nil {
			return err
		}
	}

	return nil
}

// cleanupNamespace does cleanup duty related to the tear-down of a namespace,
// and is intended to be called in a DeferCleanup clause
func cleanupNamespace(
	ctx context.Context,
	crudClient client.Client,
	namespace, testName string,
	testFailed bool,
) error {
	if testFailed {
		DumpNamespaceObjects(ctx, crudClient, namespace, "out/"+testName+".log")
	}

	if len(namespace) == 0 {
		return fmt.Errorf("namespace is empty")
	}

	if err := CleanupClusterLogs(namespace, testFailed); err != nil {
		return err
	}

	return deleteNamespace(ctx, crudClient, namespace)
}

// CreateTestNamespace creates a namespace creates a namespace.
// Prefer CreateUniqueTestNamespace instead, unless you need a
// specific namespace name. If so, make sure there is no collision
// potential.
// The namespace is automatically cleaned up at the end of the test.
func CreateTestNamespace(
	ctx context.Context,
	crudClient client.Client,
	name string,
	opts ...client.CreateOption,
) error {
	err := CreateNamespace(ctx, crudClient, name, opts...)
	if err != nil {
		return err
	}

	ginkgo.DeferCleanup(func() error {
		return cleanupNamespace(
			ctx,
			crudClient,
			name,
			ginkgo.CurrentSpecReport().LeafNodeText,
			ginkgo.CurrentSpecReport().Failed(),
		)
	})

	return nil
}

// CreateNamespace creates a namespace.
func CreateNamespace(
	ctx context.Context,
	crudClient client.Client,
	name string,
	opts ...client.CreateOption,
) error {
	// Exit immediately if the name is empty
	if name == "" {
		return errors.New("cannot create namespace with empty name")
	}

	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})
	_, err := objects.Create(ctx, crudClient, u, opts...)
	return err
}

// EnsureNamespace checks for the presence of a namespace, and if it does not
// exist, creates it
func EnsureNamespace(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) error {
	var nsList corev1.NamespaceList
	err := objects.List(ctx, crudClient, &nsList)
	if err != nil {
		return err
	}
	for _, ns := range nsList.Items {
		if ns.Name == namespace {
			return nil
		}
	}
	return CreateNamespace(ctx, crudClient, namespace)
}

// deleteNamespace deletes a namespace if existent
func deleteNamespace(
	ctx context.Context,
	crudClient client.Client,
	name string,
	opts ...client.DeleteOption,
) error {
	// Exit immediately if the name is empty
	if name == "" {
		return errors.New("cannot delete namespace with empty name")
	}

	// Exit immediately if the namespace is listed in PreserveNamespaces
	for _, v := range getPreserveNamespaces() {
		if strings.HasPrefix(name, v) {
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

	return objects.Delete(ctx, crudClient, u, opts...)
}

// DeleteNamespaceAndWait deletes a namespace if existent and returns when deletion is completed
func DeleteNamespaceAndWait(
	ctx context.Context,
	crudClient client.Client,
	name string,
	timeoutSeconds int,
) error {
	// Exit immediately if the namespace is listed in PreserveNamespaces
	for _, v := range getPreserveNamespaces() {
		if strings.HasPrefix(name, v) {
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	err := deleteNamespace(ctx, crudClient, name, client.PropagationPolicy("Background"))
	if err != nil {
		return err
	}

	podList, err := pods.List(ctx, crudClient, name)
	if err != nil {
		return err
	}

	for _, pod := range podList.Items {
		err = pods.Delete(
			ctx, crudClient,
			name, pod.Name,
			client.GracePeriodSeconds(1), client.PropagationPolicy("Background"),
		)
		if err != nil && !apierrs.IsNotFound(err) {
			return err
		}
	}

	return wait.PollUntilContextCancel(ctx, time.Second, true,
		func(ctx context.Context) (bool, error) {
			err := crudClient.Get(ctx, client.ObjectKey{Name: name}, &corev1.Namespace{})
			if apierrs.IsNotFound(err) {
				return true, nil
			}
			return false, err
		},
	)
}

// DumpNamespaceObjects logs the clusters, pods, pvcs etc. found in a namespace as JSON sections
func DumpNamespaceObjects(
	ctx context.Context,
	crudClient client.Client,
	namespace, filename string,
) {
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
	_ = objects.List(ctx, crudClient, clusterList, client.InNamespace(namespace))

	for _, cluster := range clusterList.Items {
		out, _ := json.MarshalIndent(cluster, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v cluster\n", namespace, cluster.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	podList, _ := pods.List(ctx, crudClient, namespace)
	for _, pod := range podList.Items {
		out, _ := json.MarshalIndent(pod, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v pod\n", namespace, pod.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	pvcList, _ := storage.GetPVCList(ctx, crudClient, namespace)
	for _, pvc := range pvcList.Items {
		out, _ := json.MarshalIndent(pvc, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v PVC\n", namespace, pvc.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	jobList := &batchv1.JobList{}
	_ = crudClient.List(
		ctx, jobList, client.InNamespace(namespace),
	)
	for _, job := range jobList.Items {
		out, _ := json.MarshalIndent(job, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v job\n", namespace, job.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	eventList, _ := GetEventList(ctx, crudClient, namespace)
	out, _ := json.MarshalIndent(eventList.Items, "", "    ")
	_, _ = fmt.Fprintf(w, "Dumping events for namespace %v\n", namespace)
	_, _ = fmt.Fprintln(w, string(out))

	serviceAccountList, _ := GetServiceAccountList(ctx, crudClient, namespace)
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
			endpointSlice := &discoveryv1.EndpointSlice{}
			_ = crudClient.Get(ctx, namespacedName, endpointSlice)
			out, _ := json.MarshalIndent(endpointSlice, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v endpointSlice\n", namespace, endpointSlice.Name)
			_, _ = fmt.Fprintln(w, string(out))
		}
	}
	// dump backup info
	backupList, _ := backups.List(ctx, crudClient, namespace)
	// dump backup object info if it's configure
	for _, backup := range backupList.Items {
		out, _ := json.MarshalIndent(backup, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v backup\n", namespace, backup.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}
	// dump scheduledbackup info
	scheduledBackupList, _ := GetScheduledBackupList(ctx, crudClient, namespace)
	// dump backup object info if it's configure
	for _, scheduledBackup := range scheduledBackupList.Items {
		out, _ := json.MarshalIndent(scheduledBackup, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v scheduledbackup\n", namespace, scheduledBackup.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	// dump volumesnapshot info
	volumeSnaphostList, _ := storage.GetSnapshotList(ctx, crudClient, namespace)
	for _, volumeSnapshot := range volumeSnaphostList.Items {
		out, _ := json.MarshalIndent(volumeSnapshot, "", "    ")
		_, _ = fmt.Fprintf(w, "Dumping %v/%v VolumeSnapshot\n", namespace, volumeSnapshot.Name)
		_, _ = fmt.Fprintln(w, string(out))
	}

	err = w.Flush()
	if err != nil {
		fmt.Println(err)
		return
	}
}

// GetServiceAccountList gathers the current list of jobs in a namespace
func GetServiceAccountList(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*corev1.ServiceAccountList, error) {
	serviceAccountList := &corev1.ServiceAccountList{}
	err := crudClient.List(
		ctx, serviceAccountList, client.InNamespace(namespace),
	)
	return serviceAccountList, err
}

// GetEventList gathers the current list of events in a namespace
func GetEventList(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*eventsv1.EventList, error) {
	eventList := &eventsv1.EventList{}
	err := crudClient.List(
		ctx, eventList, client.InNamespace(namespace),
	)
	return eventList, err
}

// GetScheduledBackupList gathers the current list of scheduledBackup in namespace
func GetScheduledBackupList(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*apiv1.ScheduledBackupList, error) {
	scheduledBackupList := &apiv1.ScheduledBackupList{}
	err := crudClient.List(
		ctx, scheduledBackupList, client.InNamespace(namespace),
	)
	return scheduledBackupList, err
}
