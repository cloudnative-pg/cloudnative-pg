# Primary Lease Model

Models what happens after a CloudNativePG primary loses its lease
when fail-safe mode is active — i.e. a **non-cooperative** old
primary that stops leading but cannot unlock the lease.

Background: `docs/src/failover.md` ("Safe primary election").

## Files

* `PrimaryLease.tla` — PlusCal algorithm plus its TLA+
  translation (do not edit the `BEGIN TRANSLATION` block by hand;
  re-run the PlusCal translator, e.g.
  `java -cp tla2tools.jar pcal.trans -nocfg PrimaryLease.tla`).
* `PrimaryLease.cfg` — default TLC model configuration.

## Configuration

Defaults in `PrimaryLease.cfg` (small values for exhaustive
checking, not production timings):

| Constant | Default | Meaning |
|---|---|---|
| `Nodes` | `{"one", "two"}` | Checked instances |
| `MaxRounds` | `15` | Bound on global `tick`; makes the state space finite |
| `LeaseDuration` | `5` | Owned lease must be observed unchanged for `> LeaseDuration` before take-over |
| `RenewDeadline` | `2` | Holder that has not renewed within this is treated as having lost the lease |
| `RenewPeriod` | `1` | Minimum spacing between holder renewals |
| `LivenessThreshold` | `2` | `IsAlive(n)` iff `tick - watchdogTick[n] <= threshold`; must not exceed `LeaseDuration - RenewDeadline` (see below) |

Production defaults live under `.spec.primaryLease`
(`leaseDurationSeconds`, `renewDeadlineSeconds`, `retryPeriodSeconds`);
see `failover.md#tuning-the-primary-lease`.

## State

Shared (the Kubernetes `Lease` + liveness):

* `leaseHolder`, `leaseRenewTick` — current holder and last renewal tick.
* `watchdogTick[n]` — last tick `n` petted its watchdog (liveness probe).
* `leading[n]` — `n` is acting as PostgreSQL primary.
* `lookingForLease[n]` — `n` is inside the take-over observation window.
* `tick` — single global discrete clock. Assumption: **no clock skew**.

Per instance (stale reads + local timers):

* `observedLeaseHolder`, `observedLeaseRenewTick` — last observed lease state.
* `lastRenew` — last tick this instance renewed (`-1` means never held;
  a claim at tick `0` is therefore distinguishable from "never held").
* `lastLeaseChange`, `lastLeaseRenewTick` — when the current observation
  window started and what renew tick it saw first.

## Actions

* `InstanceObserveLease` — refresh a stale view of the lease. Observing
  a renewal mid-window abandons the contest (window reset,
  `lookingForLease` cleared), so the flag means "inside one unbroken
  observation window"; re-contesting starts a fresh window.
* `HolderRenewLeaseSucceeds` / `HolderRenewLeaseFail` — holder renews
  (compare-and-swap on the observed record) or pets the watchdog only.
* `PrimaryInstanceLostLeaseAndOtherNodesUnreachable` — old primary past
  `RenewDeadline` steps down silently.
* `PrimaryInstanceLostLeaseAndOtherNodesAgreeOnMe` — old primary past
  `RenewDeadline` keeps `leading` while no candidate is contesting.
* `PrimaryInstanceLostLeaseAndOtherNodesDoNotAgreeOnMe` — old primary
  steps down once some candidate sets `lookingForLease`.
* `CandidatePrimaryClaimEmptyLease` — immediate take-over of a released
  (empty-holder) lease.
* `CandidatePrimaryStartClaimOwnedLease` — enter the observation window.
* `CandidatePetWatchdogWhileWaitingLease` — stay alive while waiting.
* `CandidatePrimaryClaimOwnedLease` — take over after the record sat
  unchanged for `> LeaseDuration` (CAS against concurrent writers).

## Checked properties

