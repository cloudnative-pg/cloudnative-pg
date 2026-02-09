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

package disk

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/cheynewallace/tabby"
	"github.com/logrusorgru/aurora/v4"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// ClusterDiskInfo contains the disk status information for display
type ClusterDiskInfo struct {
	Cluster *apiv1.Cluster `json:"cluster"`
}

// Status implements the "disk status" subcommand
func Status(
	ctx context.Context,
	clusterName string,
	format plugin.OutputFormat,
) error {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := plugin.Client.Get(ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName}, &cluster)
	if err != nil {
		return fmt.Errorf("while trying to get cluster %s in namespace %s: %w",
			clusterName, plugin.Namespace, err)
	}

	status := &ClusterDiskInfo{
		Cluster: &cluster,
	}

	err = plugin.Print(status, format, os.Stdout)
	if err != nil || format != plugin.OutputFormatText {
		return err
	}

	printDiskStatus(&cluster)

	return nil
}

// formatBytes formats bytes as a human-readable string
func formatBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TiB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GiB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MiB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KiB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func printDiskStatus(cluster *apiv1.Cluster) {
	fmt.Println()
	fmt.Printf("Disk Status for Cluster %s\n", aurora.Bold(cluster.Name))
	fmt.Println(aurora.Faint("==================================="))

	if cluster.Status.DiskStatus == nil || len(cluster.Status.DiskStatus.Instances) == 0 {
		fmt.Println()
		fmt.Println(aurora.Yellow("No disk status available. The operator may not have collected disk metrics yet."))
		fmt.Println()
		return
	}

	// Sort instance names for consistent output
	instanceNames := make([]string, 0, len(cluster.Status.DiskStatus.Instances))
	for name := range cluster.Status.DiskStatus.Instances {
		instanceNames = append(instanceNames, name)
	}
	sort.Strings(instanceNames)

	for _, instanceName := range instanceNames {
		instanceStatus := cluster.Status.DiskStatus.Instances[instanceName]
		if instanceStatus == nil {
			continue
		}

		fmt.Println()
		role := getInstanceRole(cluster, instanceName)
		fmt.Printf("Instance: %s (%s)\n", aurora.Bold(instanceName), aurora.Cyan(role))

		if instanceStatus.LastUpdated != nil {
			fmt.Printf("  Last Updated: %s\n", aurora.Faint(instanceStatus.LastUpdated.Format("2006-01-02 15:04:05 MST")))
		}

		// Print Data Volume
		if instanceStatus.DataVolume != nil {
			printVolumeStatus("Data Volume", instanceStatus.DataVolume)
		}

		// Print WAL Volume
		if instanceStatus.WALVolume != nil {
			printVolumeStatus("WAL Volume", instanceStatus.WALVolume)
		}

		// Print Tablespace Volumes
		if len(instanceStatus.Tablespaces) > 0 {
			tablespacesNames := make([]string, 0, len(instanceStatus.Tablespaces))
			for name := range instanceStatus.Tablespaces {
				tablespacesNames = append(tablespacesNames, name)
			}
			sort.Strings(tablespacesNames)

			for _, tsName := range tablespacesNames {
				tsStatus := instanceStatus.Tablespaces[tsName]
				printVolumeStatus(fmt.Sprintf("Tablespace: %s", tsName), tsStatus)
			}
		}

		// Print WAL Health
		if instanceStatus.WALHealth != nil {
			printWALHealth(instanceStatus.WALHealth)
		}
	}

	// Print Auto-Resize Events
	if len(cluster.Status.AutoResizeEvents) > 0 {
		printAutoResizeEvents(cluster.Status.AutoResizeEvents)
	}

	fmt.Println()
}

func getInstanceRole(cluster *apiv1.Cluster, instanceName string) string {
	if cluster.Status.CurrentPrimary == instanceName || cluster.Status.TargetPrimary == instanceName {
		return "primary"
	}
	return "standby"
}

func printVolumeStatus(title string, vol *apiv1.VolumeDiskStatus) {
	fmt.Printf("\n  %s:\n", aurora.Bold(title))

	// Create table for volume stats
	t := tabby.New()

	usageColor := getUsageColor(vol.PercentUsed)
	t.AddLine("    Total:", formatBytes(vol.TotalBytes))
	t.AddLine("    Used:", formatBytes(vol.UsedBytes))
	t.AddLine("    Available:", formatBytes(vol.AvailableBytes))
	t.AddLine("    Usage:", usageColor(fmt.Sprintf("%d%%", vol.PercentUsed)))

	if vol.InodesTotal > 0 {
		inodesPercent := float64(vol.InodesUsed) / float64(vol.InodesTotal) * 100
		t.AddLine("    Inodes:", fmt.Sprintf("%d / %d (%.1f%%)", vol.InodesUsed, vol.InodesTotal, inodesPercent))
	}

	t.Print()
}

func getUsageColor(percent int) func(any) aurora.Value {
	switch {
	case percent >= 95:
		return aurora.Red
	case percent >= 80:
		return aurora.Yellow
	default:
		return aurora.Green
	}
}

func printWALHealth(health *apiv1.WALHealthInfo) {
	fmt.Printf("\n  %s:\n", aurora.Bold("WAL Health"))

	t := tabby.New()

	archiveStatus := aurora.Green("Healthy")
	if !health.ArchiveHealthy {
		archiveStatus = aurora.Red("Unhealthy")
	}
	t.AddLine("    Archive Status:", archiveStatus)
	t.AddLine("    Pending WAL Files:", health.PendingWALFiles)

	if health.InactiveSlotCount > 0 {
		t.AddLine("    Inactive Slots:", aurora.Yellow(health.InactiveSlotCount))

		for _, slot := range health.InactiveSlots {
			retentionBytes := slot.RetentionBytes
			if retentionBytes < 0 {
				retentionBytes = 0
			}
			t.AddLine(fmt.Sprintf("      - %s:", slot.SlotName),
				fmt.Sprintf("retaining %s", formatBytes(uint64(retentionBytes)))) //nolint:gosec // bounds checked
		}
	} else {
		t.AddLine("    Inactive Slots:", aurora.Green("0"))
	}

	t.Print()
}

func printAutoResizeEvents(events []apiv1.AutoResizeEvent) {
	fmt.Println()
	fmt.Printf("%s\n", aurora.Bold("Recent Auto-Resize Events"))
	fmt.Println(aurora.Faint("-----------------------------------"))

	// Show only the last 5 events
	start := 0
	if len(events) > 5 {
		start = len(events) - 5
	}

	t := tabby.New()
	t.AddHeader("TIME", "INSTANCE", "VOLUME", "PREVIOUS", "NEW", "RESULT")

	for i := len(events) - 1; i >= start; i-- {
		event := events[i]
		volumeDesc := string(event.VolumeType)
		if event.Tablespace != "" {
			volumeDesc = fmt.Sprintf("%s (%s)", event.VolumeType, event.Tablespace)
		}

		resultColor := aurora.Green
		if event.Result != apiv1.ResizeResultSuccess {
			resultColor = aurora.Red
		}

		t.AddLine(
			event.Timestamp.Format("2006-01-02 15:04:05"),
			event.InstanceName,
			volumeDesc,
			event.PreviousSize,
			event.NewSize,
			resultColor(event.Result),
		)
	}

	t.Print()
}
