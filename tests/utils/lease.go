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

package utils

import (
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/controller"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetLeaderInfoFromLease gathers leader holderIdentity from the lease
func GetLeaderInfoFromLease(operatorNamespace string, env *TestingEnvironment) (string, error) {
	leaseInterface := env.Interface.CoordinationV1().Leases(operatorNamespace)
	lease, err := leaseInterface.Get(env.Ctx, controller.LeaderElectionID, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return *lease.Spec.HolderIdentity, nil
}
