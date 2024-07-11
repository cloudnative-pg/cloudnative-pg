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
	"context"
	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/logs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	watchtools "k8s.io/client-go/tools/watch"
	"os"
	"path/filepath"
)

const (
	clusterLogsDirectory = "cluster_logs/"
)

func getClusterLogDirectory(namespace string) string {
	return filepath.Join(clusterLogsDirectory, namespace)
}

func getClusterLogFile(cluster *v1.Cluster) string {
	return filepath.Join(getClusterLogDirectory(cluster.Namespace), cluster.Name, ".logs")
}

func watchClusters(ns *corev1.Namespace, env *TestingEnvironment) {
	mapping, _ := env.Client.RESTMapper().RESTMapping(schema.GroupKind{Group: "postgresql.cnpg.io", Kind: "Cluster"}, "v1")
	res := resource.Info{
		Namespace: ns.Name,
		Mapping:   mapping,
	}
	discovery := dynamic.NewForConfigOrDie(env.RestClientConfig)
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return discovery.Resource(res.Mapping.Resource).Namespace(ns.Name).List(env.Ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return discovery.Resource(res.Mapping.Resource).Namespace(ns.Name).Watch(env.Ctx, options)
		},
	}

	clustersNames := map[string]string{}

	ctx, cancel := context.WithCancel(env.Ctx)
	defer cancel()
	condFuncCluster := func(event watch.Event) (bool, error) {
		u := event.Object.(*unstructured.Unstructured)

		if _, ok := clustersNames[u.GetName()]; ok {
			return true, nil
		}

		cluster, err := env.GetCluster(u.GetNamespace(), u.GetName())
		if err != nil {
			return false, err
		}

		if len(cluster.Status.InstancesReportedState) < 1 ||
			cluster.Status.Phase == "" ||
			cluster.DeletionTimestamp != nil {
			return false, nil
		}

		// We have a cluster up in the cache
		clustersNames[cluster.Name] = cluster.Name
		return true, nil
	}
	condFuncLogs := func(event watch.Event) (bool, error) {
		u := event.Object.(*unstructured.Unstructured)

		cluster, err := env.GetCluster(u.GetNamespace(), u.GetName())
		if err != nil || cluster.DeletionTimestamp != nil {
			return false, nil
		}
		clusterLogs := logs.ClusterLogs{
			Ctx:         ctx,
			ClusterName: cluster.Name,
			Namespace:   cluster.Namespace,
			Client:      env.Interface,
			TailLines:   -1,
			Follow:      true,
		}
		clusterLogDirectory := getClusterLogDirectory(cluster.Namespace)
		if exists, _ := fileutils.FileExists(clusterLogDirectory); exists {
			return true, nil
		}
		_ = fileutils.EnsureDirectoryExists(clusterLogDirectory)
		fileName := getClusterLogFile(cluster)

		go func() {
			var writerOutput *os.File
			if exists, _ := fileutils.FileExists(fileName); exists {
				writerOutput, _ = os.Open(fileName) //nolint:gosec
				clusterLogs.TailLines = 0
			} else {
				writerOutput, _ = os.Create(fileName) //nolint:gosec
			}
			streamClusterLogs := logs.GetStreamClusterLogs(cluster, clusterLogs)
			_ = streamClusterLogs.SingleStream(clusterLogs.Ctx, writerOutput)
			defer func() {
				_ = writerOutput.Sync()
				_ = writerOutput.Close()
			}()
		}()

		return false, nil
	}

	_, _ = watchtools.UntilWithSync(ctx, lw, &unstructured.Unstructured{}, nil, condFuncCluster, condFuncLogs)
}

// WatchNamespaces set a watcher on the namespaces to look for clusters and get the logs from there
func WatchNamespaces(env *TestingEnvironment) {
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return env.Interface.CoreV1().Namespaces().List(env.Ctx, options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			return env.Interface.CoreV1().Namespaces().Watch(env.Ctx, options)
		},
	}

	namespaces := map[string]string{}
	ctx, cancel := context.WithCancel(env.Ctx)
	defer cancel()
	condFunc := func(event watch.Event) (bool, error) {
		obj := event.Object.(*corev1.Namespace)
		if obj.DeletionTimestamp != nil {
			return false, nil
		}
		if _, ok := namespaces[obj.Name]; ok {
			return false, nil
		}

		go watchClusters(obj, env)
		namespaces[obj.Name] = obj.Name

		return false, nil
	}

	_, _ = watchtools.UntilWithSync(ctx, lw, &corev1.Namespace{}, nil, condFunc)
}

// CleanupClusterLogs will delete directory logs if these result on a successful test
func CleanupClusterLogs(testFailed bool, namespace string) {
	if testFailed || namespace == "" {
		return
	}
	_ = fileutils.RemoveDirectory(getClusterLogDirectory(namespace))
}
