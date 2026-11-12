---- MODULE PrimaryLease ----
\* Primary lease with the lease-age step-down monitor
\* (monitorLeaseAge in internal/cmd/manager/instance/run/lease/stepdown.go).
\*
\* The invariant NoTwoLeaders counts every node, dead or alive. Fencing is a
\* promise about time: a primary whose last confirmed lease write is
\* older than RenewDeadline must run its peer check within StepDownDelay
\* ticks, and a decided primary must be stopped before its lease age reaches
\* LeaseDuration (the hard stop, killPostgresAndExit) or within DeliveryDelay
\* ticks of the verdict (the soft stop, TryRequestStepDownShutdown),
\* whichever comes first. The Ticker refuses to advance past a missed
\* deadline, so the model proves: IF the code keeps those bounds, THEN no two
\* nodes lead at once. It does not prove that the code keeps them.
\*
\* TLC shows NoTwoLeaders holds iff
\*     RenewDeadline + StepDownDelay < LeaseDuration
\* for any DeliveryDelay (see README.md for the runs). A write is atomic
\* here, so lastRenew is both the tick the write started and the tick the
\* candidates can see it: the code stamps the start of the write on a
\* monotonic clock for the same reason. In production units StepDownDelay is
\* retryPeriodSeconds plus isolationCheck.requestTimeout, and DeliveryDelay
\* is the wait for the lifecycle manager to accept the shutdown request plus
\* the time pg_ctl takes to deliver it; the code bounds neither part of
\* DeliveryDelay, which is why the hard stop exists.
\*
\* The lease watchdog (internal/management/watchdog) is modeled too. Every
\* lock attempt of the lease loop, landed or not, beats it, so a loop that
\* is alive but cannot renew never trips it. LeaseLoopStuck is the fault it
\* catches: the loop stops attempting (stuckAt). Once a leader has been
\* silent for more than WatchdogMaxSilence ticks the liveness probe fails:
\* the probe handler asks for the soft stop without a verdict, and the
\* kubelet kills the container at the latest ProbeDelay ticks after the
\* silence bound was crossed. MonitorWorks = FALSE switches the lease-age
\* monitor off, to check what the watchdog covers on its own. Setting
\* WatchdogMaxSilence + ProbeDelay >= MaxRounds switches the watchdog off.
\*
\* A claim gives the lease (held), not leadership: the holder asks for a
\* promotion that completes later or never. A holder that stops being the
\* target before asking yields (Runnable.Yield): it stops renewing and
\* leaves its record to go stale.

EXTENDS Integers, TLC

CONSTANTS MaxRounds, LeaseDuration, RenewDeadline, RenewPeriod, Nodes,
          StepDownDelay, DeliveryDelay, WatchdogMaxSilence, ProbeDelay,
          MonitorWorks

ASSUME /\ MaxRounds > 0
       /\ LeaseDuration > RenewDeadline
       /\ RenewDeadline > RenewPeriod
       /\ RenewPeriod > 0
       /\ StepDownDelay > 0
       /\ DeliveryDelay >= 0
       /\ WatchdogMaxSilence > 0
       /\ ProbeDelay > 0
       /\ MonitorWorks \in BOOLEAN

