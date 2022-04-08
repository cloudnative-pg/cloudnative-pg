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
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/fence"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// FencingMethod will be one of the supported ways to trigger an instance fencing
type FencingMethod string

const (
	// UsingAnnotation it is a keyword to use while fencing on/off the instances using annotation method
	UsingAnnotation FencingMethod = "annotation"
	// UsingPlugin it is a keyword to use while fencing on/off the instances using plugin method
	UsingPlugin FencingMethod = "plugin"
)

// FencingOn marks an instance in a cluster as fenced
func FencingOn(
	env *TestingEnvironment,
	serverName,
	namespace,
	clusterName string,
	fencingMethod FencingMethod,
) error {
	switch fencingMethod {
	case UsingPlugin:
		_, _, err := Run(fmt.Sprintf("kubectl cnpg fencing on %v %v -n %v",
			clusterName, serverName, namespace))
		if err != nil {
			return err
		}
	case UsingAnnotation:
		err := fence.ApplyFenceFunc(env.Ctx, env.Client, clusterName, namespace, serverName, utils.AddFencedInstance)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unrecognized fencing Method: %s", fencingMethod)
	}
	return nil
}

// FencingOff marks an instance in a cluster as not fenced
func FencingOff(
	env *TestingEnvironment,
	serverName,
	namespace,
	clusterName string,
	fencingMethod FencingMethod,
) error {
	switch fencingMethod {
	case UsingPlugin:
		_, _, err := Run(fmt.Sprintf("kubectl cnpg fencing off %v %v -n %v",
			clusterName, serverName, namespace))
		if err != nil {
			return err
		}
	case UsingAnnotation:
		err := fence.ApplyFenceFunc(env.Ctx, env.Client, clusterName, namespace, serverName, utils.RemoveFencedInstance)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unrecognized fencing Method: %s", fencingMethod)
	}
	return nil
}
