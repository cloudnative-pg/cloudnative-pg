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

// Package client contains the constants and functions for reading supported objects from cache
// or building them in case of cache miss.
package client

import (
	"encoding/json"
	"io/ioutil"
	"net/http"

	"k8s.io/client-go/util/retry"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/cache"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/url"
)

// GetCluster gets the required cluster from cache
func GetCluster() (*apiv1.Cluster, error) {
	bytes, err := httpCacheGet(cache.ClusterKey)
	if err != nil {
		return nil, err
	}

	cluster := &apiv1.Cluster{}
	err = json.Unmarshal(bytes, cluster)
	if err != nil {
		return nil, err
	}

	return cluster, nil
}

// GetEnv gets the environment variables from cache
func GetEnv(key string) ([]string, error) {
	bytes, err := httpCacheGet(key)
	if err != nil {
		return nil, err
	}

	var env []string
	err = json.Unmarshal(bytes, &env)
	if err != nil {
		return nil, err
	}

	return env, nil
}

// httpCacheGet retrieves an object from the cache.
// In case of failures it retries for a while before giving up
func httpCacheGet(urlPath string) ([]byte, error) {
	var bytes []byte
	err := retry.OnError(retry.DefaultBackoff, func(error) bool { return true }, func() error {
		var err error
		bytes, err = get(urlPath)
		return err
	})
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func get(urlPath string) ([]byte, error) {
	resp, err := http.Get(url.Local(url.PathCache+urlPath, url.LocalPort))
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, cache.ErrCacheMiss
	}

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}