(* --algorithm PrimaryLease {

variables
  leaseHolder = "",
  leaseRenewTick = 0,

  \* n holds the lease: its renewal loop and its monitor run
  held = [n \in Nodes |-> FALSE],
  \* n's PostgreSQL is writable
  leading = [n \in Nodes |-> FALSE],
  \* n asked PostgreSQL to promote (promotionRequested)
  promoReq = [n \in Nodes |-> FALSE],
  \* n stopped renewing a record that still names it
  yielded = [n \in Nodes |-> FALSE],
  lookingForLease = [n \in Nodes |-> FALSE],

  \* tick of the last successful write naming n as holder (confirmingLock)
  lastRenew = [n \in Nodes |-> -1],
  \* tick of the last peer check run by n's monitor while overdue
  lastCheck = [n \in Nodes |-> -1],
  \* tick at which n's monitor decided to step down, -1 if undecided
  stepDownAt = [n \in Nodes |-> -1],
  \* tick of the last lock attempt of n's lease loop once it got stuck, -1
  \* while the loop is alive (and beats the watchdog on every attempt)
  stuckAt = [n \in Nodes |-> -1],

  \* ASSUMPTION: no time rate skew
  tick = 0;

define {
  Max(a, b) == IF a >= b THEN a ELSE b

  \* The monitor trips when the last confirmed write is older than
  \* RenewDeadline (stepdown.go: age <= RenewDeadline => continue).
  Overdue(n) == held[n] /\ tick - lastRenew[n] > RenewDeadline

  \* Latest tick by which an overdue, undecided leader must have run a
  \* peer check: StepDownDelay after it tripped, or after its last check.
  CheckDue(n) == Max(lastRenew[n] + RenewDeadline, lastCheck[n]) + StepDownDelay

  \* The liveness probe fails for a leader whose lease loop has not attempted
  \* a lock operation for more than WatchdogMaxSilence (IsHealthy: silence >
  \* maxSilence, only after the lease was held, which leading implies).
  Silent(n) == held[n] /\ stuckAt[n] /= -1 /\ tick - stuckAt[n] > WatchdogMaxSilence

  \* Latest tick by which the kubelet has killed a stuck leader.
  KillDue(n) == stuckAt[n] + WatchdogMaxSilence + ProbeDelay

  \* Time may reach t only if no fencing step is overdue at t. A decided
  \* leader is stopped within DeliveryDelay of the verdict, and in any case
  \* before its lease age reaches LeaseDuration: the hard stop runs at the
  \* last monitor tick whose age is below it. A stuck leader is killed by
  \* KillDue.
  FencingOnTime(t) == \A n \in Nodes :
    /\ (MonitorWorks /\ held[n] /\ t - lastRenew[n] > RenewDeadline /\ stepDownAt[n] = -1)
         => t <= CheckDue(n)
    /\ stepDownAt[n] /= -1 => t <= stepDownAt[n] + DeliveryDelay
    /\ stepDownAt[n] /= -1 => t - lastRenew[n] < LeaseDuration
    /\ (held[n] /\ stuckAt[n] /= -1) => t <= KillDue(n)

  \* A peer names a target primary other than n when the operator told it
  \* to take over (it is inside its observation window) or it already holds
  \* the lease. The /failsafe answer is abstracted to this.
  Disagrees(m, n) == m /= n /\ (lookingForLease[m] \/ leaseHolder = m)

  NoTwoLeaders == \A a, b \in Nodes : leading[a] /\ leading[b] => a = b

  TypeOK ==
    /\ leaseHolder \in Nodes \cup {""}
    /\ leaseRenewTick \in 0 .. MaxRounds
    /\ held \in [Nodes -> BOOLEAN]
    /\ leading \in [Nodes -> BOOLEAN]
    /\ promoReq \in [Nodes -> BOOLEAN]
    /\ yielded \in [Nodes -> BOOLEAN]
    /\ \A n \in Nodes : leading[n] => promoReq[n]
    /\ lookingForLease \in [Nodes -> BOOLEAN]
    /\ lastRenew \in [Nodes -> -1 .. MaxRounds]
    /\ lastCheck \in [Nodes -> -1 .. MaxRounds]
    /\ stepDownAt \in [Nodes -> -1 .. MaxRounds]
    /\ stuckAt \in [Nodes -> -1 .. MaxRounds]
    /\ tick \in 0 .. MaxRounds
}

process (Ticker = "tick") {
    T: while (TRUE) {
      if (tick < MaxRounds /\ FencingOnTime(tick + 1)) {
        tick := tick + 1;
      }
    }
}

process (Instance \in Nodes)
variable observedLeaseRenewTick = 0,
  observedLeaseHolder = "",
  lastLeaseChange = -1,
  lastLeaseRenewTick = -1;
{
  InstanceLoop: while (TRUE) {
    either {
      InstanceObserveLease:
        await /\ tick < MaxRounds
              /\ (observedLeaseHolder /= leaseHolder \/ observedLeaseRenewTick /= leaseRenewTick);

        if (lastLeaseChange /= -1 /\ observedLeaseRenewTick /= leaseRenewTick) {
          \* The record changed inside the window: tryTakeOver restarts it.
          lastLeaseChange := -1;
          lastLeaseRenewTick := -1;
          lookingForLease[self] := FALSE;
        };

        observedLeaseHolder := leaseHolder;
        observedLeaseRenewTick := leaseRenewTick;
    } or {
      HolderRenewLease:
        \* Renewal is never forced: a wedged renewal loop is one of the
        \* explored behaviours, and a renewal may also land late (after
        \* RenewDeadline), as preAcquire re-adopts an "already ours" lease.
        await /\ tick < MaxRounds
              /\ held[self]
              /\ stuckAt[self] = -1
              /\ RenewPeriod <= tick - lastRenew[self]
              /\ observedLeaseHolder = self;

        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          lastRenew[self] := tick;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;
          \* A renewal withdraws a verdict still waiting for delivery: the
          \* monitor sees a fresh lease and drops it.
          stepDownAt[self] := -1;
        };
    } or {
      LeaseLoopStuck:
        \* The lease loop stops attempting lock operations (a deadlock). The
        \* last attempt is at this tick. A loop that is alive but cannot
        \* renew is not this fault: it still beats the watchdog on every
        \* attempt and is covered by "renewal is never forced" above.
        await /\ tick < MaxRounds
              /\ held[self]
              /\ stuckAt[self] = -1;

        stuckAt[self] := tick;
    } or {
      MonitorDecidesStepDown:
        \* A peer is unreachable, or a reachable peer names another target
        \* primary. Unreachability needs no precondition: the network may
        \* drop any probe.
        await /\ tick < MaxRounds
              /\ MonitorWorks
              /\ Overdue(self)
              /\ stepDownAt[self] = -1;

        lastCheck[self] := tick;
        stepDownAt[self] := tick;
    } or {
      MonitorKeepsLeading:
        \* Every peer is reachable and names us: keep leading, check again.
        await /\ tick < MaxRounds
              /\ MonitorWorks
              /\ Overdue(self)
              /\ stepDownAt[self] = -1
              /\ \A m \in Nodes : ~Disagrees(m, self);

        lastCheck[self] := tick;
    } or {
      DeliverShutdown:
        \* The shutdown request is accepted and PostgreSQL stops. The
        \* lease is not released: this is the non-cooperative old primary.
        \* The request comes from the monitor's verdict, or from the liveness
        \* probe handler of a stuck leader (liveness.go), which asks without
        \* any peer check.
        await /\ tick < MaxRounds
              /\ (stepDownAt[self] /= -1 \/ Silent(self));

        held[self] := FALSE;
        leading[self] := FALSE;
        promoReq[self] := FALSE;
        stepDownAt[self] := -1;
        stuckAt[self] := -1;
    } or {
      KubeletKill:
        \* The liveness probe failed failureThreshold times in a row: the
        \* kubelet kills the container. The kill is instant here and lands
        \* at the latest tick the Ticker allows; the soft stop above is the
        \* early path.
        await /\ tick < MaxRounds
              /\ held[self]
              /\ stuckAt[self] /= -1
              /\ tick >= KillDue(self);

        held[self] := FALSE;
        leading[self] := FALSE;
        promoReq[self] := FALSE;
        stepDownAt[self] := -1;
        stuckAt[self] := -1;
    } or {
      MonitorHardStop:
        \* The next tick would be too late (age + RetryPeriod >= LeaseDuration
        \* in stepdown.go): kill the postmaster and exit. The kill is instant
        \* here. The monitor ticks every RenewPeriod in the code but may act at
        \* every tick here, so the Ticker only forces this before the age
        \* reaches LeaseDuration, the latest tick the code can pick.
        await /\ tick < MaxRounds
              /\ stepDownAt[self] /= -1
              /\ tick - lastRenew[self] + RenewPeriod >= LeaseDuration;

        held[self] := FALSE;
        leading[self] := FALSE;
        promoReq[self] := FALSE;
        stepDownAt[self] := -1;
        stuckAt[self] := -1;
    } or {
      CandidatePrimaryClaimEmptyLease:
        await /\ tick < MaxRounds
              /\ observedLeaseHolder = "";

        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          lastRenew[self] := tick;
          stuckAt[self] := -1;
          leaseHolder := self;
          observedLeaseHolder := self;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;

          held[self] := TRUE;
        };
    } or {
      CandidatePrimaryStartClaimOwnedLease:
        await /\ tick < MaxRounds
              /\ observedLeaseHolder /= self
              /\ observedLeaseHolder /= ""
              /\ lastLeaseChange = -1;

        lookingForLease[self] := TRUE;
        lastLeaseChange := tick;
        lastLeaseRenewTick := observedLeaseRenewTick;
    } or {
      CandidatePrimaryClaimOwnedLease:
        await /\ tick < MaxRounds
              /\ observedLeaseHolder /= self
              /\ observedLeaseHolder /= ""
              /\ lastLeaseChange /= -1
              /\ lastLeaseRenewTick = observedLeaseRenewTick
              /\ (tick - lastLeaseChange) >= LeaseDuration;

        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          lastRenew[self] := tick;
          stuckAt[self] := -1;
          leaseHolder := self;
          observedLeaseHolder := self;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;

          lastLeaseChange := -1;
          lastLeaseRenewTick := -1;
          lookingForLease[self] := FALSE;
          held[self] := TRUE;
        };
    } or {
      StartPromotion:
        \* The target asks PostgreSQL to promote. Any holder may be the
        \* target: the operator is not modeled.
        await /\ tick < MaxRounds
              /\ held[self]
              /\ ~promoReq[self];

        promoReq[self] := TRUE;
    } or {
      PromoteComplete:
        \* Never forced: a promotion takes any time, or never completes.
        await /\ tick < MaxRounds
              /\ promoReq[self]
              /\ ~leading[self];

        leading[self] := TRUE;
    } or {
      YieldLease:
        \* No longer the target and promotion never asked: stop the
        \* renewal loop and the monitor, write nothing. A promotion once
        \* asked may still complete, so that node keeps its monitor.
        await /\ tick < MaxRounds
              /\ held[self]
              /\ ~promoReq[self];

        held[self] := FALSE;
        yielded[self] := TRUE;
        stepDownAt[self] := -1;
        stuckAt[self] := -1;
    } or {
      ReacquireOwn:
        \* Target again: refresh the record it still owns, with no window
        \* (the "already ours" CAS of tryTakeOver).
        await /\ tick < MaxRounds
              /\ yielded[self]
              /\ observedLeaseHolder = self;

        yielded[self] := FALSE;
        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          held[self] := TRUE;
          lastRenew[self] := tick;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;
        };
    } or {
      \* CapStutter: the simulation finished
      await tick = MaxRounds;
      skip;
    }
  };
}

} *)
\* BEGIN TRANSLATION (chksum(pcal) = "a7d7b81f" /\ chksum(tla) = "c999227c")
VARIABLES leaseHolder, leaseRenewTick, held, leading, promoReq, yielded, 
          lookingForLease, lastRenew, lastCheck, stepDownAt, stuckAt, tick, 
          pc

