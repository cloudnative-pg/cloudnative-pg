/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package hash allows the user to get a hash number for a given Kubernetes
// object. This is useful to detect when a derived resource need to be
// changed too.
//
// The code in this package is adapted from:
//
// https://github.com/kubernetes/kubernetes/blob/master/pkg/util/hash/hash.go   // wokeignore:rule=master
// https://github.com/kubernetes/kubernetes/blob/ea0764452222146c47ec826977f49d7001b0ea8c/
//   pkg/controller/controller_utils.go#L1189
package hash

import (
	"fmt"
	"hash"
	"hash/fnv"

	"github.com/davecgh/go-spew/spew"

	"k8s.io/apimachinery/pkg/util/rand"
)

// DeepHashObject writes specified object to hash using the spew library
// which follows pointers and prints actual values of the nested objects
// ensuring the hash does not change when a pointer changes.
func DeepHashObject(hasher hash.Hash, objectToWrite interface{}) error {
	hasher.Reset()
	printer := spew.ConfigState{
		Indent:         " ",
		SortKeys:       true,
		DisableMethods: true,
		SpewKeys:       true,
	}

	_, err := printer.Fprintf(hasher, "%#v", objectToWrite)
	return err
}

// ComputeHash returns a hash value calculated from pod template and
// a collisionCount to avoid hash collision. The hash will be safe encoded to
// avoid bad words.
func ComputeHash(object interface{}) (string, error) {
	podTemplateSpecHasher := fnv.New32a()
	if err := DeepHashObject(podTemplateSpecHasher, object); err != nil {
		return "", err
	}

	return rand.SafeEncodeString(fmt.Sprint(podTemplateSpecHasher.Sum32())), nil
}
