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

package status

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cheynewallace/tabby"
	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"github.com/cloudnative-pg/machinery/pkg/types"
	"github.com/logrusorgru/aurora/v4"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/plugin/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PostgresqlStatus contains the status of the Cluster and of all its instances
type PostgresqlStatus struct {
	// Cluster is the Cluster we are investigating
	Cluster *apiv1.Cluster `json:"cluster"`

	// InstanceStatus is the status of each instance, extracted directly
	// from the instance manager running into each Pod
	InstanceStatus *postgres.PostgresqlStatusList `json:"instanceStatus"`

	// PrimaryPod contains the primary Pod
	PrimaryPod corev1.Pod

	// PodDisruptionBudgetList prints every PDB that matches against the cluster
	// with the label selector
	PodDisruptionBudgetList policyv1.PodDisruptionBudgetList

	// ErrorList store the possible errors while getting the PostgreSQL status
	ErrorList []error

	// The size of the cluster
	TotalClusterSize string
}

func (fullStatus *PostgresqlStatus) getReplicationSlotList() postgres.PgReplicationSlotList {
	primary := fullStatus.tryGetPrimaryInstance()
	if primary == nil {
		return nil
	}

	return primary.ReplicationSlotsInfo
}

func (fullStatus *PostgresqlStatus) getPrintableReplicationSlotInfo(instanceName string) *postgres.PgReplicationSlot {
	for _, slot := range fullStatus.getReplicationSlotList() {
		expectedSlotName := fullStatus.Cluster.GetSlotNameFromInstanceName(instanceName)
		if slot.SlotName == expectedSlotName {
			return &slot
		}
	}

	return nil
}

func getPrintableIntegerPointer(i *int) string {
	if i == nil {
		return "NULL"
	}
	return strconv.Itoa(*i)
}

// Status implements the "status" subcommand
func Status(
	ctx context.Context,
	clusterName string,
	verbosity int,
	format plugin.OutputFormat,
	timeout time.Duration,
) error {
	var cluster apiv1.Cluster
	var errs []error

	// Create a Kubernetes client suitable for calling the "Exec" subresource
	clientInterface := kubernetes.NewForConfigOrDie(plugin.Config)

	// Get the Cluster object
	err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName}, &cluster)
	if err != nil {
		return fmt.Errorf("while trying to get cluster %s in namespace %s: %w",
			clusterName, plugin.Namespace, err)
	}

	status := extractPostgresqlStatus(ctx, cluster)
	hibernated, _ := isHibernated(status)

	err = plugin.Print(status, format, os.Stdout)
	if err != nil || format != plugin.OutputFormatText {
		return err
	}
	errs = append(errs, status.ErrorList...)

	status.printBasicInfo(ctx, clientInterface, timeout)
	status.printHibernationInfo()
	status.printDemotionTokenInfo()
	status.printPromotionTokenInfo()
	if verbosity > 1 {
		errs = append(errs, status.printPostgresConfiguration(ctx, clientInterface, timeout)...)
		status.printCertificatesStatus()
	}
	if !hibernated {
		status.printBackupStatus()
		status.printBasebackupStatus(verbosity)
		status.printReplicaStatus(verbosity)
		if verbosity > 0 {
			status.printUnmanagedReplicationSlotStatus()
			status.printRoleManagerStatus()
			status.printTablespacesStatus()
			status.printPodDisruptionBudgetStatus()
		}
		status.printInstancesStatus()
	}
	status.printPluginStatus(verbosity)

	if len(errs) > 0 {
		fmt.Println()

		errors := tabby.New()
		errors.AddHeader(aurora.Red("Error(s) extracting status"))
		for _, err := range errs {
			fmt.Printf("%s\n", err)
		}
	}

	return nil
}

// extractPostgresqlStatus gets the PostgreSQL status using the Kubernetes API
func extractPostgresqlStatus(ctx context.Context, cluster apiv1.Cluster) *PostgresqlStatus {
	var errs []error

	managedPods, primaryPod, err := resources.GetInstancePods(ctx, cluster.Name)
	if err != nil {
		errs = append(errs, err)
	}

	// Get the list of Pods created by this Cluster
	instancesStatus, errList := resources.ExtractInstancesStatus(
		ctx,
		&cluster,
		plugin.Config,
		managedPods,
	)
	if len(errList) != 0 {
		errs = append(errs, errList...)
	}

	var pdbl policyv1.PodDisruptionBudgetList
	if err := plugin.Client.List(
		ctx,
		&pdbl,
		client.InNamespace(plugin.Namespace),
		client.MatchingLabels{utils.ClusterLabelName: cluster.Name},
	); err != nil {
		errs = append(errs, err)
	}
	// Extract the status from the instances
	status := PostgresqlStatus{
		Cluster:                 &cluster,
		InstanceStatus:          &instancesStatus,
		PrimaryPod:              primaryPod,
		PodDisruptionBudgetList: pdbl,
		ErrorList:               errs,
	}
	return &status
}

func listFencedInstances(fencedInstances *stringset.Data) string {
	if fencedInstances.Has(utils.FenceAllInstances) {
		return "All Instances"
	}
	return strings.Join(fencedInstances.ToList(), ", ")
}

func (fullStatus *PostgresqlStatus) getClusterSize(
	ctx context.Context,
	client kubernetes.Interface,
	timeout time.Duration,
) (string, error) {
	// Compute the disk space through `du`
	output, _, err := utils.ExecCommand(
		ctx,
		client,
		plugin.Config,
		fullStatus.PrimaryPod,
		specs.PostgresContainerName,
		&timeout,
		"du",
		"-sLh",
		specs.PgDataPath)
	if err != nil {
		return "", err
	}

	size, _, _ := strings.Cut(output, "\t")
	return size, nil
}

