/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/expectations"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// managedResources contains the resources that are created a cluster
// and need to be managed by the controller
type managedResources struct {
	pods corev1.PodList
	pvcs corev1.PersistentVolumeClaimList
	jobs batchv1.JobList
}

// Count the number of jobs that are still running
func (resources managedResources) countRunningJobs() int {
	jobCount := len(resources.jobs.Items)
	completeJobs := utils.CountCompleteJobs(resources.jobs.Items)
	return jobCount - completeJobs
}

// Check if every managed Pod is active and will be schedules
func (resources managedResources) allPodsAreActive() bool {
	for idx := range resources.pods.Items {
		if !utils.IsPodActive(resources.pods.Items[idx]) {
			return false
		}
	}
	return true
}

// getManagedResources get the managed resources of various types
func (r *ClusterReconciler) getManagedResources(ctx context.Context,
	cluster apiv1.Cluster) (*managedResources, error) {
	// Update the status of this resource
	childPods, err := r.getManagedPods(ctx, cluster)
	if err != nil {
		return nil, err
	}

	childPVCs, err := r.getManagedPVCs(ctx, cluster)
	if err != nil {
		return nil, err
	}

	childJobs, err := r.getManagedJobs(ctx, cluster)
	if err != nil {
		return nil, err
	}

	return &managedResources{
		pods: childPods,
		pvcs: childPVCs,
		jobs: childJobs,
	}, nil
}

func (r *ClusterReconciler) getManagedPods(
	ctx context.Context,
	cluster apiv1.Cluster,
) (corev1.PodList, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	var childPods corev1.PodList
	if err := r.List(ctx, &childPods,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{podOwnerKey: cluster.Name},
	); err != nil {
		log.Error(err, "Unable to list child pods resource")
		return corev1.PodList{}, err
	}

	return childPods, nil
}

func (r *ClusterReconciler) getManagedPVCs(
	ctx context.Context,
	cluster apiv1.Cluster,
) (corev1.PersistentVolumeClaimList, error) {
	log := r.Log.WithValues("namespace", cluster.Namespace, "name", cluster.Name)

	var childPVCs corev1.PersistentVolumeClaimList
	if err := r.List(ctx, &childPVCs,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{pvcOwnerKey: cluster.Name},
	); err != nil {
		log.Error(err, "Unable to list child PVCs")
		return corev1.PersistentVolumeClaimList{}, err
	}

	return childPVCs, nil
}

// getManagedJobs extract the list of jobs which are being created
// by this cluster
func (r *ClusterReconciler) getManagedJobs(
	ctx context.Context,
	cluster apiv1.Cluster,
) (batchv1.JobList, error) {
	var childJobs batchv1.JobList
	if err := r.List(ctx, &childJobs,
		client.InNamespace(cluster.Namespace),
		client.MatchingFields{jobOwnerKey: cluster.Name},
	); err != nil {
		return batchv1.JobList{}, err
	}

	return childJobs, nil
}

func (r *ClusterReconciler) updateResourceStatus(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
) error {
	// Retrieve the cluster key
	key := expectations.KeyFunc(cluster)

	existingClusterStatus := cluster.Status

	// Fill the list of dangling PVCs
	oldPVCCount := cluster.Status.PVCCount
	newPVCCount := int32(len(resources.pvcs.Items))
	cluster.Status.PVCCount = newPVCCount
	pvcClassification := specs.DetectPVCs(resources.pods.Items, resources.jobs.Items, resources.pvcs.Items)
	cluster.Status.DanglingPVC = pvcClassification.Dangling
	cluster.Status.InitializingPVC = pvcClassification.Initializing

	// Update the pvcExpectations for the cluster
	r.pvcExpectations.LowerExpectationsDelta(key, int(newPVCCount-oldPVCCount))

	// From now on, we'll consider only Active pods: those Pods
	// that will possibly work. Let's forget about the failed ones
	filteredPods := utils.FilterActivePods(resources.pods.Items)

	// Count pods
	oldInstances := cluster.Status.Instances
	newInstances := int32(len(filteredPods))
	cluster.Status.Instances = newInstances
	cluster.Status.ReadyInstances = int32(utils.CountReadyPods(filteredPods))

	// Update the podExpectations for the cluster
	r.podExpectations.LowerExpectationsDelta(key, int(newInstances-oldInstances))

	// Count jobs
	oldJobs := cluster.Status.JobCount
	newJobs := int32(len(resources.jobs.Items))
	cluster.Status.JobCount = newJobs

	// Update the jobExpectations for the cluster
	r.jobExpectations.LowerExpectationsDelta(key, int(newJobs-oldJobs))

	// Instances status
	cluster.Status.InstancesStatus = utils.ListStatusPods(resources.pods.Items)

	// Services
	cluster.Status.WriteService = cluster.GetServiceReadWriteName()
	cluster.Status.ReadService = cluster.GetServiceReadName()

	// If we are switching, check if the target primary is still active
	if cluster.Status.TargetPrimary != cluster.Status.CurrentPrimary &&
		cluster.Status.ReadyInstances > 0 {
		found := false
		for _, pod := range utils.FilterActivePods(resources.pods.Items) {
			// If the target primary is not active, it will never be promoted
			// since is will not be scheduled anymore
			if pod.Name == cluster.Status.TargetPrimary {
				found = true
				break
			}
		}

		if !found {
			// Reset the target primary, since the available one is not active
			// or not present
			r.Log.Info("Wrong target primary, the chosen one is not active or not present",
				"targetPrimary", cluster.Status.TargetPrimary,
				"pods", resources.pods)
			cluster.Status.TargetPrimary = cluster.Status.CurrentPrimary
		}
	}

	if err := r.refreshSecretResourceVersions(ctx, cluster); err != nil {
		return err
	}

	if !reflect.DeepEqual(existingClusterStatus, cluster.Status) {
		return r.Status().Update(ctx, cluster)
	}
	return nil
}