(* define statement *)
Max(a, b) == IF a >= b THEN a ELSE b



Overdue(n) == held[n] /\ tick - lastRenew[n] > RenewDeadline



CheckDue(n) == Max(lastRenew[n] + RenewDeadline, lastCheck[n]) + StepDownDelay




Silent(n) == held[n] /\ stuckAt[n] /= -1 /\ tick - stuckAt[n] > WatchdogMaxSilence


KillDue(n) == stuckAt[n] + WatchdogMaxSilence + ProbeDelay






FencingOnTime(t) == \A n \in Nodes :
  /\ (MonitorWorks /\ held[n] /\ t - lastRenew[n] > RenewDeadline /\ stepDownAt[n] = -1)
       => t <= CheckDue(n)
  /\ stepDownAt[n] /= -1 => t <= stepDownAt[n] + DeliveryDelay
  /\ stepDownAt[n] /= -1 => t - lastRenew[n] < LeaseDuration
  /\ (held[n] /\ stuckAt[n] /= -1) => t <= KillDue(n)




Disagrees(m, n) == m /= n /\ (lookingForLease[m] \/ leaseHolder = m)

NoTwoLeaders == \A a, b \in Nodes : leading[a] /\ leading[b] => a = b

TypeOK ==
  /\ leaseHolder \in Nodes \cup {""}
  /\ leaseRenewTick \in 0 .. MaxRounds
  /\ held \in [Nodes -> BOOLEAN]
  /\ leading \in [Nodes -> BOOLEAN]
  /\ promoReq \in [Nodes -> BOOLEAN]
  /\ yielded \in [Nodes -> BOOLEAN]
  /\ \A n \in Nodes : leading[n] => promoReq[n]
  /\ lookingForLease \in [Nodes -> BOOLEAN]
  /\ lastRenew \in [Nodes -> -1 .. MaxRounds]
  /\ lastCheck \in [Nodes -> -1 .. MaxRounds]
  /\ stepDownAt \in [Nodes -> -1 .. MaxRounds]
  /\ stuckAt \in [Nodes -> -1 .. MaxRounds]
  /\ tick \in 0 .. MaxRounds