func (fullStatus *PostgresqlStatus) printBasicInfo(
	ctx context.Context,
	k8sClient kubernetes.Interface,
	timeout time.Duration,
) {
	summary := tabby.New()

	clusterSize, clusterSizeErr := fullStatus.getClusterSize(ctx, k8sClient, timeout)

	cluster := fullStatus.Cluster

	primaryInstance := cluster.Status.CurrentPrimary

	// Determine if the cluster is hibernated
	hibernated, _ := isHibernated(fullStatus)

	fencedInstances, err := utils.GetFencedInstances(cluster.Annotations)
	if err != nil {
		fmt.Printf("could not check if cluster is fenced: %v", err)
	}
	isPrimaryFenced := cluster.IsInstanceFenced(cluster.Status.CurrentPrimary)
	primaryInstanceStatus := fullStatus.tryGetPrimaryInstance()

	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		primaryInstance = fmt.Sprintf("%v (switching to %v)",
			cluster.Status.CurrentPrimary, cluster.Status.TargetPrimary)
	}

	summary.AddLine("Name", client.ObjectKeyFromObject(cluster).String())

	if primaryInstanceStatus != nil {
		summary.AddLine("System ID:", primaryInstanceStatus.SystemID)
	}
	summary.AddLine("PostgreSQL Image:", cluster.Status.Image)
	if cluster.IsReplica() {
		summary.AddLine("Designated primary:", primaryInstance)
		summary.AddLine("Source cluster: ", cluster.Spec.ReplicaCluster.Source)
	} else {
		summary.AddLine("Primary instance:", primaryInstance)
	}

	switch {
	case hibernated:
		summary.AddLine("Status:", aurora.Red("Hibernated"))
	case isPrimaryFenced:
		summary.AddLine("Status:", aurora.Red("Primary instance is fenced"))
	default:
		// Avoid printing the promotion time when hibernated or fenced
		primaryPromotionTime := getPrimaryPromotionTime(cluster)
		if len(primaryPromotionTime) > 0 {
			summary.AddLine("Primary promotion time:", primaryPromotionTime)
		}
		summary.AddLine("Status:", fullStatus.getStatus(cluster))
	}

	if cluster.Spec.Instances == cluster.Status.Instances {
		summary.AddLine("Instances:", aurora.Green(cluster.Spec.Instances))
	} else {
		summary.AddLine("Instances:", aurora.Red(cluster.Spec.Instances))
	}
	if cluster.Spec.Instances == cluster.Status.ReadyInstances {
		summary.AddLine("Ready instances:", aurora.Green(cluster.Status.ReadyInstances))
	} else {
		summary.AddLine("Ready instances:", aurora.Red(cluster.Status.ReadyInstances))
	}

	if fencedInstances != nil && fencedInstances.Len() > 0 {
		if isPrimaryFenced {
			summary.AddLine("Fenced instances:", aurora.Red(listFencedInstances(fencedInstances)))
		} else {
			summary.AddLine("Fenced instances:", aurora.Yellow(listFencedInstances(fencedInstances)))
		}
	}

	if clusterSizeErr != nil {
		switch {
		case hibernated:
			summary.AddLine("Size:", "- (hibernated)")
		case isPrimaryFenced:
			summary.AddLine("Size:", "- (fenced)")
		default:
			summary.AddLine("Size:", aurora.Red(clusterSizeErr.Error()))
		}
	} else {
		summary.AddLine("Size:", clusterSize)
	}

	if !cluster.IsReplica() && primaryInstanceStatus != nil {
		lsnInfo := fmt.Sprintf(
			"%s (Timeline: %d - WAL File: %s)",
			primaryInstanceStatus.CurrentLsn,
			primaryInstanceStatus.TimeLineID,
			primaryInstanceStatus.CurrentWAL,
		)
		summary.AddLine("Current Write LSN:", lsnInfo)
	}

	if cluster.IsReplica() {
		fmt.Println(aurora.Yellow("Replica Cluster Summary"))
	} else {
		fmt.Println(aurora.Green("Cluster Summary"))
	}

	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary {
		if cluster.Status.CurrentPrimary == "" {
			fmt.Println(aurora.Red("Primary server is initializing"))
		} else {
			fmt.Println(aurora.Red("Switchover in progress"))
		}
	}

	summary.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) printHibernationInfo() {
	hibernated, hibernationCondition := isHibernated(fullStatus)
	if hibernationCondition == nil {
		return
	}

	hibernationStatus := tabby.New()
	if hibernated {
		hibernationStatus.AddLine("Status", "Hibernated")
	} else {
		hibernationStatus.AddLine("Status", "Active")
	}
	hibernationStatus.AddLine("Message", hibernationCondition.Message)
	hibernationStatus.AddLine("Time", hibernationCondition.LastTransitionTime.UTC())

	fmt.Println(aurora.Green("Hibernation"))
	hibernationStatus.Print()

	fmt.Println()
}

func isHibernated(fullStatus *PostgresqlStatus) (bool, *metav1.Condition) {
	cluster := fullStatus.Cluster
	hibernationCondition := meta.FindStatusCondition(
		cluster.Status.Conditions,
		hibernation.HibernationConditionType,
	)

	if hibernationCondition == nil || hibernationCondition.Status != metav1.ConditionTrue {
		return false, hibernationCondition
	}

	return true, hibernationCondition
}

