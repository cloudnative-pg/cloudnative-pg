package persistentvolumeclaim

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PVCRole is the common interface for all PVC roles
type PVCRole interface {
	// GetLabels will be used as the label value
	GetLabels(instanceName string) map[string]string
	// GetPVCName will be used to get the name of the PVC
	GetPVCName(instanceName string) string
	// GetStorageConfiguration will return the storage configuration to be used
	// for this PVC role and this cluster
	GetStorageConfiguration(cluster *apiv1.Cluster) (apiv1.StorageConfiguration, error)
	// GetSource gets the PVC source to be used when creating a new PVC
	GetSource(source *StorageSource) (*corev1.TypedLocalObjectReference, error)
	// GetRoleName return the role name in string
	GetRoleName() string
	// GetInitialStatus returns the status the PVC should be first created with
	GetInitialStatus() PVCStatus
	// GetSnapshotName gets the snapshot name for a certain PVC
	GetSnapshotName(backupName string) string
}

// GetPVCRole return pvcRole based on the roleName given
func GetPVCRole(labels map[string]string) (PVCRole, error) {
	roleName := labels[utils.PvcRoleLabelName]
	tbsName := labels[utils.TablespaceNameLabelName]
	switch utils.PVCRoleValue(roleName) {
	case utils.PVCRoleValueData:
		return PgData{}, nil
	case utils.PVCRoleValueWal:
		return PgWal{}, nil
	case utils.PVCRoleValueTablespace:
		return PgTablespace{tablespaceName: tbsName}, nil
	default:
		return nil, fmt.Errorf("unknown pvc role name: %s", roleName)
	}
}

// PgData describes the role of a PVC which used for pg_data
type PgData struct{}

// PgWal describes the role of a PVC which used for pg_wal
type PgWal struct{}

// PgTablespace describes the role of a PVC which used for tablespace
type PgTablespace struct {
	tablespaceName string
}

// GetLabels will be used as the label value
func (r PgData) GetLabels(instanceName string) map[string]string {
	labels := map[string]string{
		utils.InstanceNameLabelName: instanceName,
		utils.PvcRoleLabelName:      string(utils.PVCRoleValueData),
	}
	return labels
}

// GetPVCName will be used to get the name of the PVC
func (r PgData) GetPVCName(instanceName string) string {
	return instanceName
}

// GetStorageConfiguration will return the storage configuration to be used
// for this PVC role and this cluster
func (r PgData) GetStorageConfiguration(cluster *apiv1.Cluster) (apiv1.StorageConfiguration, error) {
	return cluster.Spec.StorageConfiguration, nil
}

// GetSource gets the PVC source to be used when creating a new PVC
func (r PgData) GetSource(source *StorageSource) (*corev1.TypedLocalObjectReference, error) {
	if source == nil {
		return nil, nil
	}
	return &source.DataSource, nil
}

// GetRoleName return the role name in string
func (r PgData) GetRoleName() string {
	return string(utils.PVCRoleValueData)
}

// GetInitialStatus returns the status the PVC should be first created with
func (r PgData) GetInitialStatus() PVCStatus {
	return StatusInitializing
}

// GetSnapshotName gets the snapshot name for a certain PVC
func (r PgData) GetSnapshotName(backupName string) string {
	return backupName
}

// GetLabels will be used as the label value
func (r PgWal) GetLabels(instanceName string) map[string]string {
	labels := map[string]string{
		utils.InstanceNameLabelName: instanceName,
		utils.PvcRoleLabelName:      string(utils.PVCRoleValueWal),
	}
	return labels
}

// GetPVCName will be used to get the name of the PVC
func (r PgWal) GetPVCName(instanceName string) string {
	return instanceName + apiv1.WalArchiveVolumeSuffix
}

// GetStorageConfiguration will return the storage configuration to be used
// for this PVC role and this cluster
func (r PgWal) GetStorageConfiguration(cluster *apiv1.Cluster) (apiv1.StorageConfiguration, error) {
	return *cluster.Spec.WalStorage, nil
}

// GetSource gets the PVC source to be used when creating a new PVC
func (r PgWal) GetSource(source *StorageSource) (*corev1.TypedLocalObjectReference, error) {
	if source == nil {
		return nil, nil
	}
	if source.WALSource == nil {
		return nil, fmt.Errorf("missing StorageSource for PostgreSQL WAL (Write-Ahead Log) PVC")
	}
	return source.WALSource, nil
}

// GetRoleName return the role name in string
func (r PgWal) GetRoleName() string {
	return string(utils.PVCRoleValueWal)
}

// GetInitialStatus returns the status the PVC should be first created with
func (r PgWal) GetInitialStatus() PVCStatus {
	return StatusReady
}

// GetSnapshotName gets the snapshot name for a certain PVC
func (r PgWal) GetSnapshotName(backupName string) string {
	return fmt.Sprintf("%s%s", backupName, apiv1.WalArchiveVolumeSuffix)
}

// GetLabels will be used as the label value
func (r PgTablespace) GetLabels(instanceName string) map[string]string {
	labels := map[string]string{
		utils.InstanceNameLabelName: instanceName,
		utils.PvcRoleLabelName:      string(utils.PVCRoleValueTablespace),
	}
	// we need empty check here as we don't want to impact the label filter with empty value
	if r.tablespaceName != "" {
		labels[utils.TablespaceNameLabelName] = r.tablespaceName
	}
	return labels
}

// GetPVCName will be used to get the name of the PVC
func (r PgTablespace) GetPVCName(instanceName string) string {
	pvcName := specs.PvcNameForTablespace(instanceName, r.tablespaceName)
	return pvcName
}

// GetStorageConfiguration will return the storage configuration to be used
// for this PVC role and this cluster
func (r PgTablespace) GetStorageConfiguration(cluster *apiv1.Cluster) (apiv1.StorageConfiguration, error) {
	var storageConfiguration *apiv1.StorageConfiguration
	for tbsName, config := range cluster.Spec.Tablespaces {
		config := config
		if tbsName == r.tablespaceName {
			storageConfiguration = &config.Storage
			break
		}
	}
	if storageConfiguration == nil {
		return apiv1.StorageConfiguration{},
			fmt.Errorf(
				"storage configuration doesn't exist for the given PVC role: %s and label %s",
				utils.PVCRoleValueTablespace,
				r.tablespaceName,
			)
	}
	return *storageConfiguration, nil
}

// GetSource gets the PVC source to be used when creating a new PVC
func (r PgTablespace) GetSource(source *StorageSource) (*corev1.TypedLocalObjectReference, error) {
	if source == nil {
		return nil, nil
	}
	if s, has := source.TablespaceSource[r.tablespaceName]; has {
		return &s, nil
	}
	return nil, fmt.Errorf("missing StorageSource for tablespace %s PVC", r.tablespaceName)
}

// GetRoleName return the role name in string
func (r PgTablespace) GetRoleName() string {
	return string(utils.PVCRoleValueTablespace)
}

// GetInitialStatus returns the status the PVC should be first created with
func (r PgTablespace) GetInitialStatus() PVCStatus {
	return StatusReady
}

// GetSnapshotName gets the snapshot name for a certain PVC
func (r PgTablespace) GetSnapshotName(backupName string) string {
	return specs.SnapshotBackupNameForTablespace(backupName, r.tablespaceName)
}
