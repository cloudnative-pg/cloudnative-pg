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

package tablespaces

import (
	"context"
	"fmt"
	"slices"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/controller/tablespaces/infrastructure"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mockTablespaceManager struct {
	tablespaces map[string]infrastructure.Tablespace
	callHistory []string
}

func (m *mockTablespaceManager) List(_ context.Context) ([]infrastructure.Tablespace, error) {
	m.callHistory = append(m.callHistory, "list")
	re := make([]infrastructure.Tablespace, len(m.tablespaces))
	i := 0
	for _, r := range m.tablespaces {
		re[i] = r
		i++
	}
	return re, nil
}

func (m *mockTablespaceManager) Update(
	_ context.Context, _ infrastructure.Tablespace,
) error {
	return fmt.Errorf("not in use yet")
}

func (m *mockTablespaceManager) Create(
	_ context.Context, tablespace infrastructure.Tablespace,
) error {
	m.callHistory = append(m.callHistory, "create")
	_, found := m.tablespaces[tablespace.Name]
	if found {
		return fmt.Errorf("trying to create existing tablespace: %s", tablespace.Name)
	}
	m.tablespaces[tablespace.Name] = tablespace
	return nil
}

type mockTablespaceStorageManager struct {
	unavailableStorageLocations []string
}

func (mst mockTablespaceStorageManager) storageExists(tablespaceName string) (bool, error) {
	return !slices.Contains(
		mst.unavailableStorageLocations,
		mst.getStorageLocation(tablespaceName),
	), nil
}

func (mst mockTablespaceStorageManager) getStorageLocation(tablespaceName string) string {
	return fmt.Sprintf("/%s", tablespaceName)
}

var _ = Describe("Tablespace synchronizer tests", func() {
	tablespaceReconciler := TablespaceReconciler{
		instance: &postgres.Instance{
			Namespace: "myPod",
		},
	}

	When("tablespace configurations are realizable", func() {
		It("will do nothing if the DB contains the tablespaces in spec", func(ctx context.Context) {
			tablespacesSpec := map[string]apiv1.TablespaceConfiguration{
				"foo": {
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
			}
			tbsManager := mockTablespaceManager{
				tablespaces: map[string]infrastructure.Tablespace{
					"foo": {
						Name: "foo",
					},
				},
			}
			tbsInDatabase, err := tbsManager.List(ctx)
			Expect(err).ShouldNot(HaveOccurred())
			tbsByAction := evaluateNextActions(ctx, tbsInDatabase, tablespacesSpec)
			result, err := tablespaceReconciler.applyTablespaceActions(ctx, &tbsManager,
				mockTablespaceStorageManager{}, tbsByAction)
			Expect(result).To(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(tbsManager.callHistory).To(HaveLen(1))
			Expect(tbsManager.callHistory).To(ConsistOf("list"))
		})

		It("will create a tablespace in spec that is missing from DB", func(ctx context.Context) {
			tablespacesSpec := map[string]apiv1.TablespaceConfiguration{
				"foo": {
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
				"bar": {
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
			}
			tbsManager := mockTablespaceManager{
				tablespaces: map[string]infrastructure.Tablespace{
					"foo": {
						Name: "foo",
					},
				},
			}
			tbsInDatabase, err := tbsManager.List(ctx)
			Expect(err).ShouldNot(HaveOccurred())
			tbsByAction := evaluateNextActions(ctx, tbsInDatabase, tablespacesSpec)
			result, err := tablespaceReconciler.applyTablespaceActions(ctx, &tbsManager,
				mockTablespaceStorageManager{}, tbsByAction)
			Expect(result).To(BeNil())
			Expect(err).ShouldNot(HaveOccurred())
			Expect(tbsManager.callHistory).To(HaveLen(2))
			Expect(tbsManager.callHistory).To(ConsistOf("list", "create"))
		})

		It("will requeue the tablespace creation if the mount path doesn't exist", func(ctx context.Context) {
			tablespacesSpec := map[string]apiv1.TablespaceConfiguration{
				"foo": {
					Storage: apiv1.StorageConfiguration{
						Size: "1Gi",
					},
				},
			}
			tbsManager := mockTablespaceManager{}
			tbsInDatabase, err := tbsManager.List(ctx)
			Expect(err).ShouldNot(HaveOccurred())
			tbsByAction := evaluateNextActions(ctx, tbsInDatabase, tablespacesSpec)
			result, err := tablespaceReconciler.applyTablespaceActions(ctx, &tbsManager,
				mockTablespaceStorageManager{
					unavailableStorageLocations: []string{
						"/foo",
					},
				}, tbsByAction)
			Expect(result).To(Not(BeNil()))
			Expect(result.RequeueAfter).To(BeNumerically(">", 0))
			Expect(err).ShouldNot(HaveOccurred())
			Expect(tbsManager.callHistory).To(HaveLen(1))
			Expect(tbsManager.callHistory).To(ConsistOf("list"))
		})
	})
})