// refreshSecretResourceVersions set the resource version of the secrets
func (r *ClusterReconciler) refreshSecretResourceVersions(ctx context.Context, cluster *apiv1.Cluster) error {
	version, err := r.getSecretResourceVersion(ctx, cluster, cluster.GetSuperuserSecretName())
	if err != nil {
		return err
	}
	cluster.Status.SecretsResourceVersion.SuperuserSecretVersion = version

	version, err = r.getSecretResourceVersion(ctx, cluster, cluster.GetReplicationSecretName())
	if err != nil {
		return err
	}
	cluster.Status.SecretsResourceVersion.ReplicationSecretVersion = version

	version, err = r.getSecretResourceVersion(ctx, cluster, cluster.GetApplicationSecretName())
	if err != nil {
		return err
	}
	cluster.Status.SecretsResourceVersion.ApplicationSecretVersion = version

	version, err = r.getSecretResourceVersion(ctx, cluster, cluster.GetCASecretName())
	if err != nil {
		return err
	}
	cluster.Status.SecretsResourceVersion.CASecretVersion = version

	version, err = r.getSecretResourceVersion(ctx, cluster, cluster.GetServerSecretName())
	if err != nil {
		return err
	}
	cluster.Status.SecretsResourceVersion.ServerSecretVersion = version

	return nil
}

// getSecretResourceVersion retrieves the resource version of a secret
func (r *ClusterReconciler) getSecretResourceVersion(
	ctx context.Context,
	cluster *apiv1.Cluster,
	name string,
) (string, error) {
	secret := corev1.Secret{}
	err := r.Get(
		ctx,
		client.ObjectKey{Namespace: cluster.Namespace, Name: name},
		&secret)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	return secret.ResourceVersion, nil
}

func (r *ClusterReconciler) setPrimaryInstance(
	ctx context.Context,
	cluster *apiv1.Cluster,
	podName string,
) error {
	cluster.Status.TargetPrimary = podName
	return r.Status().Update(ctx, cluster)
}

// RegisterPhase update phase in the status cluster with the
// proper reason
func (r *ClusterReconciler) RegisterPhase(ctx context.Context,
	cluster *apiv1.Cluster,
	phase string,
	reason string,
) error {
	existingClusterStatus := cluster.Status

	cluster.Status.Phase = phase
	cluster.Status.PhaseReason = reason

	if !reflect.DeepEqual(existingClusterStatus, cluster.Status) {
		if err := r.Status().Update(ctx, cluster); err != nil {
			return err
		}
	}

	return nil
}

// ExtractInstancesStatus extracts the status of the underlying PostgreSQL instance from
// the requested Pod, via the instance manager. In case of failure, errors are passed
// in the result list
func ExtractInstancesStatus(
	ctx context.Context,
	filteredPods []corev1.Pod,
) postgres.PostgresqlStatusList {
	var result postgres.PostgresqlStatusList

	for idx := range filteredPods {
		instanceStatus := getReplicaStatusFromPodViaHTTP(ctx, filteredPods[idx])
		instanceStatus.IsReady = utils.IsPodReady(filteredPods[idx])
		result.Items = append(result.Items, instanceStatus)
	}
	return result
}

// getReplicaStatusFromPodViaHTTP retrieves the status of PostgreSQL pods via an HTTP request with GET method.
func getReplicaStatusFromPodViaHTTP(ctx context.Context, pod corev1.Pod) postgres.PostgresqlStatus {
	var result postgres.PostgresqlStatus

	statusURL := url.Build(pod.Status.PodIP, url.PathPgStatus)
	req, err := http.NewRequestWithContext(ctx, "GET", statusURL, nil)
	if err != nil {
		result.PodName = pod.Name
		result.ExecError = err
		return result
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		result.PodName = pod.Name
		result.ExecError = err
		return result
	}

	if resp.StatusCode != 200 {
		bytes, _ := ioutil.ReadAll(resp.Body)
		result.PodName = pod.Name
		result.ExecError = fmt.Errorf("%v - %v", resp.StatusCode, string(bytes))
		_ = resp.Body.Close()
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		result.PodName = pod.Name
		result.ExecError = err
		return result
	}

	err = json.Unmarshal(body, &result)
	if err != nil {
		result.PodName = pod.Name
		result.ExecError = err
		return result
	}

	err = resp.Body.Close()
	if err != nil {
		result.PodName = pod.Name
		result.ExecError = err
		return result
	}

	return result
}
