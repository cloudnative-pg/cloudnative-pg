package persistentvolumeclaim

import (
	"fmt"

	volumesnapshot "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Meta is an object capable of describing the metadata of a pvc
type Meta interface {
	// GetName will be used to get the name of the PVC
	GetName(instanceName string) string
	// GetLabels will be used as the label value
	GetLabels(instanceName string) map[string]string
	// GetRoleName return the role name in string
	GetRoleName() string
}

// Bootstrap is an object capable of describing the starting status of a pvc
type Bootstrap interface {
	// GetInitialStatus returns the status the PVC should be first created with
	GetInitialStatus() PVCStatus
}

// Backup is an object capable of describing the backup behaviour of a pvc
type Backup interface {
	// GetSnapshotName gets the snapshot name for a certain PVC
	GetSnapshotName(backupName string) string
	// GetVolumeSnapshotClass will return the volume snapshot class to be used
	// when snapshotting a PVC with this Role.
	GetVolumeSnapshotClass(configuration *apiv1.VolumeSnapshotConfiguration) *string
}

// Configuration is an object capable of describing the configuration of a pvc
type Configuration interface {
	// GetStorageConfiguration will return the storage configuration to be used
	// for this PVC role and this cluster
	GetStorageConfiguration(cluster *apiv1.Cluster) (apiv1.StorageConfiguration, error)
	// GetSource gets the PVC source to be used when creating a new PVC
	GetSource(source *StorageSource) (*corev1.TypedLocalObjectReference, error)
}

// Calculator returns the data needed for a given pvc
type Calculator interface {
	Bootstrap
	Backup
	Configuration
	Meta
}

// GetExpectedObjectCalculator return an object capable of determining a series of data for the given pvc
func GetExpectedObjectCalculator(labels map[string]string) (Calculator, error) {
	roleName := labels[utils.PvcRoleLabelName]
	tbsName := labels[utils.TablespaceNameLabelName]
	switch utils.PVCRole(roleName) {
	case utils.PVCRolePgData:
		return NewPgDataCalculator(), nil
	case utils.PVCRolePgWal:
		return NewPgWalCalculator(), nil
	case utils.PVCRolePgTablespace:
		return NewPgTablespaceCalculator(tbsName), nil
	default:
		return nil, fmt.Errorf("unknown pvc role name: %s", roleName)
	}
}

// pgData describes the role of a PVC which used for pg_data
type pgData struct{}

// pgWal describes the role of a PVC which used for pg_wal
type pgWal struct{}

// pgTablespace describes the role of a PVC which used for tablespace
type pgTablespace struct {
	tablespaceName string
}

// NewPgDataCalculator returns a Calculator for a PVC of PG_DATA type
func NewPgDataCalculator() Calculator {
	return pgData{}
}

// NewPgWalCalculator returns a Calculator for a PVC of PG_WAL type
func NewPgWalCalculator() Calculator {
	return pgWal{}
}

// NewPgTablespaceCalculator returns a Calculator for a PVC of PG_TABLESPACE type
func NewPgTablespaceCalculator(tbsName string) Calculator {
	return pgTablespace{tablespaceName: tbsName}
}

// GetLabels will be used as the label value
func (r pgData) GetLabels(instanceName string) map[string]string {
	labels := map[string]string{
		utils.InstanceNameLabelName: instanceName,
		utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
	}
	return labels
}

// GetName will be used to get the name of the PVC
func (r pgData) GetName(instanceName string) string {
	return instanceName
}

// GetStorageConfiguration will return the storage configuration to be used
// for this PVC role and this cluster
func (r pgData) GetStorageConfiguration(cluster *apiv1.Cluster) (apiv1.StorageConfiguration, error) {
	return cluster.Spec.StorageConfiguration, nil
}

// GetSource gets the PVC source to be used when creating a new PVC
func (r pgData) GetSource(source *StorageSource) (*corev1.TypedLocalObjectReference, error) {
	if source == nil {
		return nil, nil
	}
	return &source.DataSource, nil
}

// GetRoleName return the role name in string
func (r pgData) GetRoleName() string {
	return string(utils.PVCRolePgData)
}

// GetInitialStatus returns the status the PVC should be first created with
func (r pgData) GetInitialStatus() PVCStatus {
	return StatusInitializing
}

// GetSnapshotName gets the snapshot name for a certain PVC
func (r pgData) GetSnapshotName(backupName string) string {
	return backupName
}

// GetVolumeSnapshotClass implements the Role interface
func (r pgData) GetVolumeSnapshotClass(configuration *apiv1.VolumeSnapshotConfiguration) *string {
	if len(configuration.ClassName) > 0 {
		return ptr.To(configuration.ClassName)
	}

	return nil
}

// GetSourceFromBackup implements the Role interface
func (r pgData) GetSourceFromBackup(backup *apiv1.Backup) *corev1.TypedLocalObjectReference {
	for _, element := range backup.Status.BackupSnapshotStatus.Elements {
		if element.Type == string(utils.PVCRolePgData) {
			return &corev1.TypedLocalObjectReference{
				APIGroup: ptr.To(volumesnapshot.GroupName),
				Kind:     apiv1.VolumeSnapshotKind,
				Name:     element.Name,
			}
		}
	}

	return nil
}

// GetLabels will be used as the label value
func (r pgWal) GetLabels(instanceName string) map[string]string {
	labels := map[string]string{
		utils.InstanceNameLabelName: instanceName,
		utils.PvcRoleLabelName:      string(utils.PVCRolePgWal),
	}
	return labels
}

// GetName will be used to get the name of the PVC
func (r pgWal) GetName(instanceName string) string {
	return instanceName + apiv1.WalArchiveVolumeSuffix
}

// GetStorageConfiguration will return the storage configuration to be used
// for this PVC role and this cluster
func (r pgWal) GetStorageConfiguration(cluster *apiv1.Cluster) (apiv1.StorageConfiguration, error) {
	return *cluster.Spec.WalStorage, nil
}

// GetSource gets the PVC source to be used when creating a new PVC
func (r pgWal) GetSource(source *StorageSource) (*corev1.TypedLocalObjectReference, error) {
	if source == nil {
		return nil, nil
	}
	if source.WALSource == nil {
		return nil, fmt.Errorf("missing StorageSource for PostgreSQL WAL (Write-Ahead Log) PVC")
	}
	return source.WALSource, nil
}

// GetRoleName return the role name in string
func (r pgWal) GetRoleName() string {
	return string(utils.PVCRolePgWal)
}

// GetInitialStatus returns the status the PVC should be first created with
func (r pgWal) GetInitialStatus() PVCStatus {
	return StatusReady
}

// GetSnapshotName gets the snapshot name for a certain PVC
func (r pgWal) GetSnapshotName(backupName string) string {
	return fmt.Sprintf("%s%s", backupName, apiv1.WalArchiveVolumeSuffix)
}

// GetVolumeSnapshotClass implements the Role interface
func (r pgWal) GetVolumeSnapshotClass(configuration *apiv1.VolumeSnapshotConfiguration) *string {
	if len(configuration.WalClassName) > 0 {
		return ptr.To(configuration.WalClassName)
	}

	if len(configuration.ClassName) > 0 {
		return ptr.To(configuration.ClassName)
	}

	return nil
}

// GetLabels will be used as the label value
func (r pgTablespace) GetLabels(instanceName string) map[string]string {
	labels := map[string]string{
		utils.InstanceNameLabelName: instanceName,
		utils.PvcRoleLabelName:      string(utils.PVCRolePgTablespace),
	}
	// we need empty check here as we don't want to impact the label filter with empty value
	if r.tablespaceName != "" {
		labels[utils.TablespaceNameLabelName] = r.tablespaceName
	}
	return labels
}

// GetName will be used to get the name of the PVC
func (r pgTablespace) GetName(instanceName string) string {
	pvcName := specs.PvcNameForTablespace(instanceName, r.tablespaceName)
	return pvcName
}

// GetStorageConfiguration will return the storage configuration to be used
// for this PVC role and this cluster
func (r pgTablespace) GetStorageConfiguration(cluster *apiv1.Cluster) (apiv1.StorageConfiguration, error) {
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
				utils.PVCRolePgTablespace,
				r.tablespaceName,
			)
	}
	return *storageConfiguration, nil
}

