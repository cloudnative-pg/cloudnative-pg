/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package e2e

import (
	"testing"
	"time"

	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	//+kubebuilder:scaffold:imports
	clusterv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	samplesDir  = "../../docs/src/samples"
	fixturesDir = "./fixtures"
)

const (
	operatorNamespace = "postgresql-operator-system"
)

var env *tests.TestingEnvironment

var _ = BeforeSuite(func() {
	var err error
	env, err = tests.NewTestingEnvironment()
	if err != nil {
		Fail(err.Error())
	}
	_ = k8sscheme.AddToScheme(env.Scheme)
	_ = clusterv1alpha1.AddToScheme(env.Scheme)
	//+kubebuilder:scaffold:scheme
})

func TestE2ESuite(t *testing.T) {
	RegisterFailHandler(Fail)
	SetDefaultEventuallyPollingInterval(200 * time.Millisecond)
	RunSpecs(t, "Cloud Native PostgreSQL Operator E2E")
}