VARIABLES observedLeaseRenewTick, observedLeaseHolder, lastLeaseChange, 
          lastLeaseRenewTick

vars == << leaseHolder, leaseRenewTick, held, leading, promoReq, yielded, 
           lookingForLease, lastRenew, lastCheck, stepDownAt, stuckAt, tick, 
           pc, observedLeaseRenewTick, observedLeaseHolder, lastLeaseChange, 
           lastLeaseRenewTick >>

ProcSet == {"tick"} \cup (Nodes)

Init == (* Global variables *)
        /\ leaseHolder = ""
        /\ leaseRenewTick = 0
        /\ held = [n \in Nodes |-> FALSE]
        /\ leading = [n \in Nodes |-> FALSE]
        /\ promoReq = [n \in Nodes |-> FALSE]
        /\ yielded = [n \in Nodes |-> FALSE]
        /\ lookingForLease = [n \in Nodes |-> FALSE]
        /\ lastRenew = [n \in Nodes |-> -1]
        /\ lastCheck = [n \in Nodes |-> -1]
        /\ stepDownAt = [n \in Nodes |-> -1]
        /\ stuckAt = [n \in Nodes |-> -1]
        /\ tick = 0
        (* Process Instance *)
        /\ observedLeaseRenewTick = [self \in Nodes |-> 0]
        /\ observedLeaseHolder = [self \in Nodes |-> ""]
        /\ lastLeaseChange = [self \in Nodes |-> -1]
        /\ lastLeaseRenewTick = [self \in Nodes |-> -1]
        /\ pc = [self \in ProcSet |-> CASE self = "tick" -> "T"
                                        [] self \in Nodes -> "InstanceLoop"]

