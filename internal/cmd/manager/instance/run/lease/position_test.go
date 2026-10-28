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

package lease

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The position published when the primary lease is released", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
		previous    = `{"lsn":"0/1000000","timeline":2,"state":"in production","pod":"test-cluster-2"}`
	)

	controldata := func(state, redo, timeline string) string {
		return fmt.Sprintf(`pg_control version number:            1700
Database system identifier:           7421354329382109876
Database cluster state:               %s
Latest checkpoint location:           0/7000028
Latest checkpoint's REDO location:    %s
Latest checkpoint's REDO WAL file:    000000030000000000000007
Latest checkpoint's TimeLineID:       %s
Bytes per WAL segment:                16777216
`, state, redo, timeline)
	}

	var (
		kubeClient *fake.Clientset
		r          *Runnable
	)

	BeforeEach(func() {
		kubeClient = fake.NewClientset()
		r = New(kubeClient, postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName))
	})

	// release creates a lease held by this pod that carries the previous
	// annotation, releases it and returns the annotation that is left, nil when
	// it is not there
	release := func(ctx context.Context) *utils.LeasePosition {
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   namespace,
				Name:        clusterName,
				Annotations: map[string]string{utils.LeasePositionAnnotationName: previous},
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       ptr.To(thisPod),
				LeaseDurationSeconds: ptr.To(int32(15)),
			},
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Expect(r.Release(ctx)).To(Succeed())

		lease, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(lease.Spec.HolderIdentity).To(HaveValue(BeEmpty()))

		value, ok := lease.Annotations[utils.LeasePositionAnnotationName]
		Expect(ok).To(BeTrue(), "the annotation must never be removed")
		if value == previous {
			return nil
		}
		position, err := utils.ParseLeasePosition(value)
		Expect(err).ToNot(HaveOccurred())
		return position
	}

	DescribeTable("publishes what pg_controldata reports",
		func(ctx context.Context, state utils.PgDataState) {
			r.readControldata = func() (string, error) {
				return controldata(string(state), "0/7000028", "3"), nil
			}

			Expect(release(ctx)).To(Equal(&utils.LeasePosition{
				LSN:      "0/7000028",
				Timeline: 3,
				State:    state,
				Pod:      thisPod,
			}))
		},
		Entry("after a clean shutdown", utils.PgDataStateShutDown),
		Entry("when PostgreSQL was stopped while shutting down", utils.PgDataStateShuttingDown),
		Entry("when PostgreSQL did not stop at all", utils.PgDataStateInProduction),
	)

	DescribeTable("leaves the position of the previous primary when this instance was not one",
		func(ctx context.Context, state utils.PgDataState) {
			r.readControldata = func() (string, error) {
				return controldata(string(state), "0/7000028", "3"), nil
			}

			Expect(release(ctx)).To(BeNil())
		},
		Entry("shut down in recovery", utils.PgDataStateShutDownInRecovery),
		Entry("in archive recovery", utils.PgDataStateInArchiveRecovery),
		Entry("in crash recovery", utils.PgDataStateInCrashRecovery),
		Entry("starting up", utils.PgDataStateStartingUp),
	)

	DescribeTable("publishes an unknown position, and does not remove the annotation, when it cannot tell",
		func(ctx context.Context, output string, err error) {
			r.readControldata = func() (string, error) { return output, err }

			Expect(release(ctx)).To(Equal(&utils.LeasePosition{
				State: utils.LeasePositionStateUnknown,
				Pod:   thisPod,
			}))
		},
		Entry("pg_controldata fails", "", errors.New("exit status 1")),
		Entry("the output is empty", "", nil),
		Entry("the state is not one we know", controldata("on fire", "0/7000028", "3"), nil),
		Entry("the REDO location is missing", controldata("shut down", "", "3"), nil),
		Entry("the REDO location is not an LSN", controldata("shut down", "garbage", "3"), nil),
		Entry("the timeline is missing", controldata("shut down", "0/7000028", ""), nil),
	)
})

