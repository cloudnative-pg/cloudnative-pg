/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package run

import (
	"errors"
	"fmt"
)

// unretryable should be used to wrap an error, specifying explicitly it can not be retried
type unretryable struct {
	Err error
}

func (d unretryable) Error() string {
	return fmt.Sprintf("unretryable: %s", d.Err.Error())
}

func (d unretryable) Unwrap() error {
	return d.Err
}

func makeUnretryableError(err error) error {
	return unretryable{Err: err}
}

func isRunSubCommandRetryable(err error) bool {
	return !errors.As(err, &unretryable{})
}