T == /\ pc["tick"] = "T"
     /\ IF tick < MaxRounds /\ FencingOnTime(tick + 1)
           THEN /\ tick' = tick + 1
           ELSE /\ TRUE
                /\ tick' = tick
     /\ pc' = [pc EXCEPT !["tick"] = "T"]
     /\ UNCHANGED << leaseHolder, leaseRenewTick, held, leading, promoReq, 
                     yielded, lookingForLease, lastRenew, lastCheck, 
                     stepDownAt, stuckAt, observedLeaseRenewTick, 
                     observedLeaseHolder, lastLeaseChange, lastLeaseRenewTick >>

Ticker == T

InstanceLoop(self) == /\ pc[self] = "InstanceLoop"
                      /\ \/ /\ pc' = [pc EXCEPT ![self] = "InstanceObserveLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "HolderRenewLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "LeaseLoopStuck"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "MonitorDecidesStepDown"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "MonitorKeepsLeading"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "DeliverShutdown"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "KubeletKill"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "MonitorHardStop"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryClaimEmptyLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryStartClaimOwnedLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryClaimOwnedLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "StartPromotion"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "PromoteComplete"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "YieldLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "ReacquireOwn"]
                         \/ /\ tick = MaxRounds
                            /\ TRUE
                            /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                      /\ UNCHANGED << leaseHolder, leaseRenewTick, held, 
                                      leading, promoReq, yielded, 
                                      lookingForLease, lastRenew, lastCheck, 
                                      stepDownAt, stuckAt, tick, 
                                      observedLeaseRenewTick, 
                                      observedLeaseHolder, lastLeaseChange, 
                                      lastLeaseRenewTick >>

InstanceObserveLease(self) == /\ pc[self] = "InstanceObserveLease"
                              /\ /\ tick < MaxRounds
                                 /\ (observedLeaseHolder[self] /= leaseHolder \/ observedLeaseRenewTick[self] /= leaseRenewTick)
                              /\ IF lastLeaseChange[self] /= -1 /\ observedLeaseRenewTick[self] /= leaseRenewTick
                                    THEN /\ lastLeaseChange' = [lastLeaseChange EXCEPT ![self] = -1]
                                         /\ lastLeaseRenewTick' = [lastLeaseRenewTick EXCEPT ![self] = -1]
                                         /\ lookingForLease' = [lookingForLease EXCEPT ![self] = FALSE]
                                    ELSE /\ TRUE
                                         /\ UNCHANGED << lookingForLease, 
                                                         lastLeaseChange, 
                                                         lastLeaseRenewTick >>
                              /\ observedLeaseHolder' = [observedLeaseHolder EXCEPT ![self] = leaseHolder]
                              /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = leaseRenewTick]
                              /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                              /\ UNCHANGED << leaseHolder, leaseRenewTick, 
                                              held, leading, promoReq, yielded, 
                                              lastRenew, lastCheck, stepDownAt, 
                                              stuckAt, tick >>

HolderRenewLease(self) == /\ pc[self] = "HolderRenewLease"
                          /\ /\ tick < MaxRounds
                             /\ held[self]
                             /\ stuckAt[self] = -1
                             /\ RenewPeriod <= tick - lastRenew[self]
                             /\ observedLeaseHolder[self] = self
                          /\ IF observedLeaseRenewTick[self] = leaseRenewTick /\ observedLeaseHolder[self] = leaseHolder
                                THEN /\ lastRenew' = [lastRenew EXCEPT ![self] = tick]
                                     /\ leaseRenewTick' = tick
                                     /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                                     /\ stepDownAt' = [stepDownAt EXCEPT ![self] = -1]
                                ELSE /\ TRUE
                                     /\ UNCHANGED << leaseRenewTick, lastRenew, 
                                                     stepDownAt, 
                                                     observedLeaseRenewTick >>
                          /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                          /\ UNCHANGED << leaseHolder, held, leading, promoReq, 
                                          yielded, lookingForLease, lastCheck, 
                                          stuckAt, tick, observedLeaseHolder, 
                                          lastLeaseChange, lastLeaseRenewTick >>

