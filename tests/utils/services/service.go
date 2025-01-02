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

// Package services provides functions tomanage services inside K8s
package services

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// SSLMode while using psql connection with `sslmode`
type SSLMode string

const (
	// Prefer for `prefer` sslmode.
	Prefer SSLMode = "prefer"
	// Require for `require` sslmode.
	Require SSLMode = "require"
)

// CreateServiceFQDN create service name with full dns
func CreateServiceFQDN(namespace, serviceName string) string {
	return fmt.Sprintf("%v.%v.svc.cluster.local", serviceName, namespace)
}

// GetReadWriteServiceName read write service name
func GetReadWriteServiceName(clusterName string) string {
	return fmt.Sprintf("%v%v", clusterName, apiv1.ServiceReadWriteSuffix)
}

// getRwServiceObject return read write service object
func getRwServiceObject(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (*corev1.Service, error) {
	svcName := GetReadWriteServiceName(clusterName)
	service := &corev1.Service{}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      svcName,
	}
	err := crudClient.Get(ctx, namespacedName, service)
	if err != nil {
		return service, err
	}
	return service, nil
}

// CreateDSN return DSN name
func CreateDSN(host, user, dbname, password string, sslmode SSLMode, port int) string {
	const connectTimeout = 5
	return fmt.Sprintf("host=%v port=%v user=%v dbname=%v password=%v sslmode=%v connect_timeout=%v",
		host, port, user, dbname, password, sslmode, connectTimeout)
}

// GetHostName return fully qualified domain name for read write service
func GetHostName(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (string, error) {
	rwService, err := getRwServiceObject(ctx, crudClient, namespace, clusterName)
	if err != nil {
		return "", err
	}
	host := CreateServiceFQDN(namespace, rwService.GetName())
	return host, nil
}
