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

package infrastructure

import (
	"context"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// Manager abstracts the operations that need to be sent to
// the database instance for the management of Replication Slots
type Manager interface {
	// List the available replication slots
	List(ctx context.Context, config *apiv1.ReplicationSlotsConfiguration) (ReplicationSlotList, error)
	// Update the replication slot
	Update(ctx context.Context, slot ReplicationSlot) error
	// Create the replication slot
	Create(ctx context.Context, slot ReplicationSlot) error
	// Delete the replication slot
	Delete(ctx context.Context, slot ReplicationSlot) error
}
