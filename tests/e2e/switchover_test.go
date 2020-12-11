package e2e

import (
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	clusterv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Switchover", func() {

	It("reacts to switchover requests", func() {
		const namespace = "switchover-e2e"
		const sampleFile = samplesDir + "/cluster-example.yaml"
		const clusterName = "cluster-example"
		// Create a cluster in a namespace we'll delete after the test
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		defer func() {
			err := env.DeleteNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
		}()

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		var pods []string
		var oldPrimary, targetPrimary string

		// First we check that the starting situation is the expected one
		By("checking that CurrentPrimary and TargetPrimary are the same", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			cluster := &clusterv1alpha1.Cluster{}
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(cluster.Status.CurrentPrimary, err).To(BeEquivalentTo(cluster.Status.TargetPrimary))
			oldPrimary = cluster.Status.CurrentPrimary

			// Gather pod names
			podList, err := env.GetClusterPodList(namespace, clusterName)
			Expect(len(podList.Items), err).To(BeEquivalentTo(3))
			for _, p := range podList.Items {
				pods = append(pods, p.Name)
			}
			sort.Strings(pods)
			Expect(pods[0]).To(BeEquivalentTo(oldPrimary))
			targetPrimary = pods[1]
		})

		By("setting the TargetPrimary node to trigger a switchover", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			cluster := &clusterv1alpha1.Cluster{}
			err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				Expect(err).ToNot(HaveOccurred())
				cluster.Status.TargetPrimary = targetPrimary
				return env.Client.Status().Update(env.Ctx, cluster)
			})
			Expect(err).ToNot(HaveOccurred())
		})

		By("waiting that the TargetPrimary become also CurrentPrimary", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			timeout := 45
			Eventually(func() (string, error) {
				cluster := &clusterv1alpha1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cluster)
				return cluster.Status.CurrentPrimary, err
			}, timeout).Should(BeEquivalentTo(targetPrimary))
		})

		By("waiting that the old primary become ready", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      oldPrimary,
			}
			timeout := 45
			Eventually(func() (bool, error) {
				pod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, namespacedName, &pod)
				return utils.IsPodActive(pod) && utils.IsPodReady(pod), err
			}, timeout).Should(BeTrue())
		})

		By("waiting that the old primary become a standby", func() {
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      oldPrimary,
			}
			timeout := 45
			Eventually(func() (bool, error) {
				pod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, namespacedName, &pod)
				return specs.IsPodStandby(pod), err
			}, timeout).Should(BeTrue())
		})
	})
})
