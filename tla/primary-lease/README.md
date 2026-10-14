# Primary Lease Model

A model of what happens after a CloudNativePG primary loses its lease
when fail-safe mode is active, i.e. a **non-cooperative** old primary
that stops leading but cannot unlock the lease. The old primary is
fenced by the lease-age monitor (`monitorLeaseAge` in
`internal/cmd/manager/instance/run/lease/stepdown.go`): once its last
confirmed lease write is older than `RenewDeadline` it runs a peer
check, and once it has decided to step down it is stopped before its
lease age reaches `LeaseDuration`, by the hard stop
(`killPostgresAndExit`) if the soft stop did not land first.

Background: `docs/src/failover.md` ("Safe primary election").

The spec passes TLC with its default `.cfg`. It proves `NoTwoLeaders`
only under a timing relation that is documented, not `ASSUME`d, so that
breaking it shows the counterexample (see "What is proved").

## Files

* `PrimaryLease.tla`, `PrimaryLease.cfg`: the model and its default
  constants.
* The `.tla` holds the PlusCal algorithm plus its TLA+ translation. Do
  not edit the `BEGIN TRANSLATION` block by hand; re-run the translator,
  e.g. `java -cp tla2tools.jar pcal.trans -nocfg PrimaryLease.tla`.

## Configuration

Defaults in the `.cfg` file (small values for exhaustive checking, not
production timings):

| Constant | Default | Meaning |
|---|---|---|
| `Nodes` | `{"one", "two"}` | Checked instances |
| `MaxRounds` | `15` | Bound on global `tick`; makes the state space finite |
| `LeaseDuration` | `5` | Owned lease must be observed unchanged for `>= LeaseDuration` before take-over |
| `RenewDeadline` | `2` | Holder that has not renewed within this is treated as having lost the lease |
| `RenewPeriod` | `1` | Minimum spacing between holder renewals |
| `StepDownDelay` | `1` | Ticks an overdue leader may take to run one peer check |
| `DeliveryDelay` | `1` | Ticks between a step-down verdict and the soft stop; the hard stop does not depend on it |

### Mapping to production parameters

Production values live under `.spec.primaryLease` and
`.spec.probes.liveness.isolationCheck`; see
`failover.md#tuning-the-primary-lease`.

| Constant | Production counterpart |
|---|---|
| `LeaseDuration` | `leaseDurationSeconds`, default 15s |
| `RenewDeadline` | `renewDeadlineSeconds`, default 10s: `monitorLeaseAge` trips once the last confirmed write is older than this |
| `RenewPeriod` | `retryPeriodSeconds`, default 2s (client-go renew cadence and the monitor ticker) |
| `StepDownDelay` | `retryPeriodSeconds` + `isolationCheck.requestTimeout` (2s + 1000ms): the ticker phase plus one parallel probe round |
| `DeliveryDelay` | `W + X`, neither bounded by the code: `W` is the wait for the lifecycle manager to accept `TryRequestImmediateShutdown` (non-blocking send, retried every `retryPeriodSeconds`), `X` the time `pg_ctl stop -m immediate` takes to deliver the signal (a failure is logged and not retried). The hard stop caps both: on every tick after the verdict, if `age + retryPeriodSeconds >= leaseDurationSeconds`, `killPostgresAndExit` sends SIGQUIT and SIGKILL to the postmaster and exits the instance manager |

With the production defaults `RenewDeadline + StepDownDelay <
LeaseDuration` reads 10s + (2s + 1s) < 15s. The webhook warns when
`renewDeadlineSeconds + retryPeriodSeconds + isolationCheck.requestTimeout
>= leaseDurationSeconds`, which is this relation. `W + X` does not
appear: the hard stop fires at the last monitor tick before the lease
can expire, whether or not the soft stop was accepted.

## State