LeaseLoopStuck(self) == /\ pc[self] = "LeaseLoopStuck"
                        /\ /\ tick < MaxRounds
                           /\ held[self]
                           /\ stuckAt[self] = -1
                        /\ stuckAt' = [stuckAt EXCEPT ![self] = tick]
                        /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                        /\ UNCHANGED << leaseHolder, leaseRenewTick, held, 
                                        leading, promoReq, yielded, 
                                        lookingForLease, lastRenew, lastCheck, 
                                        stepDownAt, tick, 
                                        observedLeaseRenewTick, 
                                        observedLeaseHolder, lastLeaseChange, 
                                        lastLeaseRenewTick >>

MonitorDecidesStepDown(self) == /\ pc[self] = "MonitorDecidesStepDown"
                                /\ /\ tick < MaxRounds
                                   /\ MonitorWorks
                                   /\ Overdue(self)
                                   /\ stepDownAt[self] = -1
                                /\ lastCheck' = [lastCheck EXCEPT ![self] = tick]
                                /\ stepDownAt' = [stepDownAt EXCEPT ![self] = tick]
                                /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                /\ UNCHANGED << leaseHolder, leaseRenewTick, 
                                                held, leading, promoReq, 
                                                yielded, lookingForLease, 
                                                lastRenew, stuckAt, tick, 
                                                observedLeaseRenewTick, 
                                                observedLeaseHolder, 
                                                lastLeaseChange, 
                                                lastLeaseRenewTick >>

MonitorKeepsLeading(self) == /\ pc[self] = "MonitorKeepsLeading"
                             /\ /\ tick < MaxRounds
                                /\ MonitorWorks
                                /\ Overdue(self)
                                /\ stepDownAt[self] = -1
                                /\ \A m \in Nodes : ~Disagrees(m, self)
                             /\ lastCheck' = [lastCheck EXCEPT ![self] = tick]
                             /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                             /\ UNCHANGED << leaseHolder, leaseRenewTick, held, 
                                             leading, promoReq, yielded, 
                                             lookingForLease, lastRenew, 
                                             stepDownAt, stuckAt, tick, 
                                             observedLeaseRenewTick, 
                                             observedLeaseHolder, 
                                             lastLeaseChange, 
                                             lastLeaseRenewTick >>

DeliverShutdown(self) == /\ pc[self] = "DeliverShutdown"
                         /\ /\ tick < MaxRounds
                            /\ (stepDownAt[self] /= -1 \/ Silent(self))
                         /\ held' = [held EXCEPT ![self] = FALSE]
                         /\ leading' = [leading EXCEPT ![self] = FALSE]
                         /\ promoReq' = [promoReq EXCEPT ![self] = FALSE]
                         /\ stepDownAt' = [stepDownAt EXCEPT ![self] = -1]
                         /\ stuckAt' = [stuckAt EXCEPT ![self] = -1]
                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                         /\ UNCHANGED << leaseHolder, leaseRenewTick, yielded, 
                                         lookingForLease, lastRenew, lastCheck, 
                                         tick, observedLeaseRenewTick, 
                                         observedLeaseHolder, lastLeaseChange, 
                                         lastLeaseRenewTick >>

KubeletKill(self) == /\ pc[self] = "KubeletKill"
                     /\ /\ tick < MaxRounds
                        /\ held[self]
                        /\ stuckAt[self] /= -1
                        /\ tick >= KillDue(self)
                     /\ held' = [held EXCEPT ![self] = FALSE]
                     /\ leading' = [leading EXCEPT ![self] = FALSE]
                     /\ promoReq' = [promoReq EXCEPT ![self] = FALSE]
                     /\ stepDownAt' = [stepDownAt EXCEPT ![self] = -1]
                     /\ stuckAt' = [stuckAt EXCEPT ![self] = -1]
                     /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                     /\ UNCHANGED << leaseHolder, leaseRenewTick, yielded, 
                                     lookingForLease, lastRenew, lastCheck, 
                                     tick, observedLeaseRenewTick, 
                                     observedLeaseHolder, lastLeaseChange, 
                                     lastLeaseRenewTick >>

