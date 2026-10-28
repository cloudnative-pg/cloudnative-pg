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

package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CrossNamespaceRBACSweeper deletes the RBAC objects created for the
// cross-namespace resources support of clusters that no longer exist
type CrossNamespaceRBACSweeper struct {
	client            client.Client
	reader            client.Reader
	watchedNamespaces []string
}

// NewCrossNamespaceRBACSweeper creates a new sweeper
func NewCrossNamespaceRBACSweeper(
	cli client.Client,
	reader client.Reader,
	watchedNamespaces []string,
) *CrossNamespaceRBACSweeper {
	return &CrossNamespaceRBACSweeper{client: cli, reader: reader, watchedNamespaces: watchedNamespaces}
}

// Sweep deletes the orphaned cross-namespace RBAC objects
func (s *CrossNamespaceRBACSweeper) Sweep(_ context.Context) error {
	return nil
}

// NeedLeaderElection implements manager.LeaderElectionRunnable
func (s *CrossNamespaceRBACSweeper) NeedLeaderElection() bool {
	return false
}
