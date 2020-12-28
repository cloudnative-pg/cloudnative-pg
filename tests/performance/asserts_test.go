package performance

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	clusterv1alpha1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func AssertStandbysFollowPromotion(namespace string, clusterName string, timeout int32) {
	// Track the start of the assert. We expect to complete before
	// timeout.
	start := time.Now()

	By("having all the instances on timeline 2", func() {
		// One of the standbys will be promoted and the rw service
		// should point to it, so the application can keep writing.
		// Records inserted after the promotion will be marked
		// with timeline '00000002'. If all the instances are back
		// and are following the promotion, we should find those
		// records on each of them.
		// We check all of them using the

		commandTimeout := time.Second * 2
		for i := 1; i < 4; i++ {
			podName := fmt.Sprintf("%v-%v", clusterName, i)
			podNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			Eventually(func() (string, error) {
				pod := &corev1.Pod{}
				if err := env.Client.Get(env.Ctx, podNamespacedName, pod); err != nil {
					return "", err
				}
				out, _, err := env.ExecCommand(env.Ctx, *pod, "postgres",
					&commandTimeout, "psql", "-U", "postgres", "app", "-tAc",
					"SELECT count(*) > 0 FROM tps.tl "+
						"WHERE timeline = '00000002'")
				return strings.TrimSpace(out), err
			}, timeout).Should(BeEquivalentTo("t"),
				"Pod %v should have moved to timeline 2", podName)
		}
	})

	By("having all the instances ready", func() {
		clusterNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}
		cluster := &clusterv1alpha1.Cluster{}
		err := env.Client.Get(env.Ctx, clusterNamespacedName, cluster)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() (int32, error) {
			cluster := &clusterv1alpha1.Cluster{}
			err := env.Client.Get(env.Ctx, clusterNamespacedName, cluster)
			return cluster.Status.ReadyInstances, err
		}, timeout).Should(BeEquivalentTo(cluster.Spec.Instances))
	})

	By(fmt.Sprintf("restoring full cluster functionality within %v seconds", timeout), func() {
		elapsed := time.Since(start)
		fmt.Printf("Cluster has been in a degraded state for %v seconds\n", elapsed)
		Expect(elapsed.Seconds()).To(BeNumerically("<", timeout))
	})
}

func AssertWritesResumedBeforeTimeout(namespace string, clusterName string, timeout int32) {
	By(fmt.Sprintf("resuming writing in less than %v sec", timeout), func() {
		// We measure the difference between the last entry with
		// timeline 1 and the first one with timeline 2.
		// It should be less than maxFailoverTime seconds.
		// Any pod is good to measure the difference, we choose -2
		query := "WITH a AS ( " +
			"  SELECT * " +
			"  , t-lag(t) OVER (order by t) AS timediff " +
			"  FROM tps.tl " +
			") " +
			"SELECT EXTRACT ('EPOCH' FROM timediff) " +
			"FROM a " +
			"WHERE timeline = ( " +
			"  SELECT timeline " +
			"  FROM tps.tl " +
			"  ORDER BY t DESC " +
			"  LIMIT 1 " +
			") " +
			"ORDER BY t ASC " +
			"LIMIT 1;"
		podName := clusterName + "-2"
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		var switchTime float64
		commandTimeout := time.Second * 5
		pod := &corev1.Pod{}
		err := env.Client.Get(env.Ctx, namespacedName, pod)
		Expect(err).ToNot(HaveOccurred())
		out, _, _ := env.ExecCommand(env.Ctx, *pod, "postgres",
			&commandTimeout, "psql", "-U", "postgres", "app", "-tAc", query)
		switchTime, err = strconv.ParseFloat(strings.TrimSpace(out), 64)
		fmt.Printf("Write activity resumed in %v seconds\n", switchTime)
		Expect(switchTime, err).Should(BeNumerically("<", timeout))
	})
}
