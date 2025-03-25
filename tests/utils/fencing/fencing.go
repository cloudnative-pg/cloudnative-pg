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

// Package fencing provides functions to manage the fencing on cnpg clusters
package fencing

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
)

// Method will be one of the supported ways to trigger an instance fencing
type Method string

const (
	// UsingAnnotation it is a keyword to use while fencing on/off the instances using annotation method
	UsingAnnotation Method = "annotation"
	// UsingPlugin it is a keyword to use while fencing on/off the instances using plugin method
	UsingPlugin Method = "plugin"
)

// On marks an instance in a cluster as fenced
func On(
	ctx context.Context,
	crudClient client.Client,
	serverName,
	namespace,
	clusterName string,
	fencingMethod Method,
) error {
	switch fencingMethod {
	case UsingPlugin:
		_, _, err := run.Run(fmt.Sprintf("kubectl cnpg fencing on %v %v -n %v",
			clusterName, serverName, namespace))
		if err != nil {
			return err
		}
	case UsingAnnotation:
		err := utils.NewFencingMetadataExecutor(crudClient).
			AddFencing().
			ForInstance(serverName).
			Execute(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, &apiv1.Cluster{})
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unrecognized fencing Method: %s", fencingMethod)
	}
	return nil
}

// Off marks an instance in a cluster as not fenced
func Off(
	ctx context.Context,
	crudClient client.Client,
	serverName,
	namespace,
	clusterName string,
	fencingMethod Method,
) error {
	switch fencingMethod {
	case UsingPlugin:
		_, _, err := run.Run(fmt.Sprintf("kubectl cnpg fencing off %v %v -n %v",
			clusterName, serverName, namespace))
		if err != nil {
			return err
		}
	case UsingAnnotation:
		err := utils.NewFencingMetadataExecutor(crudClient).
			RemoveFencing().
			ForInstance(serverName).
			Execute(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, &apiv1.Cluster{})
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unrecognized fencing Method: %s", fencingMethod)
	}
	return nil
}
