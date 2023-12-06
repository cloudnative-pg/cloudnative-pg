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

package volumesnapshot

import (
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeBackupClient struct {
	startCalled bool
	stopCalled  bool
	injectError error
	response    *webserver.Response[webserver.BackupResultData]
}

func (f *fakeBackupClient) StatusWithErrors(
	_ context.Context,
	_ string,
) (*webserver.Response[webserver.BackupResultData], error) {
	return f.response, f.injectError
}

func (f *fakeBackupClient) Start(_ context.Context, _ string, _ webserver.StartBackupRequest) error {
	f.startCalled = true
	return f.injectError
}

func (f *fakeBackupClient) Stop(_ context.Context, _ string, _ webserver.StopBackupRequest) error {
	f.stopCalled = true
	return f.injectError
}

var _ = Describe("onlineExecutor", func() {
	var (
		ctx     context.Context
		cluster *apiv1.Cluster
		backup  *apiv1.Backup
		target  *corev1.Pod
	)
	BeforeEach(func() {
		ctx = context.TODO()
		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "online-backup",
				Namespace: "test-namespace",
			},
		}
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-namespace",
			},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{VolumeSnapshot: &apiv1.VolumeSnapshotConfiguration{
					ClassName:           "vs-test",
					Online:              ptr.To(true),
					OnlineConfiguration: apiv1.OnlineConfiguration{},
				}},
			},
			Status: apiv1.ClusterStatus{},
		}
		target = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-pod",
			},
			Status: corev1.PodStatus{
				PodIP: "0.0.0.0",
			},
		}
	})

	It("should stop when encountering an error", func() {
		expectedErr := errors.New("test-error")
		onlineExec := onlineExecutor{backupClient: &fakeBackupClient{
			injectError: expectedErr,
		}}

		_, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err.Error()).To(ContainSubstring(expectedErr.Error()))
	})

	It("should stop when encountering an error inside the body", func() {
		const expectedErr = "ERROR_CODE"
		onlineExec := onlineExecutor{backupClient: &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Error: &webserver.Error{
					Code: expectedErr,
				},
			},
		}}

		_, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err.Error()).To(ContainSubstring(expectedErr))
	})

	It("should start the backup if the current backup doesn't match", func() {
		fakeBackupClient := &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Data: &webserver.BackupResultData{
					BackupName: "not-correct-backup",
				},
			},
		}
		onlineExec := onlineExecutor{backupClient: fakeBackupClient}

		res, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(fakeBackupClient.startCalled).To(BeTrue())
	})

	It("should start the backup if the current backup doesn't match even if the body contains an error", func() {
		fakeBackupClient := &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Data: &webserver.BackupResultData{
					BackupName: "not-correct-backup",
				},
				Error: &webserver.Error{
					Code: "RANDOM_ERROR",
				},
			},
		}
		onlineExec := onlineExecutor{backupClient: fakeBackupClient}

		res, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(fakeBackupClient.startCalled).To(BeTrue())
	})

	It("should execute start when no backup is pending", func() {
		fakeBackupClient := &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Data: &webserver.BackupResultData{},
			},
		}
		onlineExec := onlineExecutor{backupClient: fakeBackupClient}

		res, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(fakeBackupClient.startCalled).To(BeTrue())
	})

	It("should do nothing if the backup is in started phase", func() {
		fakeBackupClient := &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Data: &webserver.BackupResultData{
					BackupName: backup.Name,
					Phase:      webserver.Started,
				},
			},
		}
		onlineExec := onlineExecutor{backupClient: fakeBackupClient}

		res, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeNil())
		Expect(fakeBackupClient.startCalled).To(BeFalse())
	})

	It("should requeue in starting phase", func() {
		fakeBackupClient := &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Data: &webserver.BackupResultData{
					BackupName: backup.Name,
					Phase:      webserver.Starting,
				},
			},
		}
		onlineExec := onlineExecutor{backupClient: fakeBackupClient}

		res, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(fakeBackupClient.startCalled).To(BeFalse())
	})
})
