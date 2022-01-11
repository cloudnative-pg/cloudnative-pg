/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// ReloadOperatorDeployment finds and deletes the operator pod. Returns
// error if the new pod is not ready within a defined timeout
func ReloadOperatorDeployment(env *TestingEnvironment, timeoutSeconds uint) error {
	operatorPod, err := env.GetOperatorPod()
	if err != nil {
		return err
	}
	zero := int64(0)
	err = env.Client.Delete(env.Ctx, &operatorPod,
		&ctrlclient.DeleteOptions{GracePeriodSeconds: &zero},
	)
	if err != nil {
		return err
	}
	err = retry.Do(
		func() error {
			ready, err := env.IsOperatorReady()
			if err != nil {
				return err
			}
			if !ready {
				return fmt.Errorf("operator pod %v is not ready", operatorPod.Name)
			}
			return nil
		},
		retry.Delay(time.Second),
		retry.Attempts(timeoutSeconds),
	)
	return err
}