Shared (the Kubernetes `Lease` plus the monitor's view of it):

* `leaseHolder`, `leaseRenewTick`: current holder and last renewal tick.
* `leading[n]`: `n` is acting as PostgreSQL primary.
* `lookingForLease[n]`: `n` is inside the take-over observation window.
* `tick`: single global discrete clock. Assumption: **no clock skew**.
* `lastRenew[n]`: tick of the last successful write naming `n` as
  holder (the `confirmingLock` stamp).
* `lastCheck[n]`: last peer check run while overdue.
* `stepDownAt[n]`: tick of the step-down verdict, `-1` if none.

Per instance (stale reads + local timers):

* `observedLeaseHolder`, `observedLeaseRenewTick`: last observed lease
  state.
* `lastLeaseChange`, `lastLeaseRenewTick`: when the current observation
  window started and what renew tick it saw first.

## Actions

Holder side:

* `HolderRenewLease`: renew; allowed at any age, as `preAcquire`
  re-adopts an "already ours" lease and `le.Run` writes again. Never
  forced, so "the renewal loop is wedged but the monitor still runs" is
  one of the explored behaviours. `lastRenew` moves only on a successful
  CAS, as `confirmingLock` stamps only successful writes.
* `MonitorDecidesStepDown` / `MonitorKeepsLeading`: the peer check
  (`shouldStepDown`), enabled once `tick - lastRenew > RenewDeadline`.
  The first needs no precondition: any probe may fail. The second needs
  every peer to be reachable and to name us. Both record `lastCheck`;
  the Ticker will not pass `StepDownDelay` ticks after the trigger, or
  after the previous check, while a check is pending.
* `DeliverShutdown`: the soft stop: the verdict reaches the lifecycle
  manager and PostgreSQL stops. The Ticker will not pass `DeliveryDelay`
  ticks after the verdict while it is pending. The verdict is taken once
  and only the delivery is retried, as in `monitorLeaseAge`.
* `MonitorHardStop`: the hard stop: once a verdict exists and `tick -
  lastRenew + RenewPeriod >= LeaseDuration` (the next monitor tick would
  be too late), the node stops leading at once, as `killPostgresAndExit`
  signals the postmaster and exits. The Ticker will not let a decided
  leader's age reach `LeaseDuration`. A node stopped by either stop keeps
  its stale record and cannot act until another node claims it: the
  code's re-adoption of the node's own record, a conditional write, is not
  modeled.

Candidate side:

* `InstanceObserveLease`: refresh a stale view of the lease. Observing
  a renewal mid-window abandons the contest (window reset,
  `lookingForLease` cleared), so the flag means "inside one unbroken
  observation window"; re-contesting starts a fresh window.
* `CandidatePrimaryClaimEmptyLease`: immediate take-over of a released
  (empty-holder) lease.
* `CandidatePrimaryStartClaimOwnedLease`: enter the observation window.
* `CandidatePrimaryClaimOwnedLease`: take over after the record sat
  unchanged for `>= LeaseDuration` (CAS against concurrent writers), the
  same comparison `tryTakeOver` makes.

## Checked properties

* `NoTwoLeaders`: no two nodes have `leading = TRUE`. Every node counts,
  dead or alive.
* `TypeOK`: typing invariant over the shared state.

The spec `ASSUME`s `LeaseDuration > RenewDeadline > RenewPeriod > 0`,
so a misconfigured timing set fails fast instead of being silently
checked. The admission webhook enforces `leaseDurationSeconds >
renewDeadlineSeconds` and the stricter `renewDeadlineSeconds >
1.2 * retryPeriodSeconds`.

## What is proved

TLC shows `NoTwoLeaders` holds iff

```
RenewDeadline + StepDownDelay < LeaseDuration
```

for any `DeliveryDelay`. Runs with `LeaseDuration = 5`, `RenewPeriod =
1`, default `MaxRounds`:

| `RenewDeadline` | `StepDownDelay` | `DeliveryDelay` | `RD + SDD` | Result |
|---|---|---|---|---|
| 2 | 1 | 1 | 3 | pass |
| 2 | 1 | 9 | 3 | pass |
| 2 | 2 | 1 | 4 | pass |
| 3 | 1 | 1 | 4 | pass |
| 2 | 3 | 1 | 5 | violation |
| 3 | 2 | 0 | 5 | violation |

Why: the candidate saw the record last change at tick `r` and claims at
`r + LeaseDuration`. The holder trips once its write is older than
`RenewDeadline` and must decide by `r + RenewDeadline + StepDownDelay`.
Once decided it is stopped at the latest at `r + LeaseDuration - 1`, by
the hard stop if the soft stop did not land first. The two overlap iff
the verdict itself can land at `r + LeaseDuration` or later, that is iff
`RenewDeadline + StepDownDelay >= LeaseDuration`. Rows 2 to 4 were
violations before the hard stop, when the bound was `RenewDeadline +
StepDownDelay + DeliveryDelay < LeaseDuration`.

Mutations, all with `RenewDeadline = 2`, `StepDownDelay = 1`:

| Change | `DeliveryDelay` | Result |
|---|---|---|
| hard stop removed | 1 | pass |
| hard stop removed | 9 | violation |
| hard stop at `age >= LeaseDuration` instead of `age + RenewPeriod >= LeaseDuration` | 1 | pass |
| same | 9 | violation |
| soft stop split into "request accepted" (never forced) and "stopped", `DeliveryDelay` bound dropped, hard stop intact | n/a | pass |
| same, and the Ticker's age bound on a decided leader also waits for the request to be accepted | n/a | violation |

The first two rows are the old bound coming back. Rows 3 and 4 are the
tie: killing at age `LeaseDuration` is the tick the candidate claims,
and only a fast soft stop hides it. The last two are `W` unbounded: the
hard stop must not wait for the lifecycle manager. A hard stop that waits
is only caught when the Ticker's age bound waits with it, as that bound
stands for the monitor's tick.

In production units `StepDownDelay` is `retryPeriodSeconds +
isolationCheck.requestTimeout`, so the relation is the one the webhook
warns about: `renewDeadlineSeconds + retryPeriodSeconds +
isolationCheck.requestTimeout < leaseDurationSeconds`. The probe time is
inside it because `monitorLeaseAge` reads `age` at the start of the tick
and tests the hard stop with that same value after the probe: a probe
that ends past `leaseDurationSeconds` kills too late, and the model's
`StepDownDelay` bound on the verdict is what rules that out.

One branch is outside this relation. If the peers first agree that the
node is the primary and a verdict only comes later, because a challenger's
cached target flips after a probe that saw it agreeing, the verdict tick
can run up to `retryPeriodSeconds + 2 * requestTimeout` after the flip,
since the previous probe may have read the challenger just before it. The
challenger claims no earlier than `leaseDurationSeconds` after the flip. The
model stops the node in the verdict tick, through the Ticker's age bound, so
for this branch it assumes that `retryPeriodSeconds + 2 * requestTimeout <
leaseDurationSeconds`, which nothing validates. At the defaults it is 4s
against 15s.

So the spec proves: **if** every overdue leader runs its peer check
within `StepDownDelay` ticks of tripping (and of its previous check),
**and** a decided leader is stopped before its lease age reaches
`LeaseDuration`, **then** no two leaders exist. It assumes, and does not
check, that the code keeps those two bounds; the second rests on the
process exiting on `os.Exit` and the kernel killing the rest of the PID
namespace. It also assumes the peer check reaches a verdict:
`shouldStepDown` returns "keep leading" without probing when the
isolation check is disabled or the cluster has one instance, and retries
without a verdict when it cannot build the probe (unreadable CA); neither
case is modeled.

## Assumptions and limits

* Discrete global time, no skew between instances. This is a modeling
  simplification, not a requirement of the mechanism: Kubernetes Leases
  (and this design, like client-go leader election) are resistant to
  clock skew. The holder's clock is only ever compared for equality
  ("did the holder write since I last looked"), never for ordering, and
  the take-over wait is measured on the candidate's own clock, so skew
  between candidate and previous holder cannot trigger a premature
  take-over. The spec mirrors this: `lastLeaseRenewTick =
  observedLeaseRenewTick` is equality-only, and the `LeaseDuration` wait
  is counted in locally observed ticks.
* No API-call latency: a write is atomic with the server-side commit, so
  the tick a holder renews is the tick the candidates can see it. The
  code approximates this by aging the lease from the start of the write,
  on a monotonic clock.
* Two nodes, bounded time (`MaxRounds`): liveness is out of scope,
  only safety (`NoTwoLeaders`) is checked.
* Network/API-server failures are abstracted as "renew does not land"
  or "a probe fails", not as explicit partitions.
* The step-down actions stand for the outcomes of the peer probe, not
  for the probe itself. `lookingForLease` stands for the target primary
  that peers report through the `/failsafe` endpoint, and the old
  primary reads it atomically. A peer that already holds the lease
  counts as naming itself.
* The lease starts with an empty holder, and claiming it is the only
  use of the empty-holder fast path. No action releases the lease, so the
  clean hand-over after a graceful shutdown is not modeled: after the
  first claim every take-over goes through the expiry slow path.
* No operator and no `pg_promote` time: a claim makes the node lead at
  once, which is the worst case for the old primary.
* Every node may contest the lease. The code lets only the target
  primary contest, and re-adopts its own record with a conditional
  write; neither is modeled. Letting all nodes contest only adds
  behaviours, so it cannot hide a violation.
* The check, the soft stop and the hard stop are forced by the Ticker
  guard, not by fairness. A fenced node keeps its stale `leaseHolder`
  and cannot act until another node claims it: the code's re-adoption of the
  node's own record, a conditional write, is not modeled.
* The hard stop is instant. In the code it is one `kill(2)` call and
  `os.Exit`, with the PID taken from memory; the exit of PID 1 is what makes the
  kernel SIGKILL every process left in the container. Not modeled:
  scheduling latency of the monitor goroutine between the tick and the
  exit, a pod with a shared process namespace (there only the postmaster
  gets SIGQUIT, and a backend mid-statement can still commit until that
  SIGQUIT reaches it), and the monitor's `age` being read at the start
  of the tick, before the peer probe. That last point is why the
  relation needs the probe time inside `StepDownDelay`: see "What is
  proved".

## Relation to the watchdog

PR #11358 adds a liveness watchdog on top of this: the liveness probe
fails when the lease loop stops attempting lock operations, or when
PostgreSQL ignores an immediate shutdown for too long, and the kubelet
then restarts the container. That chain takes tens of seconds, beyond
`LeaseDuration`, so it is a backstop outside the safety bound proved
here, not part of it. The PR that adds the watchdog should extend this
model.