var _ = Describe("The position published when the primary lease is written as the holder", func() {
	const (
		namespace   = "test-ns"
		clusterName = "test-cluster"
		thisPod     = "test-cluster-1"
		previous    = `{"lsn":"0/1000000","timeline":2,"state":"shut down","pod":"test-cluster-2"}`
	)

	var (
		kubeClient *fake.Clientset
		r          *Runnable
	)

	BeforeEach(func() {
		kubeClient = fake.NewClientset()
		r = New(kubeClient, postgres.NewInstance().
			WithNamespace(namespace).
			WithPodName(thisPod).
			WithClusterName(clusterName))
	})

	heldLease := func() *coordinationv1.Lease {
		return &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   namespace,
				Name:        clusterName,
				Annotations: map[string]string{utils.LeasePositionAnnotationName: previous},
			},
			Spec: coordinationv1.LeaseSpec{HolderIdentity: ptr.To(thisPod)},
		}
	}

	DescribeTable("takes the timeline from the WAL file name",
		func(ctx context.Context, walFile string, timeline int) {
			r.queryLivePosition = func(context.Context) (string, string, error) {
				return "0/7000028", walFile, nil
			}

			Expect(r.positionSource(ctx, heldLease())).To(Equal(&utils.LeasePosition{
				LSN:      "0/7000028",
				Timeline: timeline,
				State:    utils.PgDataStateInProduction,
				Pod:      thisPod,
			}))
		},
		Entry("the first timeline", "000000010000000000000007", 1),
		Entry("a later one, in hexadecimal", "0000000A0000000000000007", 10),
	)

	DescribeTable("leaves the annotation as it is",
		func(ctx context.Context, walFile string, err error) {
			r.queryLivePosition = func(context.Context) (string, string, error) {
				return "0/7000028", walFile, err
			}

			Expect(r.positionSource(ctx, heldLease())).To(BeNil())
		},
		Entry("when this instance is not a primary", "", sql.ErrNoRows),
		Entry("when PostgreSQL cannot be reached", "", errors.New("connection refused")),
		Entry("when the WAL file name is too short", "0001", nil),
		Entry("when the WAL file name does not start with a timeline", "zzzzzzzz0000000000000007", nil),
	)

	It("keeps the position of the previous primary on the lease when not a primary", func(ctx context.Context) {
		r.queryLivePosition = func(context.Context) (string, string, error) {
			return "", "", sql.ErrNoRows
		}
		lease := heldLease()
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, lease, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// A write as the holder, as the elector does on every renewal
		_, _, err = r.lock.Get(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(r.lock.Update(ctx, resourcelock.LeaderElectionRecord{HolderIdentity: thisPod})).To(Succeed())

		stored, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.Spec.HolderIdentity).To(HaveValue(Equal(thisPod)))
		Expect(stored.Annotations).To(HaveKeyWithValue(utils.LeasePositionAnnotationName, previous))
	})

	It("publishes the position on the write of the lease", func(ctx context.Context) {
		r.queryLivePosition = func(context.Context) (string, string, error) {
			return "0/7000028", "000000030000000000000007", nil
		}
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, heldLease(), metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		_, _, err = r.lock.Get(ctx)
		Expect(err).ToNot(HaveOccurred())
		Expect(r.lock.Update(ctx, resourcelock.LeaderElectionRecord{HolderIdentity: thisPod})).To(Succeed())

		stored, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		position, err := utils.ParseLeasePosition(stored.Annotations[utils.LeasePositionAnnotationName])
		Expect(err).ToNot(HaveOccurred())
		Expect(position).To(Equal(&utils.LeasePosition{
			LSN:      "0/7000028",
			Timeline: 3,
			State:    utils.PgDataStateInProduction,
			Pod:      thisPod,
		}))
	})

	It("does not delay the write of the lease past its bound when the query hangs", func(ctx context.Context) {
		r.queryLivePosition = func(ctx context.Context) (string, string, error) {
			<-ctx.Done()
			return "", "", ctx.Err()
		}
		_, err := kubeClient.CoordinationV1().Leases(namespace).Create(ctx, heldLease(), metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		_, _, err = r.lock.Get(ctx)
		Expect(err).ToNot(HaveOccurred())
		start := time.Now()
		Expect(r.lock.Update(ctx, resourcelock.LeaderElectionRecord{HolderIdentity: thisPod})).To(Succeed())
		Expect(time.Since(start)).To(BeNumerically("<", liveQueryTimeout+time.Second))

		stored, err := kubeClient.CoordinationV1().Leases(namespace).Get(ctx, clusterName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(stored.Annotations).To(HaveKeyWithValue(utils.LeasePositionAnnotationName, previous))
	})
})
