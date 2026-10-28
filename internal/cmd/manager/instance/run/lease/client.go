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

package lease

import (
	"context"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coordinationv1client "k8s.io/client-go/kubernetes/typed/coordination/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// positionSource computes the position to publish with the lease about to be
// written, or nil to leave the annotation as it is. It must not fail: a source
// that cannot tell returns nil, the write of the lease is never held back.
type positionSource func(ctx context.Context, lease *coordinationv1.Lease) *utils.LeasePosition

// annotatingLeasesGetter decorates the client of a resourcelock.LeaseLock to
// set the position annotation on every write of the lease. The lock does not
// give access to the Lease it writes, only to the client, and every write of
// it (claim, renewals, release) goes through there.
//
// The annotation travels in the same optimistic-lock write as the record, so
// it needs no extra API call and cannot conflict on its own.
type annotatingLeasesGetter struct {
	coordinationv1client.LeasesGetter

	source positionSource
}

// Leases implements coordinationv1client.LeasesGetter
func (g *annotatingLeasesGetter) Leases(namespace string) coordinationv1client.LeaseInterface {
	return &annotatingLeases{
		LeaseInterface: g.LeasesGetter.Leases(namespace),
		source:         g.source,
	}
}

// annotatingLeases overrides the writes of a LeaseInterface that create a
// lease, or replace it as a whole
type annotatingLeases struct {
	coordinationv1client.LeaseInterface

	source positionSource
}

// Create implements coordinationv1client.LeaseInterface
func (l *annotatingLeases) Create(
	ctx context.Context,
	lease *coordinationv1.Lease,
	opts metav1.CreateOptions,
) (*coordinationv1.Lease, error) {
	return l.LeaseInterface.Create(ctx, l.annotate(ctx, lease), opts)
}

// Update implements coordinationv1client.LeaseInterface
func (l *annotatingLeases) Update(
	ctx context.Context,
	lease *coordinationv1.Lease,
	opts metav1.UpdateOptions,
) (*coordinationv1.Lease, error) {
	return l.LeaseInterface.Update(ctx, l.annotate(ctx, lease), opts)
}

// annotate returns the lease to write. The one passed is the cache of the
// lock, it is not changed.
func (l *annotatingLeases) annotate(ctx context.Context, lease *coordinationv1.Lease) *coordinationv1.Lease {
	if l.source == nil {
		return lease
	}

	position := l.source(ctx, lease)
	if position == nil {
		return lease
	}

	value, err := position.Marshal()
	if err != nil {
		// Not reachable for this type, and not a reason to hold back the lease
		return lease
	}

	result := lease.DeepCopy()
	if result.Annotations == nil {
		result.Annotations = make(map[string]string, 1)
	}
	result.Annotations[utils.LeasePositionAnnotationName] = value

	return result
}
