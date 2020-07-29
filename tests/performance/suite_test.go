/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package performance

import (
	"testing"

	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	//+kubebuilder:scaffold:imports
	clusterv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	samplesDir = "../../docs/src/samples"
)

var env = tests.NewTestingEnvironment()

var _ = BeforeSuite(func() {
	_ = k8sscheme.AddToScheme(env.Scheme)
	_ = clusterv1alpha1.AddToScheme(env.Scheme)
	//+kubebuilder:scaffold:scheme
})

func TestE2ESuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cloud Native PostgreSQL Operator E2E")
}
