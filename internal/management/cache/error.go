/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package cache

import "errors"

// ErrCacheMiss is returned when the requested value is not found in cache,
// or it is expired.
var (
	ErrCacheMiss         = errors.New("cache miss")
	ErrUnsupportedObject = errors.New("unsupported cache object")
)
