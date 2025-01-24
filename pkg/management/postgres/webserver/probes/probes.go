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

package probes

import (
	"context"

	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
)

// Checker is implemented by probe strategies
type Checker interface {
	// Evaluate evaluate the status of PostgreSQL. If the probe is positive,
	// it returns a nil error, otherwise the error status describes why
	// the probe is failing
	Evaluate(ctx context.Context, instance *postgres.Instance) error
}

// ForConfiguration returns the correct checker for the passed
// probe
func ForConfiguration(probe *apiv1.ProbeWithStrategy) Checker {
	switch {
	case probe == nil:
		return PgIsReadyChecker{}
	case probe.Type == apiv1.ProbeStrategyPgIsReady:
		return PgIsReadyChecker{}
	case probe.Type == apiv1.ProbeStrategyQuery:
		return PgQueryChecker{}
	case probe.Type == apiv1.ProbeStrategyStreaming:
		result := PgStreamingChecker{}
		if probe.MaximumLag != nil {
			result.MaximumLag = ptr.To(probe.MaximumLag.AsDec().UnscaledBig().Uint64())
		}
		return result
	}

	return PgIsReadyChecker{}
}
