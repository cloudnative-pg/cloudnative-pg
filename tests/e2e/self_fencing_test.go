/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package e2e

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/nodes"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// isolationWriteStalenessThreshold bounds how long an isolated primary is allowed to go on
// acknowledging writes from a session opened before the partition. The `cluster-liveness
// -pinger-enabled` fixture leaves `.spec.probes.liveness` unset, so detection follows the
// defaults: `failureThreshold` (3) x `periodSeconds` (10) = 30 seconds before the isolation
// check ever fails once, and a manual reproduction measured 26 seconds end to end. 60 seconds
// gives that a wide margin without getting anywhere near the smart shutdown's default
// `.spec.smartShutdownTimeout` of 180 seconds this spec exists to catch: the assertion only
// has to separate "tens of seconds" from "three-plus minutes", not police a precise timing,
// and a tight bound here would make an already disruptive spec flaky too.
const isolationWriteStalenessThreshold = 60 * time.Second

// isolationWriteAckTolerance is how far after the connection's own termination line a commit
// may still be timestamped before it counts as the primary having gone on serving. It exists
// only to absorb the scheduling jitter between two pipes read by two goroutines, so it is a
// couple of orders of magnitude below anything the bug this spec catches would produce.
const isolationWriteAckTolerance = time.Second

// isolationWriter tracks a single long-lived psql backend, fed by startIsolationWriter: the
// host-side arrival time of the last row it acknowledged, and the host-side arrival time of
// the first line the backend's connection wrote to stderr, which is the positive signal that
// the connection died rather than an inference from the writer falling silent.
type isolationWriter struct {
	mu            sync.Mutex
	last          time.Time
	partitionedAt time.Time
	stderrAt      time.Time
	stderrLines   []string
	stdoutErr     error
	stderrErr     error
	waitErr       error
}

func (w *isolationWriter) record(ts time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ts.After(w.last) {
		w.last = ts
	}
}

func (w *isolationWriter) lastAcknowledged() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.last
}

// recordStderr appends a line the process wrote to stderr, timestamped on host-side arrival,
// and remembers the arrival time of the first one after the partition. A fast shutdown
// terminates open backends, so the psql inside the container writes a diagnostic here
// ("terminating connection due to administrator command", or "server closed the connection
// unexpectedly" once the shutdown escalates to immediate): this is the signal the spec keys
// on, and it is deliberately not matched on message text since the two phases word it
// differently.
//
// Lines arriving before the partition are kept for the diagnostics report but must not be
// allowed to latch stderrAt: anything at all on this stream beforehand (a psql error from a
// loop that started badly, a docker or crictl warning) would otherwise put the arrival time
// before partitionedAt, which trivially satisfies an assertion looking for a line that comes
// soon after it.
func (w *isolationWriter) recordStderr(line string, ts time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stderrLines = append(w.stderrLines, line)
	if w.stderrAt.IsZero() && !w.partitionedAt.IsZero() && ts.After(w.partitionedAt) {
		w.stderrAt = ts
	}
}

// markPartitioned tells the writer when the node was disconnected, so recordStderr can tell
// lines caused by the partition apart from noise that preceded it.
func (w *isolationWriter) markPartitioned(ts time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.partitionedAt = ts
}

func (w *isolationWriter) firstStderrArrival() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stderrAt
}

func (w *isolationWriter) setStdoutErr(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stdoutErr = err
}

func (w *isolationWriter) setStderrErr(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stderrErr = err
}

func (w *isolationWriter) setWaitErr(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.waitErr = err
}

// diagnostics reports everything captured about the process, so that a channel breaking in a
// nightly CI run leaves something readable behind: the exit error, both scanners' errors, and
// the stderr lines themselves.
func (w *isolationWriter) diagnostics() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return fmt.Sprintf(
		"wait error: %v\nstdout scanner error: %v\nstderr scanner error: %v\nstderr lines: %v\n",
		w.waitErr, w.stdoutErr, w.stderrErr, w.stderrLines)
}

