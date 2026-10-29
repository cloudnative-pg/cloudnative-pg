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

package utils

import (
	"context"
	"fmt"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CheckClusterWideAccess tells, through SelfSubjectAccessReviews, whether the
// caller can use every passed verb on every passed resource of the API group
// cluster-wide: in every namespace for a namespaced resource. When it cannot,
// it returns a description of the first missing permission. Creating a
// SelfSubjectAccessReview needs no specific permission.
func CheckClusterWideAccess(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	group string,
	resources []string,
	verbs []string,
) (allowed bool, reason string, err error) {
	for _, resource := range resources {
		for _, verb := range verbs {
			review, err := kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(
				ctx,
				&authorizationv1.SelfSubjectAccessReview{
					Spec: authorizationv1.SelfSubjectAccessReviewSpec{
						ResourceAttributes: &authorizationv1.ResourceAttributes{
							// An empty namespace means every namespace
							Namespace: "",
							Verb:      verb,
							Group:     group,
							Resource:  resource,
						},
					},
				},
				metav1.CreateOptions{},
			)
			if err != nil {
				return false, "", fmt.Errorf("while checking the permission to %s %s.%s cluster-wide: %w",
					verb, resource, group, err)
			}

			if !review.Status.Allowed {
				reason := fmt.Sprintf("cannot %s %s.%s cluster-wide", verb, resource, group)
				if review.Status.Reason != "" {
					reason = fmt.Sprintf("%s: %s", reason, review.Status.Reason)
				}
				return false, reason, nil
			}
		}
	}

	return true, "", nil
}