func (fullStatus *PostgresqlStatus) printTokenStatus(token string) {
	primaryInstanceStatus := fullStatus.tryGetPrimaryInstance()

	tokenStatus := tabby.New()
	if tokenContent, err := utils.ParsePgControldataToken(token); err != nil {
		tokenStatus.AddLine(
			"Token",
			fmt.Sprintf("%s %s", token, aurora.Red(fmt.Sprintf("(invalid format: %s)", err.Error()))),
		)
	} else if err := tokenContent.IsValid(); err != nil {
		tokenStatus.AddLine("Token", token)
		tokenStatus.AddLine("Validity", aurora.Red(fmt.Sprintf("not valid: %s", err.Error())))
	} else {
		var systemIDCheck string

		switch {
		case primaryInstanceStatus == nil:
			systemIDCheck = aurora.Red("(no primary have been found)").String()
		case tokenContent.DatabaseSystemIdentifier != primaryInstanceStatus.SystemID:
			systemIDCheck = aurora.Red("(invalid)").String()
		default:
			systemIDCheck = aurora.Green("(ok)").String()
		}

		tokenStatus.AddLine(
			"Token",
			token)
		tokenStatus.AddLine(
			"Validity",
			aurora.Green("valid"))
		tokenStatus.AddLine(
			"Latest checkpoint's TimeLineID",
			tokenContent.LatestCheckpointTimelineID)
		tokenStatus.AddLine(
			"Latest checkpoint's REDO WAL file",
			tokenContent.REDOWALFile)
		tokenStatus.AddLine(
			"Latest checkpoint's REDO location",
			tokenContent.LatestCheckpointREDOLocation)
		tokenStatus.AddLine(
			"Database system identifier",
			fmt.Sprintf("%s %s", tokenContent.DatabaseSystemIdentifier, systemIDCheck))
		tokenStatus.AddLine(
			"Time of latest checkpoint",
			tokenContent.TimeOfLatestCheckpoint)
		tokenStatus.AddLine(
			"Version of the operator",
			tokenContent.OperatorVersion)
	}
	tokenStatus.Print()
}

func (fullStatus *PostgresqlStatus) printDemotionTokenInfo() {
	demotionToken := fullStatus.Cluster.Status.DemotionToken
	if len(demotionToken) == 0 {
		return
	}

	fmt.Println(aurora.Green("Demotion token"))
	fullStatus.printTokenStatus(demotionToken)
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) printPromotionTokenInfo() {
	if fullStatus.Cluster.Spec.ReplicaCluster == nil {
		return
	}

	promotionToken := fullStatus.Cluster.Spec.ReplicaCluster.PromotionToken
	if len(promotionToken) == 0 {
		return
	}

	if promotionToken == fullStatus.Cluster.Status.LastPromotionToken {
		// This token was already processed
		return
	}

	fmt.Println(aurora.Green("Promotion token"))
	fullStatus.printTokenStatus(promotionToken)
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) getStatus(cluster *apiv1.Cluster) string {
	switch cluster.Status.Phase {
	case apiv1.PhaseHealthy, apiv1.PhaseFirstPrimary, apiv1.PhaseCreatingReplica:
		return fmt.Sprintf("%v %v", aurora.Green(cluster.Status.Phase), cluster.Status.PhaseReason)
	case apiv1.PhaseUpgrade, apiv1.PhaseWaitingForUser:
		return fmt.Sprintf("%v %v", aurora.Yellow(cluster.Status.Phase), cluster.Status.PhaseReason)
	default:
		return fmt.Sprintf("%v %v", aurora.Red(cluster.Status.Phase), cluster.Status.PhaseReason)
	}
}

func (fullStatus *PostgresqlStatus) printPostgresConfiguration(
	ctx context.Context,
	client kubernetes.Interface,
	timeout time.Duration,
) []error {
	var errs []error

	// Read PostgreSQL configuration from custom.conf
	customConf, _, err := utils.ExecCommand(ctx, client, plugin.Config, fullStatus.PrimaryPod,
		specs.PostgresContainerName,
		&timeout,
		"cat",
		path.Join(specs.PgDataPath, constants.PostgresqlCustomConfigurationFile))
	if err != nil {
		errs = append(errs, err)
	}

	// Read PostgreSQL HBA Rules from pg_hba.conf
	pgHBAConf, _, err := utils.ExecCommand(ctx, client, plugin.Config, fullStatus.PrimaryPod,
		specs.PostgresContainerName,
		&timeout, "cat", path.Join(specs.PgDataPath, constants.PostgresqlHBARulesFile))
	if err != nil {
		errs = append(errs, err)
	}

	fmt.Println(aurora.Green("PostgreSQL Configuration"))
	fmt.Println(customConf)
	fmt.Println()

	fmt.Println(aurora.Green("PostgreSQL HBA Rules"))
	fmt.Println(pgHBAConf)
	fmt.Println()

	return errs
}

