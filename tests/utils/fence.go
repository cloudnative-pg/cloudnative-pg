/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/fence"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
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
	fencingMethod FencingMethod) error {
	switch fencingMethod {
	case UsingPlugin:
		_, _, err := Run(fmt.Sprintf("kubectl cnp fencing on %v %v -n %v",
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
	fencingMethod FencingMethod) error {
	switch fencingMethod {
	case UsingPlugin:
		_, _, err := Run(fmt.Sprintf("kubectl cnp fencing off %v %v -n %v",
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
