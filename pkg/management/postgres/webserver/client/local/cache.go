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

package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"k8s.io/client-go/util/retry"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

// CacheClient is the interface to interact with the cache endpoints
type CacheClient interface {
	GetCluster() (*apiv1.Cluster, error)
	GetEnv(key string) ([]string, error)
}

type cacheClientImpl struct {
	cli *http.Client
}

// GetCluster gets the required cluster from cache
func (c *cacheClientImpl) GetCluster() (*apiv1.Cluster, error) {
	bytes, err := c.httpCacheGet(cache.ClusterKey)
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
func (c *cacheClientImpl) GetEnv(key string) ([]string, error) {
	bytes, err := c.httpCacheGet(key)
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
func (c *cacheClientImpl) httpCacheGet(urlPath string) ([]byte, error) {
	var bytes []byte
	err := retry.OnError(retry.DefaultBackoff, func(error) bool { return true }, func() error {
		var err error
		bytes, err = c.get(urlPath)
		return err
	})
	if err != nil {
		return nil, err
	}

	return bytes, nil
}

func (c *cacheClientImpl) get(urlPath string) ([]byte, error) {
	resp, err := c.cli.Get(url.Local(url.PathCache+urlPath, url.LocalPort))
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		break
	case http.StatusNotFound:
		return nil, cache.ErrCacheMiss
	case http.StatusInternalServerError:
		return nil, errors.New("encountered an internal server error while fetching cluster cache")
	default:
		return nil, fmt.Errorf("encountered an unexpected status code while fetching cluster cache: %d", resp.StatusCode)
	}

	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return bytes, nil
}