func (fullStatus *PostgresqlStatus) printBackupStatus() {
	cluster := fullStatus.Cluster

	// Check if Barman Cloud plugin is configured
	isBarmanPluginEnabled, pluginParams := isBarmanCloudPluginEnabled(cluster)

	switch {
	case isBarmanPluginEnabled:
		fmt.Println(aurora.Green("Continuous Backup status (Barman Cloud Plugin)"))
	case cluster.Spec.Backup != nil:
		fmt.Println(aurora.Green("Continuous Backup status"))
	default:
		fmt.Println(aurora.Yellow("Continuous Backup not configured"))
		fmt.Println()
		return
	}

	status := tabby.New()
	// If backup is managed by Barman Cloud plugin, fetch and display the ObjectStore CRD
	// Note: The webhook ensures barmanObjectStore and plugin WAL archiver are mutually exclusive,
	// so we don't need to check both conditions
	if isBarmanPluginEnabled {
		barmanObjectName := pluginParams["barmanObjectName"]
		if barmanObjectName == "" {
			fmt.Println(aurora.Red("Backup is managed by the Barman Cloud plugin, " +
				"but 'barmanObjectName' parameter is not configured."))
			fmt.Println(aurora.Red("Please configure the 'barmanObjectName' parameter in the plugin configuration."))
			fmt.Println()
			return
		}

		objectStore, err := fullStatus.getBarmanObject(barmanObjectName)
		if err != nil {
			fmt.Println(aurora.Red(fmt.Sprintf("Error fetching ObjectStore '%s': %v", barmanObjectName, err)))
			fmt.Println()
			return
		}

		fullStatus.printBarmanObjectStoreStatus(status, objectStore, pluginParams)
	} else if cluster.Spec.Backup != nil {
		// FirstRecoverabilityPoint is deprecated and will be removed together
		// with native Barman Cloud support. It is only shown when the backup
		// section is not empty.
		FPoR := cluster.Status.FirstRecoverabilityPoint //nolint:staticcheck
		if FPoR == "" {
			FPoR = "Not Available"
		}
		status.AddLine("First Point of Recoverability:", FPoR)
	}

	fullStatus.printWALArchivingStatus(status)
	status.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) getBarmanObject(barmanObjectName string) (*ObjectStore, error) {
	ctx := context.Background()
	objectStoreGVR := schema.GroupVersionResource{
		Group:    "barmancloud.cnpg.io",
		Version:  "v1",
		Resource: "objectstores",
	}

	dynamicClient := dynamic.NewForConfigOrDie(plugin.Config)
	unstructuredObj, err := dynamicClient.Resource(objectStoreGVR).Namespace(plugin.Namespace).Get(
		ctx, barmanObjectName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	// Convert unstructured to typed ObjectStore
	var objectStore ObjectStore
	err = convertUnstructured(unstructuredObj, &objectStore)
	if err != nil {
		return nil, fmt.Errorf("failed to convert ObjectStore: %w", err)
	}

	return &objectStore, nil
}

// convertUnstructured converts an unstructured object to a typed object
func convertUnstructured(from any, to any) error {
	data, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, to)
}

// isBarmanCloudPluginEnabled checks if the barman-cloud plugin is enabled, and the parameters map
func isBarmanCloudPluginEnabled(cluster *apiv1.Cluster) (bool, map[string]string) {
	for _, plg := range cluster.Spec.Plugins {
		if plg.Name == "barman-cloud.cloudnative-pg.io" {
			if plg.IsEnabled() {
				return true, plg.Parameters
			}
			return false, nil
		}
	}
	return false, nil
}

// printWALArchivingStatus prints the WAL archiving status to the provided tabby table
func (fullStatus *PostgresqlStatus) printWALArchivingStatus(status *tabby.Tabby) {
	primaryInstanceStatus := fullStatus.tryGetPrimaryInstance()
	if primaryInstanceStatus == nil {
		status.AddLine("No Primary instance found")
		return
	}
	isWalArchivingDisabled := fullStatus.Cluster != nil &&
		utils.IsWalArchivingDisabled(&fullStatus.Cluster.ObjectMeta)

	status.AddLine("Working WAL archiving:",
		getWalArchivingStatus(
			primaryInstanceStatus.IsArchivingWAL,
			primaryInstanceStatus.LastFailedWAL,
			isWalArchivingDisabled))
	status.AddLine("WALs waiting to be archived:", primaryInstanceStatus.ReadyWALFiles)

	if primaryInstanceStatus.LastArchivedWAL == "" {
		status.AddLine("Last Archived WAL:", "-")
	} else {
		status.AddLine("Last Archived WAL:", primaryInstanceStatus.LastArchivedWAL,
			" @ ", primaryInstanceStatus.LastArchivedWALTime)
	}
	if primaryInstanceStatus.LastFailedWAL == "" {
		status.AddLine("Last Failed WAL:", "-")
	} else {
		status.AddLine("Last Failed WAL:", primaryInstanceStatus.LastFailedWAL,
			" @ ", primaryInstanceStatus.LastFailedWALTime)
	}
}

func getWalArchivingStatus(isArchivingWAL bool, lastFailedWAL string, isWalArchivingDisabled bool) string {
	switch {
	case isWalArchivingDisabled:
		return aurora.Yellow("Disabled").String()
	case isArchivingWAL:
		return aurora.Green("OK").String()
	case lastFailedWAL != "":
		return aurora.Red("Failing").String()
	default:
		return aurora.Yellow("Starting Up").String()
	}
}

func (fullStatus *PostgresqlStatus) areReplicationSlotsEnabled() bool {
	return fullStatus.Cluster.Spec.ReplicationSlots != nil &&
		fullStatus.Cluster.Spec.ReplicationSlots.HighAvailability != nil &&
		fullStatus.Cluster.Spec.ReplicationSlots.HighAvailability.GetEnabled()
}

