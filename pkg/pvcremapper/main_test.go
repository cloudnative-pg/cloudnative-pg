package pvcremapper

import (
	"fmt"
	"testing"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ns = "myns"
)

func testPVC(instNum int, pvcRole utils.PVCRole) corev1.PersistentVolumeClaim {
	instName := fmt.Sprintf("instance%d", instNum)
	pvcName := instName
	if pvcRole == utils.PVCRolePgWal {
		pvcName += "-wal"
	} else if pvcRole == utils.PVCRolePgTablespace {
		pvcName += "-tbcs"
	}
	pvName := fmt.Sprintf("myvol%d", instNum)

	return corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: ns,
			Labels: map[string]string{
				utils.PvcRoleLabelName:      string(pvcRole),
				utils.InstanceNameLabelName: instName,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: pvName,
		},
	}

}

func TestInstancePVCFromPVC(t *testing.T) {
	configuration.Current = configuration.NewConfiguration()
	configuration.Current.DataVolumeSuffix = "-data"
	for _, instNum := range []int{0, 1, 2} {
		instName := fmt.Sprintf("instance%d", instNum)
		pvcRole := string(utils.PVCRolePgData)
		pvcName := instName
		pvName := fmt.Sprintf("myvol%d", instNum)

		pvc := testPVC(instNum, utils.PVCRolePgData)
		ipvc, err := InstancePVCFromPVC(pvc)
		assert.NoError(t, err)
		assert.Equal(t, ipvc.namespace, ns)
		assert.Equal(t, ipvc.podName, instName)
		assert.Equal(t, ipvc.PvName(), pvName)
		assert.Equal(t, ipvc.Name(), pvcName)
		assert.Equal(t, ipvc.role, pvcRole)
		assert.Equal(t,
			ipvc.AsNamespacedName(),
			types.NamespacedName{
				Namespace: ns,
				Name:      pvcName,
			},
		)
	}
}

func TestInstancePVCFromPVCIssues(t *testing.T) {
	const instNum = 3
	for _, labelKey := range []string{utils.PvcRoleLabelName, utils.InstanceNameLabelName} {
		pvc := testPVC(instNum, utils.PVCRolePgData)
		delete(pvc.ObjectMeta.Labels, labelKey)
		_, err := InstancePVCsFromPVCs([]corev1.PersistentVolumeClaim{pvc})
		assert.Error(t, err)
	}
}

func TestRemapRequired(t *testing.T) {
	for _, pvcRole := range []utils.PVCRole{
		utils.PVCRolePgData,
		utils.PVCRolePgWal,
		utils.PVCRolePgTablespace,
	} {
		configuration.Current = configuration.NewConfiguration()
		const instNum = 1
		pvc := testPVC(instNum, pvcRole)
		instName := fmt.Sprintf("instance%d", instNum)

		ipvc, err := InstancePVCFromPVC(pvc)
		require.NoError(t, err)
		if pvcRole == utils.PVCRolePgData {
			assert.False(t, ipvc.RemapRequired())
			assert.Equal(t, instName, ipvc.ExpectedName())
			configuration.Current.DataVolumeSuffix = "-data"
			assert.Equal(t, instName+"-data", ipvc.ExpectedName())
			assert.True(t, ipvc.RemapRequired())
		} else if pvcRole == utils.PVCRolePgWal {
			assert.False(t, ipvc.RemapRequired())
			assert.Equal(t, instName+"-wal", ipvc.ExpectedName())
			configuration.Current.WalArchiveVolumeSuffix = ""
			assert.Equal(t, instName, ipvc.ExpectedName())
			assert.True(t, ipvc.RemapRequired())
		} else if pvcRole == utils.PVCRolePgTablespace {
			assert.False(t, ipvc.RemapRequired())
			assert.Equal(t, instName+"-tbcs", ipvc.ExpectedName())
		}
	}
}
func TestInstancePVCsFromPVCs(t *testing.T) {
	var pvcs []corev1.PersistentVolumeClaim
	instances := make(map[string]bool)
	for _, instNum := range []int{0, 1, 2} {
		instances[fmt.Sprintf("instance%d", instNum)] = true
		for _, pvcRole := range []utils.PVCRole{
			utils.PVCRolePgData,
			utils.PVCRolePgWal,
			utils.PVCRolePgTablespace,
		} {
			pvc := testPVC(instNum, pvcRole)
			pvcs = append(pvcs, pvc)
		}
	}

	ipvcs, err := InstancePVCsFromPVCs(pvcs)
	assert.NoError(t, err)
	assert.Len(t, ipvcs, 9)
	assert.Equal(t, instances, ipvcs.Instances())

	const inst0 = "instance0"
	ipvcsInstance0 := ipvcs.ForInstance(inst0)
	assert.Len(t, ipvcsInstance0, 3)
	assert.Equal(t, map[string]bool{inst0: true}, ipvcsInstance0.Instances())

	configuration.Current = configuration.NewConfiguration()
	ipvcsRemapRequired := ipvcs.RemapRequired()
	assert.Len(t, ipvcsRemapRequired, 0)
	configuration.Current.WalArchiveVolumeSuffix = ""
	ipvcsRemapRequired2 := ipvcs.RemapRequired()
	assert.Len(t, ipvcsRemapRequired2, 3)
}
