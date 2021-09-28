/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package client contains the constants and functions for reading supported objects from cache
// or building them in case of cache miss.
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/barman"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

// GetCluster gets the required cluster from cache or from the APIServer in case of cache miss
func GetCluster(ctx context.Context,
	typedClient client.Client,
	namespace string,
	name string,
) (*apiv1.Cluster, error) {
	var cluster *apiv1.Cluster
	cached := true

	cluster, err := GetClusterFromCacheEndpoint()
	if errors.Is(err, cache.ErrCacheMiss) {
		cached = false
	} else if err != nil {
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}

	if cached {
		return cluster, nil
	}

	cluster = &apiv1.Cluster{}

	err = typedClient.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, cluster)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}

	return cluster, nil
}

// GetEnv gets the environment variables from cache or builds them in case of cache
// miss
func GetEnv(ctx context.Context,
	typedClient client.Client,
	namespace string,
	config *apiv1.BarmanObjectStoreConfiguration,
	key string,
) ([]string, error) {
	var env []string
	cached := true

	env, err := GetEnvFromCacheEndpoint(key)
	if errors.Is(err, cache.ErrCacheMiss) {
		cached = false
	} else if err != nil {
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}

	if cached {
		return env, nil
	}

	env, err = barman.EnvSetCloudCredentials(
		ctx,
		typedClient,
		namespace,
		config,
		os.Environ())
	if err != nil {
		return nil, fmt.Errorf("failed to get cloud credentials: %w", err)
	}

	return env, nil
}

// GetClusterFromCacheEndpoint retrieves the cluster from the cache
func GetClusterFromCacheEndpoint() (*apiv1.Cluster, error) {
	r, err := http.Get(url.Local(url.PathCache+cache.ClusterKey, url.LocalPort))
	if err != nil {
		return nil, err
	}

	if r.StatusCode != http.StatusOK {
		_, _ = ioutil.ReadAll(r.Body)
		_ = r.Body.Close()
		return nil, cache.ErrCacheMiss
	}
	defer func() {
		_ = r.Body.Close()
	}()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var cluster apiv1.Cluster
	err = json.Unmarshal(body, &cluster)
	if err != nil {
		return nil, err
	}

	return &cluster, nil
}

// GetEnvFromCacheEndpoint retrieves the list of environment variables from the 'cachePath' file
func GetEnvFromCacheEndpoint(c string) ([]string, error) {
	r, err := http.Get(url.Local(url.PathCache+c, url.LocalPort))
	if err != nil {
		return nil, err
	}

	if r.StatusCode != http.StatusOK {
		_, _ = ioutil.ReadAll(r.Body)
		_ = r.Body.Close()
		return nil, cache.ErrCacheMiss
	}
	defer func() {
		_ = r.Body.Close()
	}()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var env []string
	err = json.Unmarshal(body, &env)
	if err != nil {
		return nil, err
	}

	return env, nil
}
