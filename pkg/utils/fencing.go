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
	"github.com/cloudnative-pg/cloudnative-pg/api/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
func setFencedInstances(object *metav1.ObjectMeta, data *stringset.Data) error {
	if data.Len() == 0 {
		delete(object.Annotations, FencedInstanceAnnotation)
		return nil
	}

	serverList := data.ToList()
	sort.Strings(serverList)

	annotationValue, err := json.Marshal(serverList)
	if err != nil {
		return err
	}
	if object.Annotations == nil {
		object.Annotations = make(map[string]string)
	}
	object.Annotations[FencedInstanceAnnotation] = string(annotationValue)

	return nil
}

// AddFencedInstance adds the given server name to the FencedInstanceAnnotation annotation
// returns an error if the instance was already fenced
func AddFencedInstance(serverName string, object *metav1.ObjectMeta) (bool, error) {
	fencedInstances, err := GetFencedInstances(object.Annotations)
	if err != nil {
		return false, err
	}

	if fencedInstances.Has(FenceAllInstances) || fencedInstances.Has(serverName) {
		return false, nil
	}

	switch serverName {
	case FenceAllInstances:
		fencedInstances = stringset.From([]string{FenceAllInstances})
	default:
		fencedInstances.Put(serverName)
	}

	return true, setFencedInstances(object, fencedInstances)
}

// removeFencedInstance removes the given server name from the FencedInstanceAnnotation annotation
// returns an error if the instance was already unfenced
func removeFencedInstance(serverName string, object *metav1.ObjectMeta) (bool, error) {
	fencedInstances, err := GetFencedInstances(object.Annotations)
	if err != nil {
		return false, err
	}
	if fencedInstances.Len() == 0 {
		return false, nil
	}
	if serverName == FenceAllInstances {
		return true, setFencedInstances(object, stringset.New())
	}

	if fencedInstances.Has(FenceAllInstances) {
		return false, ErrorSingleInstanceUnfencing
	}

	if !fencedInstances.Has(serverName) {
		return false, nil
	}

	fencedInstances.Delete(serverName)
	return true, setFencedInstances(object, fencedInstances)
}

type FencingBuilder struct {
	fenceFunc    func(string, *metav1.ObjectMeta) (appliedChange bool, err error)
	instanceName string
	cli          client.Client
	namespace    string
	clusterName  string
}

func NewFencingBuilder(cli client.Client, clusterName, namespace string) *FencingBuilder {
	return &FencingBuilder{
		cli:         cli,
		clusterName: clusterName,
		namespace:   namespace,
	}
}

func (fb *FencingBuilder) AddFencing() *FencingBuilder {
	fb.fenceFunc = AddFencedInstance
	return fb
}

func (fb *FencingBuilder) RemoveFencing() *FencingBuilder {
	fb.fenceFunc = removeFencedInstance
	return fb
}

func (fb *FencingBuilder) ToAllInstances() *FencingBuilder {
	fb.instanceName = FenceAllInstances
	return fb
}

func (fb *FencingBuilder) ToInstance(instanceName string) *FencingBuilder {
	fb.instanceName = instanceName
	return fb
}

func (fb *FencingBuilder) Execute(ctx context.Context) error {
	var cluster v1.Cluster

	// Get the Cluster object
	err := fb.cli.Get(ctx, client.ObjectKey{Namespace: fb.namespace, Name: fb.clusterName}, &cluster)
	if err != nil {
		return err
	}

	if fb.instanceName != FenceAllInstances {
		var pod corev1.Pod
		err = fb.cli.Get(ctx, client.ObjectKey{Namespace: fb.namespace, Name: fb.instanceName}, &pod)
		if err != nil {
			return fmt.Errorf("node %s not found in namespace %s", fb.instanceName, fb.namespace)
		}
	}

	fencedCluster := cluster.DeepCopy()
	appliedChange, err := fb.fenceFunc(fb.instanceName, &fencedCluster.ObjectMeta)
	if err != nil {
		return err
	}
	if !appliedChange {
		return nil
	}

	return fb.cli.Patch(ctx, fencedCluster, client.MergeFrom(&cluster))
}
