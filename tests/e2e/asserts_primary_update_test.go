package e2e

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	corev1 "k8s.io/api/core/v1"

	. "github.com/onsi/gomega"
)

// AssertPrimary verifies that the -rw endpoint points to the expected primary,
// and checks if the new primary is the same as before (Restart)
// or has changed (Switchover)
func AssertPrimary(
	namespace, clusterName string,
	oldPrimaryPod *corev1.Pod, primaryUpdateMethod apiv1.PrimaryUpdateMethod,
) {
	var cluster *apiv1.Cluster
	var err error

	Eventually(func(g Gomega) {
		cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		g.Expect(err).ToNot(HaveOccurred())
		if primaryUpdateMethod == apiv1.PrimaryUpdateMethodSwitchover {
			g.Expect(cluster.Status.CurrentPrimary).ToNot(BeEquivalentTo(oldPrimaryPod.Name))
		} else {
			g.Expect(cluster.Status.CurrentPrimary).To(BeEquivalentTo(oldPrimaryPod.Name))
		}
	}, RetryTimeout).Should(Succeed())

	// Get the new current primary Pod
	currentPrimaryPod, err := podutils.Get(env.Ctx, env.Client, namespace, cluster.Status.CurrentPrimary)
	Expect(err).ToNot(HaveOccurred())

	endpointName := clusterName + "-rw"
	// we give 10 seconds to the apiserver to update the endpoint
	timeout := 10
	Eventually(func() (string, error) {
		endpointSlice, err := testsUtils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, endpointName)
		return testsUtils.FirstEndpointSliceIP(endpointSlice), err
	}, timeout).Should(BeEquivalentTo(currentPrimaryPod.Status.PodIP))
}