MonitorHardStop(self) == /\ pc[self] = "MonitorHardStop"
                         /\ /\ tick < MaxRounds
                            /\ stepDownAt[self] /= -1
                            /\ tick - lastRenew[self] + RenewPeriod >= LeaseDuration
                         /\ held' = [held EXCEPT ![self] = FALSE]
                         /\ leading' = [leading EXCEPT ![self] = FALSE]
                         /\ promoReq' = [promoReq EXCEPT ![self] = FALSE]
                         /\ stepDownAt' = [stepDownAt EXCEPT ![self] = -1]
                         /\ stuckAt' = [stuckAt EXCEPT ![self] = -1]
                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                         /\ UNCHANGED << leaseHolder, leaseRenewTick, yielded, 
                                         lookingForLease, lastRenew, lastCheck, 
                                         tick, observedLeaseRenewTick, 
                                         observedLeaseHolder, lastLeaseChange, 
                                         lastLeaseRenewTick >>

CandidatePrimaryClaimEmptyLease(self) == /\ pc[self] = "CandidatePrimaryClaimEmptyLease"
                                         /\ /\ tick < MaxRounds
                                            /\ observedLeaseHolder[self] = ""
                                         /\ IF observedLeaseRenewTick[self] = leaseRenewTick /\ observedLeaseHolder[self] = leaseHolder
                                               THEN /\ lastRenew' = [lastRenew EXCEPT ![self] = tick]
                                                    /\ stuckAt' = [stuckAt EXCEPT ![self] = -1]
                                                    /\ leaseHolder' = self
                                                    /\ observedLeaseHolder' = [observedLeaseHolder EXCEPT ![self] = self]
                                                    /\ leaseRenewTick' = tick
                                                    /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                                                    /\ held' = [held EXCEPT ![self] = TRUE]
                                               ELSE /\ TRUE
                                                    /\ UNCHANGED << leaseHolder, 
                                                                    leaseRenewTick, 
                                                                    held, 
                                                                    lastRenew, 
                                                                    stuckAt, 
                                                                    observedLeaseRenewTick, 
                                                                    observedLeaseHolder >>
                                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                         /\ UNCHANGED << leading, promoReq, 
                                                         yielded, 
                                                         lookingForLease, 
                                                         lastCheck, stepDownAt, 
                                                         tick, lastLeaseChange, 
                                                         lastLeaseRenewTick >>

CandidatePrimaryStartClaimOwnedLease(self) == /\ pc[self] = "CandidatePrimaryStartClaimOwnedLease"
                                              /\ /\ tick < MaxRounds
                                                 /\ observedLeaseHolder[self] /= self
                                                 /\ observedLeaseHolder[self] /= ""
                                                 /\ lastLeaseChange[self] = -1
                                              /\ lookingForLease' = [lookingForLease EXCEPT ![self] = TRUE]
                                              /\ lastLeaseChange' = [lastLeaseChange EXCEPT ![self] = tick]
                                              /\ lastLeaseRenewTick' = [lastLeaseRenewTick EXCEPT ![self] = observedLeaseRenewTick[self]]
                                              /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                              /\ UNCHANGED << leaseHolder, 
                                                              leaseRenewTick, 
                                                              held, leading, 
                                                              promoReq, 
                                                              yielded, 
                                                              lastRenew, 
                                                              lastCheck, 
                                                              stepDownAt, 
                                                              stuckAt, tick, 
                                                              observedLeaseRenewTick, 
                                                              observedLeaseHolder >>

CandidatePrimaryClaimOwnedLease(self) == /\ pc[self] = "CandidatePrimaryClaimOwnedLease"
                                         /\ /\ tick < MaxRounds
                                            /\ observedLeaseHolder[self] /= self
                                            /\ observedLeaseHolder[self] /= ""
                                            /\ lastLeaseChange[self] /= -1
                                            /\ lastLeaseRenewTick[self] = observedLeaseRenewTick[self]
                                            /\ (tick - lastLeaseChange[self]) >= LeaseDuration
                                         /\ IF observedLeaseRenewTick[self] = leaseRenewTick /\ observedLeaseHolder[self] = leaseHolder
                                               THEN /\ lastRenew' = [lastRenew EXCEPT ![self] = tick]
                                                    /\ stuckAt' = [stuckAt EXCEPT ![self] = -1]
                                                    /\ leaseHolder' = self
                                                    /\ observedLeaseHolder' = [observedLeaseHolder EXCEPT ![self] = self]
                                                    /\ leaseRenewTick' = tick
                                                    /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                                                    /\ lastLeaseChange' = [lastLeaseChange EXCEPT ![self] = -1]
                                                    /\ lastLeaseRenewTick' = [lastLeaseRenewTick EXCEPT ![self] = -1]
                                                    /\ lookingForLease' = [lookingForLease EXCEPT ![self] = FALSE]
                                                    /\ held' = [held EXCEPT ![self] = TRUE]
                                               ELSE /\ TRUE
                                                    /\ UNCHANGED << leaseHolder, 
                                                                    leaseRenewTick, 
                                                                    held, 
                                                                    lookingForLease, 
                                                                    lastRenew, 
                                                                    stuckAt, 
                                                                    observedLeaseRenewTick, 
                                                                    observedLeaseHolder, 
                                                                    lastLeaseChange, 
                                                                    lastLeaseRenewTick >>
                                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                         /\ UNCHANGED << leading, promoReq, 
                                                         yielded, lastCheck, 
                                                         stepDownAt, tick >>