func (fullStatus *PostgresqlStatus) printReplicaStatusTableHeader(table *tabby.Tabby, verbosity int) {
	switch {
	case fullStatus.areReplicationSlotsEnabled() && verbosity > 0:
		table.AddHeader(
			"Name",
			"Sent LSN",
			"Write LSN",
			"Flush LSN",
			"Replay LSN", // For standby use "Replay LSN"
			"Write Lag",
			"Flush Lag",
			"Replay Lag",
			"State",
			"Sync State",
			"Sync Priority",
			"Replication Slot", // Replication Slots
			"Slot Restart LSN",
			"Slot WAL Status",
			"Slot Safe WAL Size",
		)
	case fullStatus.areReplicationSlotsEnabled() && verbosity == 0:
		table.AddHeader(
			"Name",
			"Sent LSN",
			"Write LSN",
			"Flush LSN",
			"Replay LSN", // For standby use "Replay LSN"
			"Write Lag",
			"Flush Lag",
			"Replay Lag",
			"State",
			"Sync State",
			"Sync Priority",
			"Replication Slot", // Replication Slots
		)
	default:
		table.AddHeader(
			"Name",
			"Sent LSN",
			"Write LSN",
			"Flush LSN",
			"Replay LSN", // For standby use "Replay LSN"
			"Write Lag",
			"Flush Lag",
			"Replay Lag",
			"State",
			"Sync State",
			"Sync Priority",
		)
	}
}

// addReplicationSlotsColumns append the column data for replication slot
func (fullStatus *PostgresqlStatus) addReplicationSlotsColumns(
	applicationName string,
	columns *[]any,
	verbosity int,
) {
	printSlotActivity := func(isActive bool) string {
		if isActive {
			return "active"
		}
		return "inactive"
	}
	slot := fullStatus.getPrintableReplicationSlotInfo(applicationName)
	switch {
	case slot != nil && verbosity > 0:
		*columns = append(*columns,
			printSlotActivity(slot.Active),
			slot.RestartLsn,
			slot.WalStatus,
			getPrintableIntegerPointer(slot.SafeWalSize),
		)
	case slot != nil && verbosity == 0:
		*columns = append(*columns,
			printSlotActivity(slot.Active),
		)
	case slot == nil && verbosity > 0:
		*columns = append(*columns,
			"-",
			"-",
			"-",
			"-",
		)
	default:
		*columns = append(*columns,
			"-",
		)
	}
}

func (fullStatus *PostgresqlStatus) printReplicaStatus(verbosity int) {
	if fullStatus.Cluster.IsReplica() {
		return
	}

	fmt.Println(aurora.Green("Streaming Replication status"))
	if fullStatus.Cluster.Spec.Instances == 1 {
		fmt.Println(aurora.Yellow("Not configured").String())
		fmt.Println()
		return
	}

	primaryInstanceStatus := fullStatus.tryGetPrimaryInstance()
	if primaryInstanceStatus == nil {
		fmt.Println(aurora.Yellow("Primary instance not found").String())
		fmt.Println()
		return
	}

	if len(primaryInstanceStatus.ReplicationInfo) == 0 {
		fmt.Println(aurora.Yellow("Not available yet").String())
		fmt.Println()
		return
	}

	if fullStatus.areReplicationSlotsEnabled() {
		fmt.Println(aurora.Cyan("Replication Slots Enabled").String())
	}

	status := tabby.New()
	fullStatus.printReplicaStatusTableHeader(status, verbosity)

	// print Replication Slots columns only if the cluster has replication slots enabled
	addReplicationSlotsColumns := func(_ string, _ *[]any) {}
	if fullStatus.areReplicationSlotsEnabled() {
		addReplicationSlotsColumns = func(applicationName string, columns *[]any) {
			fullStatus.addReplicationSlotsColumns(applicationName, columns, verbosity)
		}
	}

	replicationInfo := primaryInstanceStatus.ReplicationInfo
	sort.Sort(replicationInfo)
	for _, replication := range replicationInfo {
		columns := []any{
			replication.ApplicationName,
			replication.SentLsn,
			replication.WriteLsn,
			replication.FlushLsn,
			replication.ReplayLsn,
			replication.WriteLag,
			replication.FlushLag,
			replication.ReplayLag,
			replication.State,
			replication.SyncState,
			replication.SyncPriority,
		}
		addReplicationSlotsColumns(replication.ApplicationName, &columns)
		status.AddLine(columns...)
	}
	status.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) printInstancesStatus() {
	//  Column "Replication role"
	//  If fenced, print "Fenced"
	//  else if instance is primary, print "Primary"
	//  	Otherwise, it is considered a standby
	//  else if it is not replicating:
	//  	if it is accepting connections: # readiness OK
	//      	print "Standby (file based)"
	//    	else:
	//  		if pg_rewind is running, print "Standby (pg_rewind)"  - #liveness OK, readiness Not OK
	//              else print "Standby (starting up)"
	//  else:
	//  	if it is paused, print "Standby (paused)"
	//  	else if SyncState = sync/quorum print "Standby (sync)"
	//  	else if SyncState = potential print "Standby (potential sync)"
	//  	else print "Standby (async)"

	status := tabby.New()
	fmt.Println(aurora.Green("Instances status"))
	status.AddHeader(
		"Name",
		"Current LSN", // For standby use "Replay LSN"
		"Replication role",
		"Status",
		"QoS",
		"Manager Version",
		"Node")

	sort.Sort(fullStatus.InstanceStatus)
	for _, instance := range fullStatus.InstanceStatus.Items {
		if instance.Error != nil {
			status.AddLine(
				instance.Pod.Name,
				"-",
				"-",
				apierrs.ReasonForError(instance.Error),
				instance.Pod.Status.QOSClass,
				"-",
				instance.Pod.Spec.NodeName,
			)
			continue
		}
		statusMsg := "OK"
		if instance.PendingRestart {
			statusMsg += " (pending restart)"
		}

		replicaRole := getReplicaRole(instance, fullStatus)
		status.AddLine(
			instance.Pod.Name,
			getCurrentLSN(instance),
			replicaRole,
			statusMsg,
			instance.Pod.Status.QOSClass,
			instance.InstanceManagerVersion,
			instance.Pod.Spec.NodeName,
		)
		continue
	}
	status.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) printCertificatesStatus() {
	status := tabby.New()
	status.AddHeader("Certificate Name", "Expiration Date", "Days Left Until Expiration")

	hasExpiringCertificate := false
	hasExpiredCertificate := false

	certExpirations := fullStatus.Cluster.Status.Certificates.Expirations

	// Sort `fullStatus.Cluster.Status.Certificates.Expirations` by `certificationName` asc
	certNames := make([]string, 0, len(certExpirations))
	for certName := range certExpirations {
		certNames = append(certNames, certName)
	}
	sort.Strings(certNames)

	for _, certName := range certNames {
		expirationDate := certExpirations[certName]
		expirationTime, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", expirationDate)
		if err != nil {
			fmt.Printf("\n error while parsing the following certificate: %s, date: %s",
				certName, expirationDate)
		}

		validityLeft := time.Until(expirationTime)

		validityLeftInDays := fmt.Sprintf("%.2f", validityLeft.Hours()/24)

		if validityLeft < 0 {
			validityLeftInDays = "Expired"
			hasExpiredCertificate = true
		} else if validityLeft < time.Hour*24*7 {
			validityLeftInDays += " - Expires Soon"
			hasExpiringCertificate = true
		}
		status.AddLine(certName, expirationDate, validityLeftInDays)
	}

	color := aurora.Green

	if hasExpiredCertificate {
		color = aurora.Red
	} else if hasExpiringCertificate {
		color = aurora.Yellow
	}

	fmt.Println(color("Certificates Status"))
	status.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) tryGetPrimaryInstance() *postgres.PostgresqlStatus {
	for idx, instanceStatus := range fullStatus.InstanceStatus.Items {
		if instanceStatus.IsPrimary || len(instanceStatus.ReplicationInfo) > 0 ||
			fullStatus.isReplicaClusterDesignatedPrimary(instanceStatus) {
			return &fullStatus.InstanceStatus.Items[idx]
		}
	}

	return nil
}

