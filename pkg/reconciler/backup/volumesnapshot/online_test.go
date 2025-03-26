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

package volumesnapshot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeBackupClient struct {
	startCalled       bool
	stopCalled        bool
	injectStatusError error
	injectStartError  error
	injectStopError   error
	response          *webserver.Response[webserver.BackupResultData]
}

func (f *fakeBackupClient) StatusWithErrors(
	_ context.Context,
	_ *corev1.Pod,
) (*webserver.Response[webserver.BackupResultData], error) {
	return f.response, f.injectStatusError
}

func (f *fakeBackupClient) Start(
	_ context.Context,
	_ *corev1.Pod,
	_ webserver.StartBackupRequest,
) (*webserver.Response[webserver.BackupResultData], error) {
	f.startCalled = true
	return &webserver.Response[webserver.BackupResultData]{
		Data: &webserver.BackupResultData{},
	}, f.injectStartError
}

func (f *fakeBackupClient) Stop(
	_ context.Context,
	_ *corev1.Pod,
	_ webserver.StopBackupRequest,
) (*webserver.Response[webserver.BackupResultData], error) {
	f.stopCalled = true
	return &webserver.Response[webserver.BackupResultData]{
		Data: &webserver.BackupResultData{},
	}, f.injectStopError
}

var _ = Describe("onlineExecutor prepare", func() {
	var (
		cluster *apiv1.Cluster
		backup  *apiv1.Backup
		target  *corev1.Pod
	)
	BeforeEach(func(_ SpecContext) {
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

	It("should stop when encountering an error", func(ctx SpecContext) {
		expectedErr := errors.New("test-error")
		onlineExec := onlineExecutor{backupClient: &fakeBackupClient{
			injectStatusError: expectedErr,
		}}

		_, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).To(MatchError(expectedErr))
	})

	It("should stop when encountering an error inside the body", func(ctx SpecContext) {
		const expectedErr = "ERROR_CODE"
		onlineExec := onlineExecutor{backupClient: &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Error: &webserver.Error{
					Code: expectedErr,
				},
			},
		}}

		_, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).To(MatchError(ContainSubstring(expectedErr)))
	})

	It("should stop when encountering an error calling start", func(ctx SpecContext) {
		expectedErr := errors.New("test-error")
		onlineExec := onlineExecutor{backupClient: &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Data: &webserver.BackupResultData{
					BackupName: "not-correct-backup",
				},
			},
			injectStartError: expectedErr,
		}}

		_, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).To(MatchError(expectedErr))
	})

	It("should return an error when encountering an unknown phase", func(ctx SpecContext) {
		const unexpectedPhase = "UNEXPECTED_PHASE"
		onlineExec := onlineExecutor{backupClient: &fakeBackupClient{
			response: &webserver.Response[webserver.BackupResultData]{
				Data: &webserver.BackupResultData{
					BackupName: backup.Name,
					Phase:      unexpectedPhase,
				},
			},
		}}

		_, err := onlineExec.prepare(ctx, cluster, backup, target)
		Expect(err).To(MatchError(ContainSubstring(unexpectedPhase)))
	})

	It("should start the backup if the current backup doesn't match", func(ctx SpecContext) {
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

	It("should start the backup if the current backup doesn't match even if the body contains an error",
		func(ctx SpecContext) {
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

	It("should execute start when no backup is pending", func(ctx SpecContext) {
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

	It("should do nothing if the backup is in started phase", func(ctx SpecContext) {
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

	It("should requeue in starting phase", func(ctx SpecContext) {
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

var _ = Describe("onlineExecutor finalize", func() {
	var (
		executor   *onlineExecutor
		backup     *apiv1.Backup
		targetPod  *corev1.Pod
		fakeClient *fakeBackupClient
	)

	BeforeEach(func(_ SpecContext) {
		executor = &onlineExecutor{}
		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "online-backup",
				Namespace: "test-namespace",
			},
		}
		targetPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-pod",
			},
			Status: corev1.PodStatus{
				PodIP: "0.0.0.0",
			},
		}
		fakeClient = &fakeBackupClient{}
		executor.backupClient = fakeClient
	})

	It("should return an error when getting status fails", func(ctx SpecContext) {
		expectedErr := errors.New("test-error")
		fakeClient.injectStatusError = expectedErr

		_, err := executor.finalize(ctx, nil, backup, targetPod)
		Expect(err).To(MatchError(fmt.Sprintf("while getting status while finalizing: %s", expectedErr)))
	})

	It("should handle backup being in the Completed phase", func(ctx SpecContext) {
		fakeBeginLSN := types.LSN("ABCDEF00")
		fakeEndLSN := types.LSN("12345678")
		fakeLabelFile := []byte("test-label")
		fakeSpcmapFile := []byte("test-spcamp")

		fakeClient.response = &webserver.Response[webserver.BackupResultData]{
			Data: &webserver.BackupResultData{
				BeginLSN:   fakeBeginLSN,
				EndLSN:     fakeEndLSN,
				LabelFile:  fakeLabelFile,
				SpcmapFile: fakeSpcmapFile,
				BackupName: backup.Name,
				Phase:      webserver.Completed,
			},
		}

		result, err := executor.finalize(ctx, nil, backup, targetPod)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeNil())
		Expect(backup.Status.TablespaceMapFile).To(Equal(fakeSpcmapFile))
		Expect(backup.Status.BackupLabelFile).To(Equal(fakeLabelFile))
		Expect(backup.Status.BeginLSN).To(BeEquivalentTo(fakeBeginLSN))
		Expect(backup.Status.EndLSN).To(BeEquivalentTo(fakeEndLSN))
	})

	It("should handle backup being in the Closing phase", func(ctx SpecContext) {
		fakeClient.response = &webserver.Response[webserver.BackupResultData]{
			Data: &webserver.BackupResultData{
				BackupName: backup.Name,
				Phase:      webserver.Closing,
			},
		}

		result, err := executor.finalize(ctx, nil, backup, targetPod)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(&ctrl.Result{RequeueAfter: time.Second * 5}))
	})

	It("should return an error when the backup name doesn't match", func(ctx SpecContext) {
		fakeClient.response = &webserver.Response[webserver.BackupResultData]{
			Data: &webserver.BackupResultData{
				BackupName: "mismatched-backup-name",
				Phase:      webserver.Started, // Adjust phase if needed
			},
		}

		_, err := executor.finalize(ctx, nil, backup, targetPod)
		expectedErr := fmt.Sprintf("trying to stop backup with name: %s, while reconciling backup with name: %s",
			"mismatched-backup-name", backup.Name)
		Expect(err).To(MatchError(expectedErr))
	})

	It("should return an error for an unexpected phase", func(ctx SpecContext) {
		fakeClient.response = &webserver.Response[webserver.BackupResultData]{
			Data: &webserver.BackupResultData{
				BackupName: backup.Name,
				Phase:      "UnexpectedPhase",
			},
		}

		_, err := executor.finalize(ctx, nil, backup, targetPod)
		expectedErr := "found the instance in an unexpected state while finalizing the backup, phase: UnexpectedPhase"
		Expect(err).To(MatchError(expectedErr))
	})
})