StartPromotion(self) == /\ pc[self] = "StartPromotion"
                        /\ /\ tick < MaxRounds
                           /\ held[self]
                           /\ ~promoReq[self]
                        /\ promoReq' = [promoReq EXCEPT ![self] = TRUE]
                        /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                        /\ UNCHANGED << leaseHolder, leaseRenewTick, held, 
                                        leading, yielded, lookingForLease, 
                                        lastRenew, lastCheck, stepDownAt, 
                                        stuckAt, tick, observedLeaseRenewTick, 
                                        observedLeaseHolder, lastLeaseChange, 
                                        lastLeaseRenewTick >>

PromoteComplete(self) == /\ pc[self] = "PromoteComplete"
                         /\ /\ tick < MaxRounds
                            /\ promoReq[self]
                            /\ ~leading[self]
                         /\ leading' = [leading EXCEPT ![self] = TRUE]
                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                         /\ UNCHANGED << leaseHolder, leaseRenewTick, held, 
                                         promoReq, yielded, lookingForLease, 
                                         lastRenew, lastCheck, stepDownAt, 
                                         stuckAt, tick, observedLeaseRenewTick, 
                                         observedLeaseHolder, lastLeaseChange, 
                                         lastLeaseRenewTick >>

YieldLease(self) == /\ pc[self] = "YieldLease"
                    /\ /\ tick < MaxRounds
                       /\ held[self]
                       /\ ~promoReq[self]
                    /\ held' = [held EXCEPT ![self] = FALSE]
                    /\ yielded' = [yielded EXCEPT ![self] = TRUE]
                    /\ stepDownAt' = [stepDownAt EXCEPT ![self] = -1]
                    /\ stuckAt' = [stuckAt EXCEPT ![self] = -1]
                    /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                    /\ UNCHANGED << leaseHolder, leaseRenewTick, leading, 
                                    promoReq, lookingForLease, lastRenew, 
                                    lastCheck, tick, observedLeaseRenewTick, 
                                    observedLeaseHolder, lastLeaseChange, 
                                    lastLeaseRenewTick >>

ReacquireOwn(self) == /\ pc[self] = "ReacquireOwn"
                      /\ /\ tick < MaxRounds
                         /\ yielded[self]
                         /\ observedLeaseHolder[self] = self
                      /\ yielded' = [yielded EXCEPT ![self] = FALSE]
                      /\ IF observedLeaseRenewTick[self] = leaseRenewTick /\ observedLeaseHolder[self] = leaseHolder
                            THEN /\ held' = [held EXCEPT ![self] = TRUE]
                                 /\ lastRenew' = [lastRenew EXCEPT ![self] = tick]
                                 /\ leaseRenewTick' = tick
                                 /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                            ELSE /\ TRUE
                                 /\ UNCHANGED << leaseRenewTick, held, 
                                                 lastRenew, 
                                                 observedLeaseRenewTick >>
                      /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                      /\ UNCHANGED << leaseHolder, leading, promoReq, 
                                      lookingForLease, lastCheck, stepDownAt, 
                                      stuckAt, tick, observedLeaseHolder, 
                                      lastLeaseChange, lastLeaseRenewTick >>

Instance(self) == InstanceLoop(self) \/ InstanceObserveLease(self)
                     \/ HolderRenewLease(self) \/ LeaseLoopStuck(self)
                     \/ MonitorDecidesStepDown(self)
                     \/ MonitorKeepsLeading(self) \/ DeliverShutdown(self)
                     \/ KubeletKill(self) \/ MonitorHardStop(self)
                     \/ CandidatePrimaryClaimEmptyLease(self)
                     \/ CandidatePrimaryStartClaimOwnedLease(self)
                     \/ CandidatePrimaryClaimOwnedLease(self)
                     \/ StartPromotion(self) \/ PromoteComplete(self)
                     \/ YieldLease(self) \/ ReacquireOwn(self)

Next == Ticker
           \/ (\E self \in Nodes: Instance(self))

Spec == Init /\ [][Next]_vars

\* END TRANSLATION 
====
