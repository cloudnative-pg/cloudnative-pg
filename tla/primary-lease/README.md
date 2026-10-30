# Primary Lease Model

A model of what happens after a CloudNativePG primary loses its lease
when fail-safe mode is active, i.e. a **non-cooperative** old primary
that stops leading but cannot unlock the lease. The old primary is
fenced by the lease-age monitor (`monitorLeaseAge` in
`internal/cmd/manager/instance/run/lease/stepdown.go`): once its last
confirmed lease write is older than `RenewDeadline` it runs a peer
check, and once it has decided to step down it is stopped before its
lease age reaches `LeaseDuration`, by the hard stop
(`killPostgresAndExit`) if the soft stop did not land first. A leader
whose lease loop has wedged is stopped by the liveness watchdog instead.

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

| Constant | Default | Meaning | Production counterpart |
|---|---|---|---|
| `Nodes` | `{"one", "two"}` | Checked instances | |
| `MaxRounds` | `12` | Bound on global `tick`; makes the state space finite | |
| `LeaseDuration` | `5` | Owned lease must be observed unchanged for `>= LeaseDuration` before take-over | `leaseDurationSeconds`, default 15s |
| `RenewDeadline` | `2` | Holder whose last confirmed write is older than this is overdue | `renewDeadlineSeconds`, default 10s |
| `RenewPeriod` | `1` | Minimum spacing between holder renewals; also the monitor's tick | `retryPeriodSeconds`, default 2s |
| `StepDownDelay` | `1` | Ticks an overdue leader may take to run one peer check | `retryPeriodSeconds` + `isolationCheck.requestTimeout` (2s + 1000ms): the ticker phase plus one parallel probe round |
| `DeliveryDelay` | `1` | Ticks between a step-down verdict and the soft stop | `W + X`, neither bounded by the code: `W` is the wait for the lifecycle manager to accept `TryRequestImmediateShutdown` (non-blocking send, retried every `retryPeriodSeconds`), `X` the time `pg_ctl stop -m immediate` takes to deliver the signal (a failure is logged and not retried) |
| `WatchdogMaxSilence` | `2` | Silence of the lease loop after which the liveness probe fails | `defaultWatchdogMaxSilence`, `renewDeadlineSeconds + 2 * retryPeriodSeconds`, 14s at the defaults |
| `ProbeDelay` | `6` | Ticks from the silence bound being crossed to the kubelet kill | liveness `periodSeconds * failureThreshold`, 10s * 3 = 30s at the defaults |
| `MonitorWorks` | `TRUE` | `FALSE` switches the lease-age monitor off | none: stands for a `monitorLeaseAge` goroutine that no longer runs |

Production values live under `.spec.primaryLease` and
`.spec.probes.liveness.isolationCheck`; see
`failover.md#tuning-the-primary-lease`.

## State

