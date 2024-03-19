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
	v12 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/stringset"
)

var (
	// ErrorFencedInstancesSyntax is emitted when the fencedInstances annotation
	// have an invalid syntax
	ErrorFencedInstancesSyntax = errors.New("fencedInstances annotation has invalid syntax")

	// ErrorServerAlreadyFenced is emitted when trying to fence an instance
	// which is already fenced
	ErrorServerAlreadyFenced = errors.New("this instance has already been fenced")

	// ErrorServerAlreadyUnfenced is emitted when trying to unfencing an instance
	// which was not fenced
	ErrorServerAlreadyUnfenced = errors.New("this instance was not fenced")

	// ErrorSingleInstanceUnfencing is emitted when unfencing a single instance
	// while all the cluster is fenced
	ErrorSingleInstanceUnfencing = errors.New("unfencing an instance while the whole cluster is fenced is not supported")
)

const (
	// FenceAllServers is the wildcard that, if put inside the fenced instances list, will fence every
	// CNPG instance
	FenceAllServers = "*"
)

// GetFencedInstances gets the set of fenced servers from the annotations
func GetFencedInstances(annotations map[string]string) (*stringset.Data, error) {
	fencedInstances, ok := annotations[FencedInstanceAnnotation]
	if !ok {
		return stringset.New(), nil
	}

	var fencedInstancesList []string
	err := json.Unmarshal([]byte(fencedInstances), &fencedInstancesList)
	if err != nil {
		return nil, ErrorFencedInstancesSyntax
	}

	return stringset.From(fencedInstancesList), nil
}

// SetFencedInstances sets the list of fenced servers inside the annotations
func SetFencedInstances(object *metav1.ObjectMeta, data *stringset.Data) error {
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
func AddFencedInstance(serverName string, object *metav1.ObjectMeta) error {
	fencedInstances, err := GetFencedInstances(object.Annotations)
	if err != nil {
		return err
	}

	if fencedInstances.Has(FenceAllServers) {
		return nil
	}
	if fencedInstances.Has(serverName) {
		return ErrorServerAlreadyFenced
	}

	if serverName == FenceAllServers {
		fencedInstances = stringset.From([]string{FenceAllServers})
	} else {
		fencedInstances.Put(serverName)
	}

	return SetFencedInstances(object, fencedInstances)
}

// RemoveFencedInstance removes the given server name from the FencedInstanceAnnotation annotation
// returns an error if the instance was already unfenced
func RemoveFencedInstance(serverName string, object *metav1.ObjectMeta) error {
	if serverName == FenceAllServers {
		return SetFencedInstances(object, stringset.New())
	}

	fencedInstances, err := GetFencedInstances(object.Annotations)
	if err != nil {
		return err
	}

	if fencedInstances.Has(FenceAllServers) {
		return ErrorSingleInstanceUnfencing
	}

	if !fencedInstances.Has(serverName) {
		return ErrorServerAlreadyUnfenced
	}

	fencedInstances.Delete(serverName)
	return SetFencedInstances(object, fencedInstances)
}

type FencingBuilder struct {
	fenceFunc    func(string, *metav1.ObjectMeta) error
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

func (fb *FencingBuilder) Add() *FencingBuilder {
	fb.fenceFunc = AddFencedInstance
	return fb
}

func (fb *FencingBuilder) Remove() *FencingBuilder {
	fb.fenceFunc = RemoveFencedInstance
	return fb
}

func (fb *FencingBuilder) AllInstances() *FencingBuilder {
	fb.instanceName = FenceAllServers
	return fb
}

func (fb *FencingBuilder) Instance(instanceName string) *FencingBuilder {
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

	if fb.instanceName != FenceAllServers {
		// Check if the Pod exist
		var pod v12.Pod
		err = fb.cli.Get(ctx, client.ObjectKey{Namespace: fb.namespace, Name: fb.instanceName}, &pod)
		if err != nil {
			return fmt.Errorf("node %s not found in namespace %s", fb.instanceName, fb.namespace)
		}
	}

	fencedCluster := cluster.DeepCopy()
	if err = fb.fenceFunc(fb.instanceName, &fencedCluster.ObjectMeta); err != nil {
		return err
	}

	err = fb.cli.Patch(ctx, fencedCluster, client.MergeFrom(&cluster))
	if err != nil {
		return err
	}

	return nil
}
