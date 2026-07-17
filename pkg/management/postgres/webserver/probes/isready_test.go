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

package probes

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// staticResultRunner is a runner returning a fixed result
type staticResultRunner struct {
	err error
}

func (r staticResultRunner) IsHealthy(_ context.Context, _ *postgres.Instance) error {
	return r.err
}

var _ = Describe("startupPgIsReadyChecker", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("succeeds when the server is accepting connections", func() {
		checker := startupPgIsReadyChecker{inner: staticResultRunner{}}
		Expect(checker.IsHealthy(ctx, nil)).To(Succeed())
	})

	It("succeeds when the server is alive but rejecting connections", func() {
		checker := startupPgIsReadyChecker{inner: staticResultRunner{err: postgres.ErrPgRejectingConnection}}
		Expect(checker.IsHealthy(ctx, nil)).To(Succeed())
	})

	It("succeeds when the rejecting-connections error is wrapped", func() {
		checker := startupPgIsReadyChecker{
			inner: staticResultRunner{err: fmt.Errorf("wrapped: %w", postgres.ErrPgRejectingConnection)},
		}
		Expect(checker.IsHealthy(ctx, nil)).To(Succeed())
	})

	It("fails when no connection can be established", func() {
		checker := startupPgIsReadyChecker{inner: staticResultRunner{err: postgres.ErrNoConnectionEstablished}}
		Expect(checker.IsHealthy(ctx, nil)).To(MatchError(postgres.ErrNoConnectionEstablished))
	})

	It("fails on any other error", func() {
		expectedError := errors.New("pg_isready usage error")
		checker := startupPgIsReadyChecker{inner: staticResultRunner{err: expectedError}}
		Expect(checker.IsHealthy(ctx, nil)).To(MatchError(expectedError))
	})
})
