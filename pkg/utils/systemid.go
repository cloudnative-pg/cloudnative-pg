/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var systemUID string

// DetectKubeSystemUID retrieves the UID of the kube-system namespace of the containing cluster
func DetectKubeSystemUID(ctx context.Context, cli *kubernetes.Clientset) error {
	kubeSystemNamespace, err := cli.CoreV1().Namespaces().Get(ctx, "kube-system", metav1.GetOptions{})
	if err != nil {
		return err
	}

	systemUID = string(kubeSystemNamespace.UID)

	return nil
}

// GetKubeSystemUID returns the uid of the kube-system namespace
func GetKubeSystemUID() string {
	return systemUID
}
