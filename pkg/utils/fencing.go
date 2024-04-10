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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/stringset"
)

var (
	// ErrorFencedInstancesSyntax is emitted when the fencedInstances annotation
	// have an invalid syntax
	ErrorFencedInstancesSyntax = errors.New("fencedInstances annotation has invalid syntax")

	// ErrorSingleInstanceUnfencing is emitted when unfencing a single instance
	// while all the cluster is fenced
	ErrorSingleInstanceUnfencing = errors.New("unfencing an instance while the whole cluster is fenced is not supported")
)

const (
	// FenceAllInstances is the wildcard that, if put inside the fenced instances list, will fence every
	// CNPG instance
	FenceAllInstances = "*"
)

// GetFencedInstances gets the set of fenced servers from the annotations
func GetFencedInstances(annotations map[string]string) (*stringset.Data, error) {
	fencedInstances, ok := annotations[FencedInstanceAnnotation]
	if !ok {
		return stringset.New(), nil
	}

	var fencedInstancesList []string
	if err := json.Unmarshal([]byte(fencedInstances), &fencedInstancesList); err != nil {
		return nil, ErrorFencedInstancesSyntax
	}

	return stringset.From(fencedInstancesList), nil
}

// setFencedInstances sets the list of fenced servers inside the annotations
func setFencedInstances(object metav1.Object, data *stringset.Data) error {
	annotations := object.GetAnnotations()
	defer func() {
		object.SetAnnotations(annotations)
	}()
	if data.Len() == 0 {
		delete(annotations, FencedInstanceAnnotation)
		return nil
	}

	serverList := data.ToList()
	sort.Strings(serverList)

	annotationValue, err := json.Marshal(serverList)
	if err != nil {
		return err
	}
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[FencedInstanceAnnotation] = string(annotationValue)

	return nil
}

// AddFencedInstance adds the given server name to the FencedInstanceAnnotation annotation
// returns an error if the instance was already fenced
func AddFencedInstance(instanceName string, object metav1.Object) (bool, error) {
	fencedInstances, err := GetFencedInstances(object.GetAnnotations())
	if err != nil {
		return false, err
	}

	if fencedInstances.Has(FenceAllInstances) || fencedInstances.Has(instanceName) {
		return false, nil
	}

	switch instanceName {
	case FenceAllInstances:
		fencedInstances = stringset.From([]string{FenceAllInstances})
	default:
		fencedInstances.Put(instanceName)
	}

	return true, setFencedInstances(object, fencedInstances)
}

// removeFencedInstance removes the given server name from the FencedInstanceAnnotation annotation
// returns an error if the instance was already unfenced
func removeFencedInstance(instanceName string, object metav1.Object) (bool, error) {
	fencedInstances, err := GetFencedInstances(object.GetAnnotations())
	if err != nil {
		return false, err
	}
	if fencedInstances.Len() == 0 {
		return false, nil
	}
	if instanceName == FenceAllInstances {
		return true, setFencedInstances(object, stringset.New())
	}

	if fencedInstances.Has(FenceAllInstances) {
		return false, ErrorSingleInstanceUnfencing
	}

	if !fencedInstances.Has(instanceName) {
		return false, nil
	}

	fencedInstances.Delete(instanceName)
	return true, setFencedInstances(object, fencedInstances)
}

// FencingMetadataExecutor executes the logic regarding adding and removing the fencing annotation for a kubernetes
// object
type FencingMetadataExecutor struct {
	fenceFunc    func(string, metav1.Object) (appliedChange bool, err error)
	instanceName string
	cli          client.Client
}

// NewFencingMetadataExecutor creates a fluent client for FencingMetadataExecutor
func NewFencingMetadataExecutor(cli client.Client) *FencingMetadataExecutor {
	return &FencingMetadataExecutor{
		cli: cli,
	}
}

// AddFencing instructs the client to execute the logic of adding a instance
func (fb *FencingMetadataExecutor) AddFencing() *FencingMetadataExecutor {
	fb.fenceFunc = AddFencedInstance
	return fb
}

// RemoveFencing instructs the client to execute the logic of removing an instance
func (fb *FencingMetadataExecutor) RemoveFencing() *FencingMetadataExecutor {
	fb.fenceFunc = removeFencedInstance
	return fb
}

// ForAllInstances applies the logic to all cluster instances
func (fb *FencingMetadataExecutor) ForAllInstances() *FencingMetadataExecutor {
	fb.instanceName = FenceAllInstances
	return fb
}

// ForInstance applies the logic to the specified instance
func (fb *FencingMetadataExecutor) ForInstance(instanceName string) *FencingMetadataExecutor {
	fb.instanceName = instanceName
	return fb
}

// Execute executes the instructions given with the fluent builder, returns any error encountered
func (fb *FencingMetadataExecutor) Execute(ctx context.Context, key types.NamespacedName, obj client.Object) error {
	if fb.instanceName == "" {
		return errors.New("chose an operation to execute")
	}

	if err := fb.cli.Get(ctx, key, obj); err != nil {
		return err
	}

	if fb.instanceName != FenceAllInstances {
		var pod corev1.Pod
		if err := fb.cli.Get(ctx, client.ObjectKey{Namespace: key.Namespace, Name: fb.instanceName}, &pod); err != nil {
			return fmt.Errorf("node %s not found in namespace %s", fb.instanceName, key.Namespace)
		}
	}

	fencedObject := obj.DeepCopyObject().(client.Object)
	appliedChange, err := fb.fenceFunc(fb.instanceName, fencedObject)
	if err != nil {
		return err
	}
	if !appliedChange {
		return nil
	}

	return fb.cli.Patch(ctx, fencedObject, client.MergeFrom(obj))
}