func getCurrentLSN(instance postgres.PostgresqlStatus) types.LSN {
	if instance.IsPrimary {
		return instance.CurrentLsn
	}
	return instance.ReplayLsn
}

func getReplicaRole(instance postgres.PostgresqlStatus, fullStatus *PostgresqlStatus) string {
	if instance.MightBeUnavailable {
		return "Fenced"
	}
	if instance.IsPrimary {
		return "Primary"
	}
	if fullStatus.isReplicaClusterDesignatedPrimary(instance) {
		return "Designated primary"
	}

	if !instance.IsWalReceiverActive {
		if utils.IsPodReady(*instance.Pod) {
			return "Standby (file based)"
		}
		if instance.IsPgRewindRunning {
			return "Standby (pg_rewind)"
		}
		return "Standby (starting up)"
	}

	if instance.ReplayPaused {
		return "Standby (paused)"
	}

	primaryInstanceStatus := fullStatus.tryGetPrimaryInstance()
	if primaryInstanceStatus == nil {
		return "Unknown"
	}

	for _, state := range primaryInstanceStatus.ReplicationInfo {
		// todo: handle others states other than 'streaming'
		if !(state.ApplicationName == instance.Pod.Name && state.State == "streaming") {
			continue
		}
		switch state.SyncState {
		case "quorum", "sync":
			return "Standby (sync)"
		case "potential":
			return "Standby (potential sync)"
		case "async":
			return "Standby (async)"
		default:
			continue
		}
	}

	// TODO: improve the way we detect a standby in a replica cluster.
	// A fuller fix would make sure the Designated Primary gets the replication
	// list from pg_stat_replication
	if len(primaryInstanceStatus.ReplicationInfo) == 0 {
		return "Standby (in Replica Cluster)"
	}

	return "Unknown"
}

// TODO: improve the way we detect the Designated Primary in a replica cluster
func (fullStatus *PostgresqlStatus) isReplicaClusterDesignatedPrimary(instance postgres.PostgresqlStatus) bool {
	return fullStatus.Cluster.IsReplica() && instance.Pod.Name == fullStatus.PrimaryPod.Name
}

