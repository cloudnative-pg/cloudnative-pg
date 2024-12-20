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
package e2e

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/blang/semver"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/openshift"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Upgrade Paths on OpenShift", Label(tests.LabelUpgrade), Ordered, Serial, func() {
	const (
		level             = tests.Lowest
		operatorNamespace = "openshift-operators"
		namespacePrefix   = "cluster-upgrade-e2e"
		clusterName       = "postgresql-storage-class"
		sampleFile        = fixturesDir + "/base/cluster-storage-class.yaml.template"
	)

	var ocp412 semver.Version
	var ocpVersion semver.Version
	var err error

	BeforeAll(func() {
		Skip("Disable until a new fix is compatible")
	})

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !IsOpenshift() {
			Skip("This test case is only applicable on OpenShift clusters")
		}
		// Setup OpenShift Versions
		ocp412, err = semver.Make("4.12.0")
		Expect(err).ToNot(HaveOccurred())
		// Get current OpenShift Versions
		ocpVersion, err = openshift.GetOpenshiftVersion(env.Ctx, env.RestClientConfig)
		Expect(err).ToNot(HaveOccurred())
	})

	cleanupOperator := func() error {
		// Cleanup the Operator
		err = openshift.DeleteOperatorCRDs(env.Ctx, env.Client)
		if err != nil {
			return err
		}
		err = openshift.DeleteSubscription(env.Ctx, env.Client)
		if err != nil {
			return err
		}
		err = openshift.DeleteCSV(env.Ctx, env.Client)
		if err != nil {
			return err
		}
		return nil
	}

	cleanupOpenshift := func() {
		err := cleanupOperator()
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() error {
			_, err = operator.GetOperatorPod(env.Ctx, env.Client)
			return err
		}, 120).Should(HaveOccurred())
	}

	assertClusterIsAligned := func(namespace, clusterName string) {
		By("Verifying the cluster pods have been upgraded", func() {
			Eventually(func() bool {
				return operator.HasOperatorBeenUpgraded(env.Ctx, env.Client)
			}).Should(BeTrue())

			operatorPodName, err := operator.GetOperatorPodName(env.Ctx, env.Client)
			Expect(err).ToNot(HaveOccurred())

			expectedVersion, err := operator.GetOperatorVersion("openshift-operators", operatorPodName)
			Expect(err).ToNot(HaveOccurred())

			podList, err := clusterutils.GetClusterPodList(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			for _, pod := range podList.Items {
				Eventually(func() (string, error) {
					return GetManagerVersion(namespace, pod.Name)
				}, 300).Should(BeEquivalentTo(expectedVersion))
			}
		})
	}

	applyUpgrade := func(initialSubscription, upgradeSubscription string) {
		// Apply a subscription in the openshift-operators namespace.
		// This should create the operator
		By("Applying the initial subscription", func() {
			err := openshift.CreateSubscription(env.Ctx, env.Client, initialSubscription)
			Expect(err).ToNot(HaveOccurred())
			AssertOperatorIsReady(env.Ctx, env.Client, env.Interface)
		})

		// Gather the version and semantic Versions of the operator
		currentVersion, err := openshift.GetSubscriptionVersion(env.Ctx, env.Client)
		Expect(err).ToNot(HaveOccurred())
		currentSemVersion, err := semver.Make(currentVersion)
		Expect(err).ToNot(HaveOccurred())
		newPolicyRelease, err := semver.Make("1.16.0")
		Expect(err).ToNot(HaveOccurred())

		// Create a Cluster in a namespace we'll delete at the end
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("Patching the status condition if required", func() {
			// Patch the status conditions if we are running on a pre new-policy release
			if currentSemVersion.LT(newPolicyRelease) {
				err = openshift.PatchStatusCondition(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		By("Applying the upgrade subscription", func() {
			// Apply the new subscription to upgrade to a new version of the operator
			err = openshift.UpgradeSubscription(env.Ctx, env.Client, upgradeSubscription)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() (string, error) {
				return openshift.GetSubscriptionVersion(env.Ctx, env.Client)
			}, 300).
				ShouldNot(BeEquivalentTo(currentVersion))
			AssertOperatorIsReady(env.Ctx, env.Client, env.Interface)
		})

		// Check if the upgrade was successful by making sure all the pods
		// have the new instance manager version
		assertClusterIsAligned(namespace, clusterName)
	}

	It("stable-v1 to alpha, currently version 1.22", func() {
		if ocpVersion.GT(ocp412) {
			Skip("This test runs only on OCP 4.12 or lower")
		}
		DeferCleanup(cleanupOpenshift)
		applyUpgrade("stable-v1", "alpha")
	})
})

// GetManagerVersion returns the current manager version of a given pod
func GetManagerVersion(namespace, podName string) (string, error) {
	out, _, err := run.Unchecked(fmt.Sprintf(
		"kubectl -n %v exec %v -c postgres -- /controller/manager version",
		namespace,
		podName,
	))
	if err != nil {
		return "", err
	}
	versionRegexp := regexp.MustCompile(`^Build: {Version:(\d+.*) Commit.*}$`)
	ver := versionRegexp.FindStringSubmatch(strings.TrimSpace(out))[1]
	return ver, nil
}
