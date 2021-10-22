/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"reflect"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils/hash"
)

// updateOwnedObjects ensure that we have the required objects
func (r *PoolerReconciler) updateOwnedObjects(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	if err := r.updateDeployment(ctx, pooler, resources); err != nil {
		return err
	}

	if err := r.updateRBAC(ctx, pooler, resources); err != nil {
		return err
	}

	return r.updateService(ctx, pooler, resources)
}

//nolint:dupl
// updateDeployment update the deployment or create it when needed
func (r *PoolerReconciler) updateDeployment(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)

	deployment, err := pgbouncer.Deployment(pooler, resources.Cluster)
	if err != nil {
		return err
	}

	switch {
	case resources.Deployment == nil:
		// Create a new deployment
		if err := ctrl.SetControllerReference(pooler, deployment, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating deployment")
		err := r.Create(ctx, deployment)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.Deployment = deployment
		return nil

	case resources.Deployment != nil:
		currentVersion := resources.Deployment.Annotations[pgbouncer.PgbouncerPoolerSpecHash]
		updatedVersion, err := hash.ComputeHash(pooler.Spec)
		if err != nil {
			return err
		}

		if currentVersion == updatedVersion {
			// Everything fine, the two deployments are using the
			// same specifications
			return nil
		}

		updatedDeployment := resources.Deployment.DeepCopy()
		updatedDeployment.Spec = deployment.Spec
		if updatedDeployment.Annotations == nil {
			updatedDeployment.Annotations = make(map[string]string)
		}
		updatedDeployment.Annotations[pgbouncer.PgbouncerPoolerSpecHash] = updatedVersion

		log.Info("Updating deployment")
		err = r.Patch(ctx, updatedDeployment, client.MergeFrom(resources.Deployment))
		if err != nil {
			return err
		}

		resources.Deployment = updatedDeployment
	}

	return nil
}

//nolint:dupl
// updateService update or create the pgbouncer service as needed
func (r *PoolerReconciler) updateService(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)

	if resources.Service == nil {
		service := pgbouncer.Service(pooler)
		if err := ctrl.SetControllerReference(pooler, service, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating service")
		err := r.Create(ctx, service)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.Service = service
		return nil
	}

	return nil
}

// updateRBAC update or create the pgbouncer RBAC
func (r *PoolerReconciler) updateRBAC(
	ctx context.Context,
	pooler *apiv1.Pooler,
	resources *poolerManagedResources,
) error {
	contextLog := log.FromContext(ctx)

	if resources.ServiceAccount == nil {
		serviceAccount := pgbouncer.ServiceAccount(pooler)
		if err := ctrl.SetControllerReference(pooler, serviceAccount, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating service account")
		err := r.Create(ctx, serviceAccount)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.ServiceAccount = serviceAccount
	}

	role := pgbouncer.Role(pooler)
	if resources.Role == nil {
		if err := ctrl.SetControllerReference(pooler, role, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating role")
		err := r.Create(ctx, role)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.Role = role
	} else if !reflect.DeepEqual(role.Rules, resources.Role.Rules) {
		contextLog.Info("Updating role")
		resources.Role.Rules = role.Rules
		if err := r.Update(ctx, resources.Role); err != nil {
			return err
		}
	}

	if resources.RoleBinding == nil {
		roleBinding := pgbouncer.RoleBinding(pooler)
		if err := ctrl.SetControllerReference(pooler, &roleBinding, r.Scheme); err != nil {
			return err
		}

		contextLog.Info("Creating role binding")
		err := r.Create(ctx, &roleBinding)
		if err != nil && !apierrs.IsAlreadyExists(err) {
			return err
		}
		resources.RoleBinding = &roleBinding
	}

	return nil
}