// startIsolationWriter opens one psql backend inside the given container and keeps it fed
// with one INSERT every 300ms from a loop running inside the container, so that a single
// backend stays open across the whole spec rather than one connection per statement: a fresh
// connection per INSERT would give a smart shutdown nothing to wait for, so it would
// complete at once and hide the very bug this spec exists to catch.
//
// It goes through the host's Docker daemon (docker exec + crictl exec), the same path
// verifyIsolatedPrimary already uses for crictl ps, rather than kubectl exec: kubectl exec
// reaches the pod through the API server and the kubelet on the node under test, so that
// channel dies the instant the node is disconnected, and measuring through it would measure
// when the tunnel broke rather than when PostgreSQL stopped acknowledging writes.
//
// Everything is timestamped on the host clock at the moment a line is read from either
// stream, rather than parsed out of the server's own clock_timestamp(): the pipe latency is
// negligible against the thresholds this spec checks, and a single clock avoids mixing a
// container-derived timestamp into a host-side time.Since.
func startIsolationWriter(ctx context.Context, node, containerID string) *isolationWriter {
	writer := &isolationWriter{}

	// #nosec G204 -- node and containerID come from crictl/docker output earlier in this spec
	cmd := exec.CommandContext(ctx, "docker", "exec", node,
		"crictl", "exec", containerID, "bash", "-c",
		`while true; do echo "INSERT INTO isolation_writes(ts) VALUES (clock_timestamp()) `+
			`RETURNING 1;"; sleep 0.3; done | psql -U postgres -Atq app`)

	stdout, err := cmd.StdoutPipe()
	Expect(err).ToNot(HaveOccurred())
	stderr, err := cmd.StderrPipe()
	Expect(err).ToNot(HaveOccurred())
	Expect(cmd.Start()).To(Succeed())

	var pipesDone sync.WaitGroup
	pipesDone.Add(2)

	go func() {
		defer pipesDone.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == "" {
				continue
			}
			writer.record(time.Now())
		}
		writer.setStdoutErr(scanner.Err())
	}()

	go func() {
		defer pipesDone.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			writer.recordStderr(scanner.Text(), time.Now())
		}
		writer.setStderrErr(scanner.Err())
	}()

	// cmd.Wait must not run until both pipes are fully drained, so a dedicated goroutine
	// owns the process and records its exit for the diagnostics report: the container being
	// killed by the fast shutdown is itself expected here and must not fail the spec on its
	// own, it is the stderr line that carries the assertion.
	go func() {
		pipesDone.Wait()
		writer.setWaitErr(cmd.Wait())
	}()

	return writer
}