Shared (the Kubernetes `Lease` plus the monitor's view of it):

* `leaseHolder`, `leaseRenewTick`: current holder and last renewal tick.
* `held[n]`: `n` holds the lease: its renewal loop and monitor run.
* `leading[n]`: `n`'s PostgreSQL is writable.
* `promoReq[n]`: `n` asked PostgreSQL to promote (`promotionRequested`).
* `yielded[n]`: `n` stopped renewing a record that still names it.
* `lookingForLease[n]`: `n` is inside the take-over observation window.
* `tick`: single global discrete clock. Assumption: **no clock skew**.
* `lastRenew[n]`: tick of the last successful write naming `n` as
  holder (the `confirmingLock` stamp).
* `lastCheck[n]`: last peer check run while overdue.
* `stepDownAt[n]`: tick of the step-down verdict, `-1` if none.
* `stuckAt[n]`: `-1` while `n`'s lease loop is alive, else the tick of
  its last lock attempt (the last watchdog beat).

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
  CAS, as `confirmingLock` stamps only successful writes. The same
  behaviour stands for `Runnable.StopRenewing`, used by a primary whose write
  path is stalled (`.spec.probes.liveness.writePathCheck`): it stops the
  renewals but, unlike `Runnable.Yield`, keeps the monitor, so the node stays
  `held` and is stopped by the monitor as above. The model allows a renewal at
  any time, so it covers a node that never renews. A successful
  renewal also clears `stepDownAt`: the monitor sees a fresh lease and
  withdraws a verdict that was not delivered yet.
* `MonitorDecidesStepDown` / `MonitorKeepsLeading`: the peer check
  (`shouldStepDown`), enabled once `tick - lastRenew > RenewDeadline`.
  The first needs no precondition: any probe may fail. The second needs
  every peer to be reachable and to name us. Both record `lastCheck`;
  the Ticker will not pass `StepDownDelay` ticks after the trigger, or
  after the previous check, while a check is pending.
* `DeliverShutdown`: the soft stop: the verdict reaches the lifecycle
  manager and PostgreSQL stops. The Ticker will not pass `DeliveryDelay`
  ticks after the verdict while it is pending. The verdict is taken once
  and only the delivery is retried, as in `monitorLeaseAge`, until a
  renewal withdraws it.
* `MonitorHardStop`: the hard stop: once a verdict exists and `tick -
  lastRenew + RenewPeriod >= LeaseDuration` (the next monitor tick would
  be too late), the node stops leading at once, as `killPostgresAndExit`
  signals the postmaster and exits. The Ticker will not let a decided
  leader's age reach `LeaseDuration`.

A node stopped by either stop keeps its stale record and cannot act
until another node claims it: the code's re-adoption of the node's own
record, a conditional write, is modeled only after a yield.

Watchdog side (see "The lease watchdog"):

* `LeaseLoopStuck`: the lease loop of a leader stops attempting lock
  operations; `stuckAt` records its last attempt. A loop that is alive
  but cannot renew is not this fault: it beats the watchdog on every
  attempt, and the model already covers it by never forcing a renewal.
* `DeliverShutdown` is also enabled for a stuck leader whose silence
  exceeds `WatchdogMaxSilence`: the probe handler asks for the immediate
  shutdown, without any peer check.
* `KubeletKill`: the kubelet kills a stuck leader at the latest
  `WatchdogMaxSilence + ProbeDelay` ticks after its last attempt. The
  Ticker will not pass that tick.

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

Both claims make the node hold the lease (`held`), not lead:

* `StartPromotion`: a holder asks PostgreSQL to promote. The operator is
  not modeled, so any holder may be the target at any time.
* `PromoteComplete`: `pg_promote` finished. Never forced.
* `YieldLease` (`Runnable.Yield`): a holder that stopped being the
  target before asking to promote stops its renewal loop and its
  monitor and writes nothing: the record goes stale and the new target
  takes it over through the observation window. The `~promoReq` guard
  (`promotionRequested` in `yieldPrimaryLeaseIfNotTarget`) is needed: a
  promotion once asked may still complete, so that node must keep its
  monitor. Without the guard `NoTwoLeaders` fails.
* `ReacquireOwn`: a yielded node that is the target again refreshes the
  record it still owns, with no window (the "already ours" CAS of
  `tryTakeOver`).

The stops also clear `held` and `promoReq`.

## Checked properties

* `NoTwoLeaders`: no two nodes have `leading = TRUE`. Every node counts,
  dead or alive.
* `TypeOK`: typing invariant over the shared state.

The spec `ASSUME`s `LeaseDuration > RenewDeadline > RenewPeriod > 0`
(and positive `WatchdogMaxSilence` and `ProbeDelay`), so a misconfigured
timing set fails fast instead of being silently checked. The admission
webhook enforces `leaseDurationSeconds > renewDeadlineSeconds` and the
stricter `renewDeadlineSeconds > 1.2 * retryPeriodSeconds`.

## What is proved

TLC shows `NoTwoLeaders` holds iff

```
RenewDeadline + StepDownDelay < LeaseDuration
```

for any `DeliveryDelay`, with the watchdog on or off (see "The lease
watchdog"). Runs with `LeaseDuration = 5`, `RenewPeriod = 1`, default
watchdog constants and `MaxRounds = 15`, before promotion and yield
were modeled:

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
`RenewDeadline + StepDownDelay >= LeaseDuration`. Without the hard stop
the bound would be `RenewDeadline + StepDownDelay + DeliveryDelay <
LeaseDuration`, which rows 2 to 4 break.

Mutations, all with `RenewDeadline = 2`, `StepDownDelay = 1`:

| Change | `DeliveryDelay` | Result |
|---|---|---|
| hard stop removed | 1 | pass |
| hard stop removed | 9 | violation |
| hard stop at `age >= LeaseDuration` instead of `age + RenewPeriod >= LeaseDuration` | 1 | pass |
| same | 9 | violation |
| soft stop split into "request accepted" (never forced) and "stopped", `DeliveryDelay` bound dropped, hard stop intact | n/a | pass |
| same, and the Ticker's age bound on a decided leader also waits for the request to be accepted | n/a | violation |

The first two rows show that bound: without the hard stop, a long
`DeliveryDelay` breaks the invariant. Rows 3 and 4 are the
tie: killing at age `LeaseDuration` is the tick the candidate claims,
and only a fast soft stop hides it. The last two are `W` unbounded: the
hard stop must not wait for the lifecycle manager. A hard stop that waits
is only caught when the Ticker's age bound waits with it, as that bound
stands for the monitor's tick.

With promotion and yield, default constants unless noted:

| `StepDownDelay` | `MaxRounds` | Checked | Result | States (generated / distinct) |
|---|---|---|---|---|
| 1 | 12 | `NoTwoLeaders`, `TypeOK` | pass (default `.cfg`) | 471,579,457 / 154,022,144 |
| 1 | 11 | same | pass | 273,512,737 / 89,344,000 |
| 3 | 11 | same | violation | |
| 1 | 11 | same, `~promoReq` guard dropped from `YieldLease` | violation | |
| 1 | 11 | probe: a node other than a yielder leads | reachable | |

The guard row: a holder asks to promote, yields and stops its monitor,
the other node claims after the window and promotes, then the first
promotion completes. The probe row is what #11677 needs: after a yield
the other node leads through the observation window.

In production units the relation reads `renewDeadlineSeconds +
retryPeriodSeconds + isolationCheck.requestTimeout <
leaseDurationSeconds`, 10s + 2s + 1s < 15s at the defaults, and it is the
one the webhook warns about. The probe time is inside it because
`monitorLeaseAge` reads `age` at the start of the tick and tests the
hard stop with that same value after the probe: a probe that ends past
`leaseDurationSeconds` kills too late, and the model's `StepDownDelay`
bound on the verdict is what rules that out. `W + X` does not appear:
the hard stop fires at the last monitor tick before the lease can
expire, whether or not the soft stop was accepted.

One branch is outside this relation. If the peers first agree that the
node is the primary and a verdict only comes later, because a challenger's
cached target flips after a probe that saw it agreeing, the verdict tick
can run up to `retryPeriodSeconds + 2 * requestTimeout` after the flip,
since the previous probe may have read the challenger just before it. The
challenger starts its observation window when it becomes the target, so it
claims no earlier than `leaseDurationSeconds` after the flip. The
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
  only safety (`NoTwoLeaders`) is checked. The check, the soft stop and
  the hard stop are forced by the Ticker guard, not by fairness.
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
* A claim gives the lease, not leadership, and the promotion may never
  complete. The renewal loop and the monitor run from the claim, as in
  the code, so a holder that never promoted is fenced like a leader.
* Every node may contest the lease. The code lets only the target
  primary contest; letting all nodes contest only adds behaviours, so it
  cannot hide a violation.
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

## The lease watchdog

The liveness probe (`pkg/management/postgres/webserver/probes/liveness.go`)
catches a leader whose lease loop has wedged, something the lease-age
monitor cannot see when its own goroutine is the one that is stuck.

How it works:

* Every `Get`, `Create` and `Update` on the lease lock beats the watchdog
  (`LeaseWatchdog.WrapLock`), whether or not the call succeeds. A call
  that is still running counts as activity, however long the API server
  takes to answer.
* The probe fails once the lease was held at least once (`MarkAcquired`)
  and the silence since the last beat exceeds `maxSilence`, set to
  `renewDeadline + 2 * retryPeriod` when the lease is acquired.
* A failing probe also calls `TryRequestImmediateShutdown`, without a
  peer check. After `failureThreshold` failures the kubelet kills the
  container.
* The probe also fails when PostgreSQL did not stop within
  `immediateShutdownTimeout` (30s by default) after an immediate
  shutdown. The lifecycle manager then exits on `SIGTERM` without a new
  shutdown attempt.

In the model, `LeaseLoopStuck` is the loop that stops attempting. Once
`tick - stuckAt > WatchdogMaxSilence` the soft stop is enabled for that
leader (`DeliverShutdown`), and `KubeletKill` is forced by `stuckAt +
WatchdogMaxSilence + ProbeDelay`. The default `.cfg` keeps the watchdog
slower than the lease (`2 + 6 > 5`), the production shape. A loop that is
alive but cannot renew keeps beating and is never flagged: that is the
"renewal is never forced" behaviour of `HolderRenewLease`.

Runs, `LeaseDuration = 5`, `RenewPeriod = 1`, `DeliveryDelay = 1`,
`RenewDeadline = 2`, `MaxRounds = 15`, before promotion and yield were
modeled:

| `MonitorWorks` | `StepDownDelay` | `WatchdogMaxSilence` | `ProbeDelay` | Result | States (generated / distinct) |
|---|---|---|---|---|---|
| TRUE | 1 | 2 | 6 | pass (default `.cfg` then) | 260,984,209 / 86,310,432 |
| TRUE | 2 | 2 | 1 | pass | 358,556,113 / 118,389,024 |
| TRUE | 3 | 2 | 1 | violation | |
| TRUE | 2 | 14 | 1 | pass (watchdog off) | 287,821,585 / 95,097,600 |
| TRUE | 3 | 14 | 1 | violation (watchdog off) | |
| FALSE | 1 | 2 | 1 | violation | |

Rows 2 to 5: the relation `RenewDeadline + StepDownDelay < LeaseDuration`
is the same with the watchdog on or off, even with a watchdog faster than
the lease (`2 + 1 < 5`). The watchdog only stops nodes earlier. It does
not hide a late verdict, because the leader that is late is alive and
keeps beating.

Row 6: with the monitor off, no watchdog setting makes the model safe.
The counterexample is a leader whose loop is alive but never lands a
write: it beats forever and nothing stops it. To isolate what the
watchdog alone covers, a mutation forces an alive loop to renew every
`RenewPeriod` (Ticker guard) and lets the loop get stuck only in the tick
of a confirmed write (`stuckAt = lastRenew`), with `MonitorWorks =
FALSE`:

| `WatchdogMaxSilence` | `ProbeDelay` | Sum | Result |
|---|---|---|---|
| 2 | 2 | 4 | pass |
| 1 | 3 | 4 | pass |
| 2 | 3 | 5 | violation |
| 3 | 2 | 5 | violation |

So under those two assumptions `NoTwoLeaders` holds iff

```
WatchdogMaxSilence + ProbeDelay < LeaseDuration
```

The kill lands at the latest at `stuckAt + WatchdogMaxSilence +
ProbeDelay` and the candidate claims at `lastRenew + LeaseDuration`; they
meet when the sum equals `LeaseDuration`. If the loop gets stuck later
than its last confirmed write, the kill moves later by the same amount
and no constant relation holds: the watchdog measures silence from the
last attempt, not the lease age. At the production defaults the relation
reads 14s + 30s < 15s, which is false; even the first failing probe, at
the latest 24s after the last beat, is past `leaseDurationSeconds`. A
stuck monitor is therefore not covered at the defaults. The watchdog is
an availability backstop: it restarts a wedged primary, it does not set
the fencing bound.

Stuck loop with peers agreeing (single node, `Nodes = {"one"}`, the
step-down verdict restricted to a disagreeing peer, as `shouldStepDown`
keeps leading without probing in a one-instance cluster):

| Watchdog | `NoVerdict` (`stepDownAt = -1` always) | `StaysLeading` (a node that holds the lease keeps leading) |
|---|---|---|
| on (2, 6) | holds | violation |
| off (14, 1) | holds | holds |

The monitor never decides, and with the watchdog off the stuck leader
leads until `MaxRounds`. With it on, the soft stop asked by the probe
handler, or the kill, is the only path that stops the node.

Assumptions and limits of this part:

* The kill is instant and lands at the latest tick. In the code the
  kubelet sends `SIGTERM`; the lifecycle manager then runs a smart/fast
  shutdown bounded by the Pod's termination grace period, unless
  PostgreSQL was already found unresponsive to an immediate shutdown.
  That delay is not in `ProbeDelay`.
* The probe's timeout and the phase of its period are inside `ProbeDelay`
  only as the worst case of one period.
* A lock call that never returns counts as activity and never trips the
  watchdog. The model has no such call: a loop that stops attempting is
  the only stuck loop.
* The unresponsive-PostgreSQL branch is not modeled: the soft stop stops
  the node at once, so a postmaster that ignores `SIGQUIT` after the hard
  stop has no counterpart.
* A stuck monitor with a live loop is unmodeled beyond `MonitorWorks =
  FALSE`, and nothing in the code catches it.
