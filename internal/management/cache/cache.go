/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package cache contains the constants and functions for reading/writing to the process local cache
// some specific supported objects
package cache

import (
	"sync"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

const (
	// ClusterKey is the key to be used to access the cached cluster
	ClusterKey = "cluster"
	// WALArchiveKey is the key to be used to access the cached envs for wal-archive
	WALArchiveKey = "wal-archive"
	// WALRestoreKey is the key to be used to access the cached envs for wal-restore
	WALRestoreKey = "wal-restore"
)

var cache sync.Map

// Store write an object into the local cache
func Store(c string, v interface{}) {
	cache.Store(c, v)
}

// Delete an object from the local cache
func Delete(c string) {
	cache.Delete(c)
}

// LoadEnv loads a key from the local cache
func LoadEnv(c string) ([]string, error) {
	value, ok := cache.Load(c)
	if !ok {
		return nil, ErrCacheMiss
	}

	if v, ok := value.([]string); ok {
		return v, nil
	}

	return nil, ErrUnsupportedObject
}

// LoadCluster loads a key from the local cache
func LoadCluster() (*apiv1.Cluster, error) {
	value, ok := cache.Load(ClusterKey)
	if !ok {
		return nil, ErrCacheMiss
	}

	if v, ok := value.(*apiv1.Cluster); ok {
		return v, nil
	}

	return nil, ErrUnsupportedObject
}
