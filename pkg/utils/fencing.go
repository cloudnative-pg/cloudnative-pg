/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/stringset"
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
	// FencedInstanceAnnotation is the annotation to be used for fencing instances, the value should be a
	// JSON list of all the instances we want to be fenced, e.g. `["cluster-example-1","cluster-example-2`"].
	// If the list contain the "*" element, every node is fenced.
	FencedInstanceAnnotation = "k8s.enterprisedb.io/fencedInstances"

	// FenceAllServers is the wildcard that, if put inside the fenced instances list, will fence every
	// CNP instance
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
func SetFencedInstances(annotations map[string]string, data *stringset.Data) error {
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
	annotations[FencedInstanceAnnotation] = string(annotationValue)

	return nil
}

// AddFencedInstance adds the given server name to the FencedInstanceAnnotation annotation
// returns an error if the instance was already fenced
func AddFencedInstance(serverName string, annotations map[string]string) error {
	fencedInstances, err := GetFencedInstances(annotations)
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

	if err := SetFencedInstances(annotations, fencedInstances); err != nil {
		return err
	}

	return nil
}

// RemoveFencedInstance removes the given server name from the FencedInstanceAnnotation annotation
// returns an error if the instance was already unfenced
func RemoveFencedInstance(serverName string, annotations map[string]string) error {
	if serverName == FenceAllServers {
		return SetFencedInstances(annotations, stringset.New())
	}

	fencedInstances, err := GetFencedInstances(annotations)
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
	return SetFencedInstances(annotations, fencedInstances)
}
