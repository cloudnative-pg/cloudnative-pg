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

package postgres

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/cloudnative-pg/machinery/pkg/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
)

var podNamePool = []string{"cluster-1", "cluster-2", "cluster-3"}

// run is one randomized iteration, seeded by its index so failures replay.
type run struct {
	*rand.Rand
	seed int
}

func (t *run) bool() bool { return t.IntN(2) == 1 }

func (t *run) Fatalf(format string, args ...any) {
	GinkgoHelper()
	Fail(fmt.Sprintf("seed %d: ", t.seed) + fmt.Sprintf(format, args...))
}

func check(f func(t *run)) {
	for i := range 1000 {
		f(&run{rand.New(rand.NewPCG(uint64(i), 0)), i}) //nolint:gosec // deterministic seed, not security-sensitive
	}
}

func genPostgresqlStatus(t *run) PostgresqlStatus {
	name := podNamePool[t.IntN(len(podNamePool))]

	var statusErr error
	if t.bool() {
		statusErr = errors.New("synthetic status error")
	}

	// A tiny LSN range is deliberate: with only a handful of possible values,
	// generated instances frequently tie on LSN, which is required to reach
	// the replica-cluster tie-break at the bottom of Less. A wide range would
	// almost never collide and the interesting branch would go unexercised.
	receivedLsn := types.Int64ToLSN(t.Uint64N(4))
	replayLsn := types.Int64ToLSN(t.Uint64N(4))

	return PostgresqlStatus{
		Pod:         &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}},
		Error:       statusErr,
		IsFenced:    t.bool(),
		IsPrimary:   t.bool(),
		ReceivedLsn: receivedLsn,
		ReplayLsn:   replayLsn,
	}
}

func genPostgresqlStatusList(t *run) *PostgresqlStatusList {
	n := 2 + t.IntN(4)

	items := make([]PostgresqlStatus, n)
	for i := range items {
		items[i] = genPostgresqlStatus(t)
	}

	isReplicaCluster := t.bool()
	currentPrimary := podNamePool[t.IntN(len(podNamePool))]

	return &PostgresqlStatusList{
		Items:            items,
		IsReplicaCluster: isReplicaCluster,
		CurrentPrimary:   currentPrimary,
	}
}

func describeItem(list *PostgresqlStatusList, idx int) string {
	item := list.Items[idx]
	return fmt.Sprintf(
		"{name:%s error:%v isFenced:%v isPrimary:%v receivedLsn:%s replayLsn:%s}",
		item.Pod.Name, item.Error != nil, item.IsFenced, item.IsPrimary, item.ReceivedLsn, item.ReplayLsn,
	)
}

var _ = Describe("PostgresqlStatusList.Less", func() {
	It("forms a strict weak ordering over any generated list", func() {
		check(func(t *run) {
			list := genPostgresqlStatusList(t)
			n := len(list.Items)

			// Irreflexivity: an item never sorts before itself.
			for i := range n {
				if list.Less(i, i) {
					t.Fatalf("Less(%d, %d) is true: an item must not sort before itself (%s)",
						i, i, describeItem(list, i))
				}
			}

			// Asymmetry: Less(i, j) and Less(j, i) must not both hold.
			for i := range n {
				for j := range n {
					if i == j {
						continue
					}
					if list.Less(i, j) && list.Less(j, i) {
						t.Fatalf(
							"asymmetry violated: Less(%d, %d) and Less(%d, %d) are both true\n"+
								"  i=%s\n  j=%s\n  IsReplicaCluster=%v CurrentPrimary=%q",
							i, j, j, i, describeItem(list, i), describeItem(list, j),
							list.IsReplicaCluster, list.CurrentPrimary)
					}
				}
			}

			// Transitivity: Less(i, j) && Less(j, k) => Less(i, k).
			// This is what sort.Sort's algorithm actually depends on
			// to produce a consistent order; without it, the
			// "first item is the promotion candidate" assumption
			// made elsewhere in the codebase is not guaranteed.
			for i := range n {
				for j := range n {
					for k := range n {
						if list.Less(i, j) && list.Less(j, k) && !list.Less(i, k) {
							t.Fatalf(
								"transitivity violated: Less(%d,%d) && Less(%d,%d) but !Less(%d,%d)\n"+
									"  i=%s\n  j=%s\n  k=%s\n  IsReplicaCluster=%v CurrentPrimary=%q",
								i, j, j, k, i, k,
								describeItem(list, i), describeItem(list, j), describeItem(list, k),
								list.IsReplicaCluster, list.CurrentPrimary)
						}
					}
				}
			}
		})
	})

	// Within instances whose status we could actually reach (Error == nil),
	// a fenced instance must never be preferred over a non-fenced one: a
	// fenced instance has PostgreSQL shut down and cannot become primary.
	It("always sorts a reachable fenced instance after a reachable non-fenced one", func() {
		check(func(t *run) {
			list := genPostgresqlStatusList(t)
			n := len(list.Items)

			for i := range n {
				for j := range n {
					if i == j {
						continue
					}
					reachable := list.Items[i].Error == nil && list.Items[j].Error == nil
					// i is fenced, j is not: "fenced sorts after non-fenced"
					// means i must NOT sort before j, and j must sort before i.
					if reachable && list.Items[i].IsFenced && !list.Items[j].IsFenced {
						if list.Less(i, j) || !list.Less(j, i) {
							t.Fatalf(
								"fenced instance did not sort after non-fenced one: "+
									"Less(%d,%d)=%v Less(%d,%d)=%v\n  fenced=%s\n  non-fenced=%s",
								i, j, list.Less(i, j), j, i, list.Less(j, i),
								describeItem(list, i), describeItem(list, j))
						}
					}
				}
			}
		})
	})

	// End-to-end version of the previous property: after a real sort.Sort, the
	// instance that would be picked as promotion candidate (Items[0]) must
	// not be fenced whenever a reachable, non-fenced alternative exists
	// anywhere in the list.
	It("never sorts a fenced instance first when a reachable non-fenced instance exists", func() {
		check(func(t *run) {
			list := genPostgresqlStatusList(t)

			hasReachableNonFenced := false
			for _, item := range list.Items {
				if item.Error == nil && !item.IsFenced {
					hasReachableNonFenced = true
					break
				}
			}

			sort.Sort(list)

			if hasReachableNonFenced && list.Items[0].IsFenced {
				t.Fatalf(
					"a fenced instance was sorted first despite a reachable non-fenced instance existing: %s",
					describeItem(list, 0))
			}
		})
	})
})