func (fullStatus *PostgresqlStatus) printUnmanagedReplicationSlotStatus() {
	var unmanagedReplicationSlots postgres.PgReplicationSlotList
	for _, slot := range fullStatus.getReplicationSlotList() {
		// we skip replication slots that we manage
		replicationSlots := fullStatus.Cluster.Spec.ReplicationSlots
		if replicationSlots != nil && replicationSlots.HighAvailability != nil &&
			strings.HasPrefix(slot.SlotName, replicationSlots.HighAvailability.GetSlotPrefix()) {
			continue
		}
		unmanagedReplicationSlots = append(unmanagedReplicationSlots, slot)
	}

	const headerMessage = "Unmanaged Replication Slot Status"
	status := tabby.New()
	if len(unmanagedReplicationSlots) == 0 {
		status.AddLine(aurora.Green(headerMessage))
		status.AddLine("No unmanaged replication slots found")
		fmt.Println()
		return
	}

	status.AddHeader(
		"Slot Name",
		"Slot Type",
		"Database",
		"Active",
		"Restart LSN",
		"XMin",
		"Catalog XMin",
		"Datoid",
		"Plugin",
		"Wal Status",
		"Safe Wal Size",
	)

	var containsFailure bool
	for _, slot := range unmanagedReplicationSlots {
		// add any other failure conditions here
		if !slot.Active {
			containsFailure = true
		}
		status.AddLine(
			slot.SlotName,
			slot.SlotType,
			slot.Database,
			slot.Active,
			slot.RestartLsn,
			slot.Xmin,
			slot.CatalogXmin,
			slot.Datoid,
			slot.Plugin,
			slot.WalStatus,
			getPrintableIntegerPointer(slot.SafeWalSize),
		)
	}
	color := aurora.Green
	if containsFailure {
		color = aurora.Red
	}

	fmt.Println(color(headerMessage))
	status.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) printPodDisruptionBudgetStatus() {
	const header = "Pod Disruption Budgets status"

	fmt.Println(aurora.Green(header))

	if len(fullStatus.PodDisruptionBudgetList.Items) == 0 {
		fmt.Println("No active PodDisruptionBudgets found")
		fmt.Println()
		return
	}

	status := tabby.New()
	status.AddHeader(
		"Name",
		"Role",
		"Expected Pods",
		"Current Healthy",
		"Minimum Desired Healthy",
		"Disruptions Allowed",
	)

	for _, item := range fullStatus.PodDisruptionBudgetList.Items {
		status.AddLine(item.Name,
			item.Spec.Selector.MatchLabels[utils.ClusterInstanceRoleLabelName],
			item.Status.ExpectedPods,
			item.Status.CurrentHealthy,
			item.Status.DesiredHealthy,
			item.Status.DisruptionsAllowed,
		)
	}

	status.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) printBasebackupStatus(verbosity int) {
	const header = "Physical backups"

	primaryInstanceStatus := fullStatus.tryGetPrimaryInstance()
	if primaryInstanceStatus == nil {
		fmt.Println(aurora.Red(header))
		fmt.Println(aurora.Red("Primary instance not found").String())
		fmt.Println()
		return
	}

	if len(primaryInstanceStatus.PgStatBasebackupsInfo) == 0 {
		if verbosity > 0 {
			fmt.Println(aurora.Green(header))
			fmt.Println(aurora.Yellow("No running physical backups found").String())
			fmt.Println()
		}
		return
	}

	fmt.Println(aurora.Green(header))

	status := tabby.New()
	status.AddHeader(
		"Name",
		"Phase",
		"Started at",
		"Total",
		"Transferred",
		"Progress",
		"Tablespaces",
	)

	basebackupsInfo := primaryInstanceStatus.PgStatBasebackupsInfo

	for _, bb := range basebackupsInfo {
		// BackupTotal may be empty when PostgreSQL is waiting for a checkpoint or when the estimation
		// have been disabled by the user (i.e. with the `--no-estimate-size` option).
		// See: https://www.postgresql.org/docs/current/progress-reporting.html#BASEBACKUP-PROGRESS-REPORTING
		progress := ""
		if bb.BackupTotal != 0 {
			progress = fmt.Sprintf("%.2f%%", float64(bb.BackupStreamed)/float64(bb.BackupTotal)*100)
		}

		columns := []any{
			bb.ApplicationName,
			bb.Phase,
			bb.BackendStart,
			bb.BackupTotalPretty,
			bb.BackupStreamedPretty,
			progress,
			fmt.Sprintf("%v/%v", bb.TablespacesStreamed, bb.TablespacesTotal),
		}

		status.AddLine(columns...)
	}

	status.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) printRoleManagerStatus() {
	const header = "Managed roles status"

	managedRolesStatus := fullStatus.Cluster.Status.ManagedRolesStatus
	containsErrors := len(managedRolesStatus.CannotReconcile) > 0
	containsWarnings := func() bool {
		for status, elements := range managedRolesStatus.ByStatus {
			if len(elements) == 0 {
				continue
			}

			switch status {
			case apiv1.RoleStatusReconciled, apiv1.RoleStatusReserved:
				continue
			default:
				return true
			}
		}

		return false
	}

	headerColor := aurora.Green
	if containsErrors {
		headerColor = aurora.Red
	} else if containsWarnings() {
		headerColor = aurora.Yellow
	}

	fmt.Println(headerColor(header))

	if len(managedRolesStatus.ByStatus) == 0 && len(managedRolesStatus.CannotReconcile) == 0 {
		fmt.Println("No roles managed")
		fmt.Println()
		return
	}

	roleStatus := tabby.New()
	roleStatus.AddHeader("Status", "Roles")

	for status, roles := range managedRolesStatus.ByStatus {
		roleStatus.AddLine(status, strings.Join(roles, ","))
	}
	roleStatus.Print()
	fmt.Println()

	if containsErrors {
		fmt.Println(aurora.Red("Irreconcilable roles"))
		errorStatus := tabby.New()
		errorStatus.AddHeader("Role", "Errors")
		for role, errors := range managedRolesStatus.CannotReconcile {
			errorStatus.AddLine(role, strings.Join(errors, ","))
		}
		errorStatus.Print()
		fmt.Println()
	}
}

