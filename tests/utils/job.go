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

package utils

import (
	"errors"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
)

// GetJob gets a Job by namespace and name
func (env TestingEnvironment) GetJob(namespace, jobName string) (*batchv1.Job, error) {
	wrapErr := func(err error) error {
		return fmt.Errorf("while getting job '%s/%s': %w", namespace, jobName, err)
	}
	jobList, err := env.GetJobList(namespace)
	if err != nil {
		return nil, wrapErr(err)
	}
	for _, job := range jobList.Items {
		if jobName == job.Name {
			return &job, nil
		}
	}
	return nil, wrapErr(errors.New("job not found"))
}
