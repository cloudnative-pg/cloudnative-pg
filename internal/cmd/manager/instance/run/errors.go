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