func (fullStatus *PostgresqlStatus) printTablespacesStatus() {
	const header = "Tablespaces status"

	tablespacesStatus := fullStatus.Cluster.Status.TablespacesStatus
	var containsErrors, hasPendingTablespaces bool
	for _, stat := range tablespacesStatus {
		if stat.Error != "" {
			containsErrors = true
		}
		if stat.State == apiv1.TablespaceStatusPendingReconciliation {
			hasPendingTablespaces = true
		}
	}

	headerColor := aurora.Green
	if containsErrors {
		headerColor = aurora.Red
	} else if hasPendingTablespaces {
		headerColor = aurora.Yellow
	}

	temporaryTablespaces := stringset.New()
	for _, tablespace := range fullStatus.Cluster.Spec.Tablespaces {
		if tablespace.Temporary {
			temporaryTablespaces.Put(tablespace.Name)
		}
	}

	fmt.Println(headerColor(header))

	if len(tablespacesStatus) == 0 {
		fmt.Println("No managed tablespaces")
		fmt.Println()
		return
	}

	tbsStatus := tabby.New()
	tbsStatus.AddHeader("Tablespace", "Owner", "Status", "Temporary", "Error")

	for _, tbs := range tablespacesStatus {
		tbsStatus.AddLine(tbs.Name, tbs.Owner, tbs.State, temporaryTablespaces.Has(tbs.Name), tbs.Error)
	}
	tbsStatus.Print()
	fmt.Println()
}

func (fullStatus *PostgresqlStatus) printPluginStatus(verbosity int) {
	const header = "Plugins status"

	parseCapabilities := func(capabilities []string) string {
		if len(capabilities) == 0 {
			return "N/A"
		}

		result := make([]string, len(capabilities))
		for idx, capability := range capabilities {
			switch capability {
			case identity.PluginCapability_Service_TYPE_BACKUP_SERVICE.String():
				result[idx] = "Backup Service"
			case identity.PluginCapability_Service_TYPE_RESTORE_JOB.String():
				result[idx] = "Restore Job"
			case identity.PluginCapability_Service_TYPE_RECONCILER_HOOKS.String():
				result[idx] = "Reconciler Hooks"
			case identity.PluginCapability_Service_TYPE_WAL_SERVICE.String():
				result[idx] = "WAL Service"
			case identity.PluginCapability_Service_TYPE_OPERATOR_SERVICE.String():
				result[idx] = "Operator Service"
			case identity.PluginCapability_Service_TYPE_LIFECYCLE_SERVICE.String():
				result[idx] = "Lifecycle Service"
			case identity.PluginCapability_Service_TYPE_POSTGRES.String():
				result[idx] = "Postgres Service"
			case identity.PluginCapability_Service_TYPE_UNSPECIFIED.String():
				continue
			default:
				result[idx] = capability
			}
		}

		return strings.Join(result, ", ")
	}

	if len(fullStatus.Cluster.Status.PluginStatus) == 0 {
		if verbosity > 0 {
			fmt.Println(aurora.Green(header))
			fmt.Println("No plugins found")
		}
		return
	}

	fmt.Println(aurora.Green(header))

	status := tabby.New()
	status.AddHeader("Name", "Version", "Status", "Reported Operator Capabilities")

	for _, plg := range fullStatus.Cluster.Status.PluginStatus {
		plgStatus := "N/A"
		if plg.Status != "" {
			plgStatus = plg.Status
		}
		status.AddLine(plg.Name, plg.Version, plgStatus, parseCapabilities(plg.Capabilities))
	}

	status.Print()
	fmt.Println()
}

func getPrimaryPromotionTime(cluster *apiv1.Cluster) string {
	return getPrimaryPromotionTimeIdempotent(cluster, time.Now())
}

func getPrimaryPromotionTimeIdempotent(cluster *apiv1.Cluster, currentTime time.Time) string {
	if len(cluster.Status.CurrentPrimaryTimestamp) == 0 {
		return ""
	}

	primaryInstanceTimestamp, err := time.Parse(
		metav1.RFC3339Micro,
		cluster.Status.CurrentPrimaryTimestamp,
	)
	if err != nil {
		return aurora.Red("error: " + err.Error()).String()
	}

	duration := currentTime.Sub(primaryInstanceTimestamp)
	return fmt.Sprintf(
		"%s (%s)",
		primaryInstanceTimestamp.Round(time.Second),
		duration.Round(time.Second),
	)
}

// printBarmanObjectStoreStatus prints the ObjectStore CRD status in a tree-like format
func (fullStatus *PostgresqlStatus) printBarmanObjectStoreStatus(
	status *tabby.Tabby,
	objectStore *ObjectStore,
	params map[string]string,
) {
	serverName := params["serverName"]
	if serverName == "" {
		serverName = fullStatus.Cluster.Name
	}

	// Retrieve server's recovery window information
	recoveryWindow, ok := objectStore.Status.ServerRecoveryWindow[serverName]
	if !ok {
		fmt.Println(aurora.Red(fmt.Sprintf("No recovery window information found in ObjectStore '%s' for server '%s'",
			objectStore.GetName(), serverName)))
		return
	}

	status.AddLine("ObjectStore / Server name:", objectStore.GetName()+"/"+serverName)

	// Format and print first recoverability point
	if recoveryWindow.FirstRecoverabilityPoint != nil {
		status.AddLine("First Point of Recoverability:",
			recoveryWindow.FirstRecoverabilityPoint.Format("2006-01-02 15:04:05 MST"))
	} else {
		status.AddLine("First Point of Recoverability:", "-")
	}

	// Format and print last successful backup time
	if recoveryWindow.LastSuccessfulBackupTime != nil {
		status.AddLine("Last Successful Backup:",
			recoveryWindow.LastSuccessfulBackupTime.Format("2006-01-02 15:04:05 MST"))
	} else {
		status.AddLine("Last Successful Backup:", "-")
	}

	// Format and print last failed backup time (in red if present)
	if recoveryWindow.LastFailedBackupTime != nil {
		status.AddLine("Last Failed Backup:",
			aurora.Red(recoveryWindow.LastFailedBackupTime.Format("2006-01-02 15:04:05 MST")))
	} else {
		status.AddLine("Last Failed Backup:", "-")
	}
}