var _ = Describe("Self-fencing with liveness probe", Serial, Label(tests.LabelDisruptive), func() {
	const (
		level           = tests.Lowest
		namespacePrefix = "self-fencing"
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
		if !IsKind() {
			Skip("This test only runs on kind clusters")
		}
	})

	verifyIsolatedPrimary := func(namespace, isolatedPod, isolatedNode string, livenessPingerEnabled bool) {
		By("verifying the isolatedPod behaviour", func() {
			defaultCommand := fmt.Sprintf(
				"docker exec %v crictl ps -a -q "+
					"--label io.kubernetes.pod.namespace=%s,io.kubernetes.pod.name=%s "+
					"--name postgres", isolatedNode, namespace, isolatedPod)

			if livenessPingerEnabled {
				Eventually(func(g Gomega) {
					out, _, err := run.Unchecked(fmt.Sprintf("%s -s Exited", defaultCommand))
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(out).ToNot(BeEmpty())
					if out != "" {
						GinkgoWriter.Printf("Container %s (%s) has been terminated\n",
							isolatedPod, strings.TrimSpace(out))
					}
				}, 120).Should(Succeed())
			} else {
				Consistently(func(g Gomega) {
					out, _, err := run.Unchecked(fmt.Sprintf("%s -s Running", defaultCommand))
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(out).ToNot(BeEmpty())
					if out != "" {
						GinkgoWriter.Printf("Container %s (%s) is still running\n",
							isolatedPod, strings.TrimSpace(out))
					}
				}, 20, 5).Should(Succeed())
			}
		})
	}

	assertLivenessPinger := func(clusterManifest string, livenessPingerEnabled bool) {
		var namespace, clusterName, isolatedNode string
		var err error
		var oldPrimaryPod *corev1.Pod
		var writer *isolationWriter
		var partitionedAt time.Time

		DeferCleanup(func() {
			// Ensure the isolatedNode networking is re-established
			if CurrentSpecReport().Failed() {
				_, _, _ = run.Unchecked(fmt.Sprintf("docker network connect kind %v", isolatedNode))
			}
		})

		By("creating a Cluster", func() {
			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
		})

		By("setting up the environment", func() {
			// Ensure the operator is not running on the same node as the primary.
			// If it is, we switch to a new primary
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			operatorPod, err := operator.GetPod(env.Ctx, env.Client)
			Expect(err).NotTo(HaveOccurred())
			if primaryPod.Spec.NodeName == operatorPod.Spec.NodeName {
				clusterasserts.AssertSwitchover(env, testTimeouts, namespace, clusterName)
			}
		})

		if livenessPingerEnabled {
			By("opening one session on the primary that will outlive the partition", func() {
				oldPrimaryPod, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				isolatedNode = oldPrimaryPod.Spec.NodeName

				_, err = postgres.RunExecOverForward(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					namespace, clusterName, postgres.AppDBName,
					apiv1.ApplicationUserSecretSuffix,
					"CREATE TABLE IF NOT EXISTS isolation_writes (ts timestamptz)")
				Expect(err).ToNot(HaveOccurred())

				containerID, _, err := run.Unchecked(fmt.Sprintf(
					"docker exec %v crictl ps -q "+
						"--label io.kubernetes.pod.namespace=%s,io.kubernetes.pod.name=%s "+
						"--name postgres", isolatedNode, namespace, oldPrimaryPod.Name))
				Expect(err).ToNot(HaveOccurred())
				containerID = strings.TrimSpace(containerID)
				Expect(containerID).ToNot(BeEmpty())

				writerCtx, cancel := context.WithCancel(env.Ctx)
				DeferCleanup(cancel)
				writer = startIsolationWriter(writerCtx, isolatedNode, containerID)

				// Give the loop time to open its one backend and land a first commit
				// before the partition, so that the assertion below measures a session
				// that was genuinely open beforehand rather than one still connecting.
				Eventually(func() time.Time {
					return writer.lastAcknowledged()
				}, 20).ShouldNot(BeZero())
			})
		}

		By("disconnecting the node containing the primary", func() {
			if oldPrimaryPod == nil {
				oldPrimaryPod, err = clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				isolatedNode = oldPrimaryPod.Spec.NodeName
			}
			_, _, err = run.Unchecked(fmt.Sprintf("docker network disconnect kind %v", isolatedNode))
			Expect(err).ToNot(HaveOccurred())
			partitionedAt = time.Now()
			if writer != nil {
				writer.markPartitioned(partitionedAt)
			}
		})

		By("verifying that a new primary has been promoted", func() {
			clusterasserts.AssertClusterEventuallyReachesPhase(env, namespace, clusterName,
				[]string{apiv1.PhaseFailOver}, 120)

			Eventually(func(g Gomega) {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cluster.Status.CurrentPrimary).ToNot(BeEquivalentTo(oldPrimaryPod.Name))
			}, testTimeouts[timeouts.NewPrimaryAfterFailover]).Should(Succeed())
		})

		if livenessPingerEnabled {
			By("verifying the isolated primary stopped acknowledging writes promptly", func() {
				defer func() {
					GinkgoWriter.Printf("isolation writer diagnostics:\n%s", writer.diagnostics())
				}()

				// Wait for the connection to report its own termination, rather than
				// inferring it from the writer falling silent: silence has six unrelated
				// causes (the host-side docker exec client dying, the crictl exec session
				// ending, psql exiting, the in-container bash loop dying, the container
				// being killed, or bufio.Scanner stopping on its own error), and only one
				// of those is the behaviour under test.
				Eventually(func() time.Time {
					return writer.firstStderrArrival()
				}, isolationWriteStalenessThreshold+30*time.Second, 2*time.Second).ShouldNot(BeZero())

				stderrAt := writer.firstStderrArrival()
				Expect(stderrAt.Sub(partitionedAt)).To(
					BeNumerically("<", isolationWriteStalenessThreshold),
					"the isolated primary's connection took %s to report its termination, "+
						"past the %s threshold",
					stderrAt.Sub(partitionedAt), isolationWriteStalenessThreshold)

				// Kept as the secondary assertion: a stderr line proves something on that
				// stream reported trouble, not that PostgreSQL stopped serving, so this
				// watches for commits that keep being acknowledged after it. Checked over
				// a few seconds rather than once, since reading the counter the instant
				// the stderr line lands says nothing about what arrives next.
				//
				// The tolerance is what keeps this off a knife edge: the two streams are
				// drained by their own goroutine and stamped when a line is read, so a
				// commit written a fraction of a millisecond before the termination line
				// can legitimately be read after it, and a bare ordering comparison would
				// then fail with nothing wrong.
				Expect(writer.lastAcknowledged()).ToNot(BeZero())
				Consistently(func() time.Duration {
					return writer.lastAcknowledged().Sub(stderrAt)
				}, 5*time.Second, time.Second).Should(
					BeNumerically("<", isolationWriteAckTolerance),
					"the isolated primary went on acknowledging writes after its connection "+
						"reported termination at %s", stderrAt)
			})
		}

		verifyIsolatedPrimary(namespace, oldPrimaryPod.Name, isolatedNode, livenessPingerEnabled)

		By("reconnecting the isolated Node", func() {
			_, _, err = run.Unchecked(fmt.Sprintf("docker network connect kind %v", isolatedNode))
			Expect(err).ToNot(HaveOccurred())

			// Assert that the oldPrimary comes back as a replica
			namespacedName := types.NamespacedName{
				Namespace: oldPrimaryPod.Namespace,
				Name:      oldPrimaryPod.Name,
			}
			timeout := 180
			Eventually(func(g Gomega) {
				pod := corev1.Pod{}
				err := env.Client.Get(env.Ctx, namespacedName, &pod)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(utils.IsPodActive(pod)).To(BeTrue())
				g.Expect(utils.IsPodReady(pod)).To(BeTrue())
				g.Expect(specs.IsPodStandby(pod)).To(BeTrue())
				g.Expect(nodes.IsNodeReachable(env.Ctx, env.Client, isolatedNode)).To(BeTrue())
			}, timeout).Should(Succeed())
		})
	}

	When("livenessPinger is enabled", func() {
		const sampleFile = fixturesDir + "/self-fencing/cluster-liveness-pinger-enabled.yaml.template"
		It("will terminate an isolated primary", func() {
			assertLivenessPinger(sampleFile, true)
		})
	})

	When("livenessPinger is disabled", func() {
		const sampleFile = fixturesDir + "/self-fencing/cluster-liveness-pinger-disabled.yaml.template"
		It("will not restart an isolated primary", func() {
			assertLivenessPinger(sampleFile, false)
		})
	})
})
