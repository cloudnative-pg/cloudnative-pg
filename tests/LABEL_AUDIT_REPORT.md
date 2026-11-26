# E2E Labeling Audit Report
Generated on: Fri Feb 20 16:50:26 AEDT 2026

## Table of Contents
* [1. Label Usage Summary](#1-label-usage-summary)
* [2. Detailed Breakdown by Label](#2-detailed-breakdown-by-label)
* [3. Detailed Breakdown by File](#3-detailed-breakdown-by-file)

## 1. Label Usage Summary
| Label Name | File Count | Total Matches |
| :--- | :---: | :---: |
| LabelBackupRestore             | 7          | 12            |
| LabelBasic                     | 10         | 10            |
| LabelClusterMetadata           | 2          | 2             |
| LabelDeclarativeDatabases      | 1          | 1             |
| LabelDisruptive                | 8          | 8             |
| LabelImageVolumeExtensions     | 1          | 1             |
| LabelImportingDatabases        | 2          | 2             |
| LabelMaintenance               | 1          | 1             |
| LabelNoOpenshift               | 2          | 2             |
| LabelObservability             | 4          | 5             |
| LabelOperator                  | 4          | 4             |
| LabelPerformance               | 2          | 2             |
| LabelPlugin                    | 2          | 3             |
| LabelPodScheduling             | 3          | 3             |
| LabelPostgresConfiguration     | 1          | 1             |
| LabelPostgresMajorUpgrade      | 1          | 1             |
| LabelPublicationSubscription   | 1          | 1             |
| LabelRecovery                  | 2          | 2             |
| LabelReplication               | 4          | 5             |
| LabelSecurity                  | 1          | 1             |
| LabelSelfHealing               | 5          | 5             |
| LabelServiceConnectivity       | 5          | 10            |
| LabelSmoke                     | 7          | 7             |
| LabelSnapshot                  | 2          | 2             |
| LabelStorage                   | 5          | 5             |
| LabelTablespaces               | 1          | 1             |
| LabelUpgrade                   | 1          | 1             |

## 2. Detailed Breakdown by Label
### Label: `LabelBackupRestore`
    - `./e2e/backup_restore_azure_test.go`: 2 matches
    - `./e2e/backup_restore_azurite_test.go`: 2 matches
    - `./e2e/wal_restore_parallel_test.go`: 1 matches
    - `./e2e/backup_restore_minio_test.go`: 2 matches
    - `./e2e/replica_mode_cluster_test.go`: 3 matches
    - `./e2e/tablespaces_test.go`: 1 matches
    - `./e2e/volume_snapshot_test.go`: 1 matches

**Summary:** Found in 7 files with 12 total occurrences.

### Label: `LabelBasic`
    - `./e2e/operator_deployment_test.go`: 1 matches
    - `./e2e/probes_test.go`: 1 matches
    - `./e2e/managed_roles_test.go`: 1 matches
    - `./e2e/pod_patch_test.go`: 1 matches
    - `./e2e/managed_services_test.go`: 1 matches
    - `./e2e/initdb_test.go`: 1 matches
    - `./e2e/tablespaces_test.go`: 1 matches
    - `./e2e/architecture_test.go`: 1 matches
    - `./e2e/cluster_setup_test.go`: 1 matches
    - `./e2e/declarative_database_management_test.go`: 1 matches

**Summary:** Found in 10 files with 10 total occurrences.

### Label: `LabelClusterMetadata`
    - `./e2e/config_support_test.go`: 1 matches
    - `./e2e/configuration_update_test.go`: 1 matches

**Summary:** Found in 2 files with 2 total occurrences.

### Label: `LabelDeclarativeDatabases`
    - `./e2e/declarative_database_management_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

### Label: `LabelDisruptive`
    - `./e2e/config_support_test.go`: 1 matches
    - `./e2e/eviction_test.go`: 1 matches
    - `./e2e/operator_unavailable_test.go`: 1 matches
    - `./e2e/self_fencing_test.go`: 1 matches
    - `./e2e/webhook_test.go`: 1 matches
    - `./e2e/operator_ha_test.go`: 1 matches
    - `./e2e/drain_node_test.go`: 1 matches
    - `./e2e/tolerations_test.go`: 1 matches

**Summary:** Found in 8 files with 8 total occurrences.

### Label: `LabelImageVolumeExtensions`
    - `./e2e/imagevolume_extensions_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

### Label: `LabelImportingDatabases`
    - `./e2e/cluster_monolithic_test.go`: 1 matches
    - `./e2e/cluster_microservice_test.go`: 1 matches

**Summary:** Found in 2 files with 2 total occurrences.

### Label: `LabelMaintenance`
    - `./e2e/drain_node_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

### Label: `LabelNoOpenshift`
    - `./e2e/apparmor_test.go`: 1 matches
    - `./e2e/upgrade_test.go`: 1 matches

**Summary:** Found in 2 files with 2 total occurrences.

### Label: `LabelObservability`
    - `./e2e/pgbouncer_metrics_test.go`: 1 matches
    - `./e2e/metrics_test.go`: 1 matches
    - `./e2e/logs_test.go`: 2 matches
    - `./e2e/monitoring_test.go`: 1 matches

**Summary:** Found in 4 files with 5 total occurrences.

### Label: `LabelOperator`
    - `./e2e/operator_deployment_test.go`: 1 matches
    - `./e2e/operator_unavailable_test.go`: 1 matches
    - `./e2e/webhook_test.go`: 1 matches
    - `./e2e/operator_ha_test.go`: 1 matches

**Summary:** Found in 4 files with 4 total occurrences.

### Label: `LabelPerformance`
    - `./e2e/fastswitchover_test.go`: 1 matches
    - `./e2e/fastfailover_test.go`: 1 matches

**Summary:** Found in 2 files with 2 total occurrences.

### Label: `LabelPlugin`
    - `./e2e/certificates_test.go`: 2 matches
    - `./e2e/fencing_test.go`: 1 matches

**Summary:** Found in 2 files with 3 total occurrences.

### Label: `LabelPodScheduling`
    - `./e2e/affinity_test.go`: 1 matches
    - `./e2e/nodeselector_test.go`: 1 matches
    - `./e2e/tolerations_test.go`: 1 matches

**Summary:** Found in 3 files with 3 total occurrences.

### Label: `LabelPostgresConfiguration`
    - `./e2e/rolling_update_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

### Label: `LabelPostgresMajorUpgrade`
    - `./e2e/cluster_major_upgrade_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

### Label: `LabelPublicationSubscription`
    - `./e2e/publication_subscription_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

### Label: `LabelRecovery`
    - `./e2e/pg_basebackup_test.go`: 1 matches
    - `./e2e/pg_data_corruption_test.go`: 1 matches

**Summary:** Found in 2 files with 2 total occurrences.

### Label: `LabelReplication`
    - `./e2e/replication_slot_test.go`: 1 matches
    - `./e2e/replica_mode_cluster_test.go`: 2 matches
    - `./e2e/scaling_test.go`: 1 matches
    - `./e2e/syncreplicas_test.go`: 1 matches

**Summary:** Found in 4 files with 5 total occurrences.

### Label: `LabelSecurity`
    - `./e2e/apparmor_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

### Label: `LabelSelfHealing`
    - `./e2e/switchover_test.go`: 1 matches
    - `./e2e/fastswitchover_test.go`: 1 matches
    - `./e2e/pvc_deletion_test.go`: 1 matches
    - `./e2e/fastfailover_test.go`: 1 matches
    - `./e2e/failover_test.go`: 1 matches

**Summary:** Found in 5 files with 5 total occurrences.

### Label: `LabelServiceConnectivity`
    - `./e2e/update_user_test.go`: 2 matches
    - `./e2e/pgbouncer_types_test.go`: 1 matches
    - `./e2e/connection_test.go`: 1 matches
    - `./e2e/pgbouncer_test.go`: 1 matches
    - `./e2e/certificates_test.go`: 5 matches

**Summary:** Found in 5 files with 10 total occurrences.

### Label: `LabelSmoke`
    - `./e2e/managed_roles_test.go`: 1 matches
    - `./e2e/pod_patch_test.go`: 1 matches
    - `./e2e/managed_services_test.go`: 1 matches
    - `./e2e/initdb_test.go`: 1 matches
    - `./e2e/tablespaces_test.go`: 1 matches
    - `./e2e/cluster_setup_test.go`: 1 matches
    - `./e2e/declarative_database_management_test.go`: 1 matches

**Summary:** Found in 7 files with 7 total occurrences.

### Label: `LabelSnapshot`
    - `./e2e/tablespaces_test.go`: 1 matches
    - `./e2e/volume_snapshot_test.go`: 1 matches

**Summary:** Found in 2 files with 2 total occurrences.

### Label: `LabelStorage`
    - `./e2e/tablespaces_test.go`: 1 matches
    - `./e2e/disk_space_test.go`: 1 matches
    - `./e2e/pg_wal_volume_test.go`: 1 matches
    - `./e2e/storage_expansion_test.go`: 1 matches
    - `./e2e/volume_snapshot_test.go`: 1 matches

**Summary:** Found in 5 files with 5 total occurrences.

### Label: `LabelTablespaces`
    - `./e2e/tablespaces_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

### Label: `LabelUpgrade`
    - `./e2e/upgrade_test.go`: 1 matches

**Summary:** Found in 1 files with 1 total occurrences.

---
## 3. Detailed Breakdown by File
### File: `./e2e/affinity_test.go`
    - `LabelPodScheduling`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/apparmor_test.go`
    - `LabelNoOpenshift`: 1 matches
    - `LabelSecurity`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/architecture_test.go`
    - `LabelBasic`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/backup_restore_azure_test.go`
    - `LabelBackupRestore`: 2 matches

**Summary:** Contains 1 unique labels with 2 total occurrences.

### File: `./e2e/backup_restore_azurite_test.go`
    - `LabelBackupRestore`: 2 matches

**Summary:** Contains 1 unique labels with 2 total occurrences.

### File: `./e2e/backup_restore_minio_test.go`
    - `LabelBackupRestore`: 2 matches

**Summary:** Contains 1 unique labels with 2 total occurrences.

### File: `./e2e/certificates_test.go`
    - `LabelPlugin`: 2 matches
    - `LabelServiceConnectivity`: 5 matches

**Summary:** Contains 2 unique labels with 7 total occurrences.

### File: `./e2e/cluster_major_upgrade_test.go`
    - `LabelPostgresMajorUpgrade`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/cluster_microservice_test.go`
    - `LabelImportingDatabases`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/cluster_monolithic_test.go`
    - `LabelImportingDatabases`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/cluster_setup_test.go`
    - `LabelBasic`: 1 matches
    - `LabelSmoke`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/config_support_test.go`
    - `LabelClusterMetadata`: 1 matches
    - `LabelDisruptive`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/configuration_update_test.go`
    - `LabelClusterMetadata`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/connection_test.go`
    - `LabelServiceConnectivity`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/declarative_database_management_test.go`
    - `LabelBasic`: 1 matches
    - `LabelDeclarativeDatabases`: 1 matches
    - `LabelSmoke`: 1 matches

**Summary:** Contains 3 unique labels with 3 total occurrences.

### File: `./e2e/disk_space_test.go`
    - `LabelStorage`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/drain_node_test.go`
    - `LabelDisruptive`: 1 matches
    - `LabelMaintenance`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/eviction_test.go`
    - `LabelDisruptive`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/failover_test.go`
    - `LabelSelfHealing`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/fastfailover_test.go`
    - `LabelPerformance`: 1 matches
    - `LabelSelfHealing`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/fastswitchover_test.go`
    - `LabelPerformance`: 1 matches
    - `LabelSelfHealing`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/fencing_test.go`
    - `LabelPlugin`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/imagevolume_extensions_test.go`
    - `LabelImageVolumeExtensions`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/initdb_test.go`
    - `LabelBasic`: 1 matches
    - `LabelSmoke`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/logs_test.go`
    - `LabelObservability`: 2 matches

**Summary:** Contains 1 unique labels with 2 total occurrences.

### File: `./e2e/managed_roles_test.go`
    - `LabelBasic`: 1 matches
    - `LabelSmoke`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/managed_services_test.go`
    - `LabelBasic`: 1 matches
    - `LabelSmoke`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/metrics_test.go`
    - `LabelObservability`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/monitoring_test.go`
    - `LabelObservability`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/nodeselector_test.go`
    - `LabelPodScheduling`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/operator_deployment_test.go`
    - `LabelBasic`: 1 matches
    - `LabelOperator`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/operator_ha_test.go`
    - `LabelDisruptive`: 1 matches
    - `LabelOperator`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/operator_unavailable_test.go`
    - `LabelDisruptive`: 1 matches
    - `LabelOperator`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/pg_basebackup_test.go`
    - `LabelRecovery`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/pg_data_corruption_test.go`
    - `LabelRecovery`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/pg_wal_volume_test.go`
    - `LabelStorage`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/pgbouncer_metrics_test.go`
    - `LabelObservability`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/pgbouncer_test.go`
    - `LabelServiceConnectivity`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/pgbouncer_types_test.go`
    - `LabelServiceConnectivity`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/pod_patch_test.go`
    - `LabelBasic`: 1 matches
    - `LabelSmoke`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/probes_test.go`
    - `LabelBasic`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/publication_subscription_test.go`
    - `LabelPublicationSubscription`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/pvc_deletion_test.go`
    - `LabelSelfHealing`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/replica_mode_cluster_test.go`
    - `LabelBackupRestore`: 3 matches
    - `LabelReplication`: 2 matches

**Summary:** Contains 2 unique labels with 5 total occurrences.

### File: `./e2e/replication_slot_test.go`
    - `LabelReplication`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/rolling_update_test.go`
    - `LabelPostgresConfiguration`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/scaling_test.go`
    - `LabelReplication`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/self_fencing_test.go`
    - `LabelDisruptive`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/storage_expansion_test.go`
    - `LabelStorage`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/switchover_test.go`
    - `LabelSelfHealing`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/syncreplicas_test.go`
    - `LabelReplication`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/tablespaces_test.go`
    - `LabelBackupRestore`: 1 matches
    - `LabelBasic`: 1 matches
    - `LabelSmoke`: 1 matches
    - `LabelSnapshot`: 1 matches
    - `LabelStorage`: 1 matches
    - `LabelTablespaces`: 1 matches

**Summary:** Contains 6 unique labels with 6 total occurrences.

### File: `./e2e/tolerations_test.go`
    - `LabelDisruptive`: 1 matches
    - `LabelPodScheduling`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/update_user_test.go`
    - `LabelServiceConnectivity`: 2 matches

**Summary:** Contains 1 unique labels with 2 total occurrences.

### File: `./e2e/upgrade_test.go`
    - `LabelNoOpenshift`: 1 matches
    - `LabelUpgrade`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

### File: `./e2e/volume_snapshot_test.go`
    - `LabelBackupRestore`: 1 matches
    - `LabelSnapshot`: 1 matches
    - `LabelStorage`: 1 matches

**Summary:** Contains 3 unique labels with 3 total occurrences.

### File: `./e2e/wal_restore_parallel_test.go`
    - `LabelBackupRestore`: 1 matches

**Summary:** Contains 1 unique labels with 1 total occurrences.

### File: `./e2e/webhook_test.go`
    - `LabelDisruptive`: 1 matches
    - `LabelOperator`: 1 matches

**Summary:** Contains 2 unique labels with 2 total occurrences.

