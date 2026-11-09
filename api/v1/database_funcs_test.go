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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Database GetClusterNamespace", func() {
	It("returns the ClusterRef.Namespace when set", func() {
		db := &Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: DatabaseSpec{
				ClusterRef: ClusterObjectReference{
					Name:      "my-cluster",
					Namespace: "cluster-namespace",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		Expect(db.GetClusterNamespace()).To(Equal("cluster-namespace"))
	})

	It("returns the Database's namespace when ClusterRef.Namespace is empty", func() {
		db := &Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: DatabaseSpec{
				ClusterRef: ClusterObjectReference{
					Name: "my-cluster",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		Expect(db.GetClusterNamespace()).To(Equal("app-namespace"))
	})
})

var _ = Describe("Database IsCrossNamespace", func() {
	It("returns true when ClusterRef.Namespace differs from Database's namespace", func() {
		db := &Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: DatabaseSpec{
				ClusterRef: ClusterObjectReference{
					Name:      "my-cluster",
					Namespace: "cluster-namespace",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		Expect(db.IsCrossNamespace()).To(BeTrue())
	})

	It("returns false when ClusterRef.Namespace equals Database's namespace", func() {
		db := &Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "same-namespace",
			},
			Spec: DatabaseSpec{
				ClusterRef: ClusterObjectReference{
					Name:      "my-cluster",
					Namespace: "same-namespace",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		Expect(db.IsCrossNamespace()).To(BeFalse())
	})

	It("returns false when ClusterRef.Namespace is empty", func() {
		db := &Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: DatabaseSpec{
				ClusterRef: ClusterObjectReference{
					Name: "my-cluster",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		Expect(db.IsCrossNamespace()).To(BeFalse())
	})
})

var _ = Describe("Database GetClusterRef", func() {
	It("returns the ClusterObjectReference with both name and namespace", func() {
		db := &Database{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mydb",
				Namespace: "app-namespace",
			},
			Spec: DatabaseSpec{
				ClusterRef: ClusterObjectReference{
					Name:      "my-cluster",
					Namespace: "cluster-namespace",
				},
				Name:  "mydb",
				Owner: "app",
			},
		}

		ref := db.GetClusterRef()
		Expect(ref.Name).To(Equal("my-cluster"))
		Expect(ref.Namespace).To(Equal("cluster-namespace"))
	})
})