// GetSource gets the PVC source to be used when creating a new PVC
func (r pgTablespace) GetSource(source *StorageSource) (*corev1.TypedLocalObjectReference, error) {
	if source == nil {
		return nil, nil
	}
	if s, has := source.TablespaceSource[r.tablespaceName]; has {
		return &s, nil
	}
	return nil, fmt.Errorf("missing StorageSource for tablespace %s PVC", r.tablespaceName)
}

// GetRoleName return the role name in string
func (r pgTablespace) GetRoleName() string {
	return string(utils.PVCRolePgTablespace)
}

// GetInitialStatus returns the status the PVC should be first created with
func (r pgTablespace) GetInitialStatus() PVCStatus {
	return StatusReady
}

// GetSnapshotName gets the snapshot name for a certain PVC
func (r pgTablespace) GetSnapshotName(backupName string) string {
	return specs.SnapshotBackupNameForTablespace(backupName, r.tablespaceName)
}

// GetVolumeSnapshotClass implements the Role interface
func (r pgTablespace) GetVolumeSnapshotClass(configuration *apiv1.VolumeSnapshotConfiguration) *string {
	if className, ok := configuration.TablespaceClassName[r.tablespaceName]; ok && len(className) > 0 {
		return ptr.To(className)
	}

	if len(configuration.ClassName) > 0 {
		return ptr.To(configuration.ClassName)
	}

	return nil
}