* `NoTwoLeaders` — no two *alive* nodes have `leading = TRUE`.
  Liveness-gated by design: see "Safety is qualified by liveness" below.
* `TypeOK` — typing invariant over the shared state (holder, renew tick,
  watchdog ticks, `leading`, `lookingForLease`, clock).

The spec also `ASSUME`s `LeaseDuration > RenewDeadline > RenewPeriod > 0`,
so a misconfigured timing set fails fast instead of being silently
checked. The admission webhook enforces `leaseDurationSeconds >
renewDeadlineSeconds` and the stricter `renewDeadlineSeconds >
1.2 * retryPeriodSeconds`.

`NoTwoLeaders` holds only while `LivenessThreshold <= LeaseDuration -
RenewDeadline`. A holder can pet its watchdog `RenewDeadline` ticks
after its last renewal, even while a candidate contests, and then stop
acting while `leading` is still set. A candidate can claim the lease
`LeaseDuration + 1` ticks after that renewal. With a larger
`LivenessThreshold` the old holder is still alive at that point, so both
nodes are alive leaders. This is the model's counterpart of the
requirement that an isolated primary stops before a replica may promote.
The relation is not `ASSUME`d, so raising `LivenessThreshold` past the
bound shows the counterexample.

## Assumptions and limits

* Discrete global time, no skew between instances. This is a modeling
  simplification, not a requirement of the mechanism: Kubernetes Leases
  (and this design, like client-go leader election) are resistant to
  clock skew. The holder's clock is only ever compared for equality
  ("did the holder write since I last looked"), never for ordering, and
  the take-over wait is measured on the candidate's own clock — so skew
  between candidate and previous holder cannot trigger a premature
  take-over. The spec mirrors this: `lastLeaseRenewTick =
  observedLeaseRenewTick` is equality-only, and the `LeaseDuration` wait
  is counted in locally observed ticks.
* Two nodes, bounded time (`MaxRounds`) - liveness is out of scope,
  only safety (`NoTwoLeaders`) is checked.
* Network/API-server failures are abstracted as "renew does not land"
  plus watchdog liveness, not as explicit partitions.
* The three `PrimaryInstanceLostLease*` actions stand for the outcomes
  of the peer probe, not for the probe itself. `lookingForLease` stands
  for the target primary that peers report through the `/failsafe`
  endpoint, and the old primary reads it atomically. The probe and
  shutdown latency is the time an old primary may delay its step-down:
  past the deadline it cannot pet its watchdog while a candidate
  contests, so that delay is bounded by `LivenessThreshold`.
* The lease starts with an empty holder, and claiming it is the only
  use of the empty-holder fast path. No action releases the lease, so the
  clean hand-over after a graceful shutdown is not modeled: after the
  first claim every take-over goes through the expiry slow path.
* Fencing and the primary isolation check are not modeled as mechanisms:
  there is no connectivity probing, no liveness-probe failure, and no
  kubelet-driven restart/shutdown in the spec. Their effect is assumed
  instead — a node that stops acting falls behind the global clock,
  stops being `IsAlive`, and is excluded from `NoTwoLeaders` (see
  "Safety is qualified by liveness" below).

### Safety is qualified by liveness

`NoTwoLeaders` deliberately quantifies only over *alive* nodes
(`IsAlive(n)` — still petting the watchdog within `LivenessThreshold`).
A node that stops petting is excluded from the invariant, even if its
`leading` flag is still set. This mirrors Kubernetes `Lease` semantics:
the lease object alone does not fence a holder that lost API-server
contact but keeps serving - fencing comes from the liveness probe
(the primary isolation check), which makes the kubelet restart the
isolated pod. In other words, the model proves "at most one *live*
primary", and the liveness/isolation machinery is what turns a
partitioned primary into a non-live one. Keep both mechanisms (lease +
isolation check) enabled, as documented in
`docs/src/failover.md#relationship-with-the-primary-isolation-check`.
