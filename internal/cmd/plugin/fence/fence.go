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

// Package fence implements a command to fence instances in a cluster
package fence

import (
	"context"
	"fmt"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// fencingOn marks an instance in a cluster as fenced
func fencingOn(ctx context.Context, clusterName string, serverName string) error {
	err := utils.NewFencingBuilder(plugin.Client, clusterName, plugin.Namespace).
		Add().
		Instance(serverName).
		Execute(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%s fenced\n", serverName)
	return nil
}

// fencingOff marks an instance in a cluster as not fenced
func fencingOff(ctx context.Context, clusterName string, serverName string) error {
	err := utils.NewFencingBuilder(plugin.Client, clusterName, plugin.Namespace).
		Remove().
		Instance(serverName).
		Execute(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%s unfenced\n", serverName)
	return nil
}
