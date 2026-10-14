---- MODULE PrimaryLease ----
\* Primary lease with the lease-age step-down monitor
\* (monitorLeaseAge in internal/cmd/manager/instance/run/lease/stepdown.go).
\*
\* The invariant NoTwoLeaders counts every node, dead or alive. Fencing is a
\* promise about time: a primary whose last confirmed lease write is
\* older than RenewDeadline must run its peer check within StepDownDelay
\* ticks, and a decided primary must be stopped before its lease age reaches
\* LeaseDuration (the hard stop, killPostgresAndExit) or within DeliveryDelay
\* ticks of the verdict (the soft stop, TryRequestImmediateShutdown),
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

EXTENDS Integers, TLC

CONSTANTS MaxRounds, LeaseDuration, RenewDeadline, RenewPeriod, Nodes,
          StepDownDelay, DeliveryDelay

ASSUME /\ MaxRounds > 0
       /\ LeaseDuration > RenewDeadline
       /\ RenewDeadline > RenewPeriod
       /\ RenewPeriod > 0
       /\ StepDownDelay > 0
       /\ DeliveryDelay >= 0

(* --algorithm PrimaryLease {

variables
  leaseHolder = "",
  leaseRenewTick = 0,

  leading = [n \in Nodes |-> FALSE],
  lookingForLease = [n \in Nodes |-> FALSE],

  \* tick of the last successful write naming n as holder (confirmingLock)
  lastRenew = [n \in Nodes |-> -1],
  \* tick of the last peer check run by n's monitor while overdue
  lastCheck = [n \in Nodes |-> -1],
  \* tick at which n's monitor decided to step down, -1 if undecided
  stepDownAt = [n \in Nodes |-> -1],

  \* ASSUMPTION: no time rate skew
  tick = 0;

define {
  Max(a, b) == IF a >= b THEN a ELSE b

  \* The monitor trips when the last confirmed write is older than
  \* RenewDeadline (stepdown.go: age <= RenewDeadline => continue).
  Overdue(n) == leading[n] /\ tick - lastRenew[n] > RenewDeadline

  \* Latest tick by which an overdue, undecided leader must have run a
  \* peer check: StepDownDelay after it tripped, or after its last check.
  CheckDue(n) == Max(lastRenew[n] + RenewDeadline, lastCheck[n]) + StepDownDelay

  \* Time may reach t only if no fencing step is overdue at t. A decided
  \* leader is stopped within DeliveryDelay of the verdict, and in any case
  \* before its lease age reaches LeaseDuration: the hard stop runs at the
  \* last monitor tick whose age is below it.
  FencingOnTime(t) == \A n \in Nodes :
    /\ (leading[n] /\ t - lastRenew[n] > RenewDeadline /\ stepDownAt[n] = -1)
         => t <= CheckDue(n)
    /\ stepDownAt[n] /= -1 => t <= stepDownAt[n] + DeliveryDelay
    /\ stepDownAt[n] /= -1 => t - lastRenew[n] < LeaseDuration

  \* A peer names a target primary other than n when the operator told it
  \* to take over (it is inside its observation window) or it already holds
  \* the lease. The /failsafe answer is abstracted to this.
  Disagrees(m, n) == m /= n /\ (lookingForLease[m] \/ leaseHolder = m)

  NoTwoLeaders == \A a, b \in Nodes : leading[a] /\ leading[b] => a = b

  TypeOK ==
    /\ leaseHolder \in Nodes \cup {""}
    /\ leaseRenewTick \in 0 .. MaxRounds
    /\ leading \in [Nodes -> BOOLEAN]
    /\ lookingForLease \in [Nodes -> BOOLEAN]
    /\ lastRenew \in [Nodes -> -1 .. MaxRounds]
    /\ lastCheck \in [Nodes -> -1 .. MaxRounds]
    /\ stepDownAt \in [Nodes -> -1 .. MaxRounds]
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
              /\ leading[self]
              /\ RenewPeriod <= tick - lastRenew[self]
              /\ observedLeaseHolder = self;

        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          lastRenew[self] := tick;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;
        };
    } or {
      MonitorDecidesStepDown:
        \* A peer is unreachable, or a reachable peer names another target
        \* primary. Unreachability needs no precondition: the network may
        \* drop any probe.
        await /\ tick < MaxRounds
              /\ Overdue(self)
              /\ stepDownAt[self] = -1;

        lastCheck[self] := tick;
        stepDownAt[self] := tick;
    } or {
      MonitorKeepsLeading:
        \* Every peer is reachable and names us: keep leading, check again.
        await /\ tick < MaxRounds
              /\ Overdue(self)
              /\ stepDownAt[self] = -1
              /\ \A m \in Nodes : ~Disagrees(m, self);

        lastCheck[self] := tick;
    } or {
      DeliverShutdown:
        \* TryRequestImmediateShutdown is accepted and PostgreSQL stops. The
        \* lease is not released: this is the non-cooperative old primary.
        await /\ tick < MaxRounds
              /\ stepDownAt[self] /= -1;

        leading[self] := FALSE;
        stepDownAt[self] := -1;
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

        leading[self] := FALSE;
        stepDownAt[self] := -1;
    } or {
      CandidatePrimaryClaimEmptyLease:
        await /\ tick < MaxRounds
              /\ observedLeaseHolder = "";

        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          lastRenew[self] := tick;
          leaseHolder := self;
          observedLeaseHolder := self;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;

          leading[self] := TRUE;
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
          leaseHolder := self;
          observedLeaseHolder := self;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;

          lastLeaseChange := -1;
          lastLeaseRenewTick := -1;
          lookingForLease[self] := FALSE;
          leading[self] := TRUE;
        };
    } or {
      \* CapStutter: the simulation finished
      await tick = MaxRounds;
      skip;
    }
  };
}

} *)
\* BEGIN TRANSLATION (chksum(pcal) = "567ba9ab" /\ chksum(tla) = "1b2a743b")
VARIABLES leaseHolder, leaseRenewTick, leading, lookingForLease, lastRenew, 
          lastCheck, stepDownAt, tick, pc

(* define statement *)
Max(a, b) == IF a >= b THEN a ELSE b



Overdue(n) == leading[n] /\ tick - lastRenew[n] > RenewDeadline



CheckDue(n) == Max(lastRenew[n] + RenewDeadline, lastCheck[n]) + StepDownDelay





FencingOnTime(t) == \A n \in Nodes :
  /\ (leading[n] /\ t - lastRenew[n] > RenewDeadline /\ stepDownAt[n] = -1)
       => t <= CheckDue(n)
  /\ stepDownAt[n] /= -1 => t <= stepDownAt[n] + DeliveryDelay
  /\ stepDownAt[n] /= -1 => t - lastRenew[n] < LeaseDuration




Disagrees(m, n) == m /= n /\ (lookingForLease[m] \/ leaseHolder = m)

NoTwoLeaders == \A a, b \in Nodes : leading[a] /\ leading[b] => a = b

TypeOK ==
  /\ leaseHolder \in Nodes \cup {""}
  /\ leaseRenewTick \in 0 .. MaxRounds
  /\ leading \in [Nodes -> BOOLEAN]
  /\ lookingForLease \in [Nodes -> BOOLEAN]
  /\ lastRenew \in [Nodes -> -1 .. MaxRounds]
  /\ lastCheck \in [Nodes -> -1 .. MaxRounds]
  /\ stepDownAt \in [Nodes -> -1 .. MaxRounds]
  /\ tick \in 0 .. MaxRounds

VARIABLES observedLeaseRenewTick, observedLeaseHolder, lastLeaseChange, 
          lastLeaseRenewTick

vars == << leaseHolder, leaseRenewTick, leading, lookingForLease, lastRenew, 
           lastCheck, stepDownAt, tick, pc, observedLeaseRenewTick, 
           observedLeaseHolder, lastLeaseChange, lastLeaseRenewTick >>

ProcSet == {"tick"} \cup (Nodes)

Init == (* Global variables *)
        /\ leaseHolder = ""
        /\ leaseRenewTick = 0
        /\ leading = [n \in Nodes |-> FALSE]
        /\ lookingForLease = [n \in Nodes |-> FALSE]
        /\ lastRenew = [n \in Nodes |-> -1]
        /\ lastCheck = [n \in Nodes |-> -1]
        /\ stepDownAt = [n \in Nodes |-> -1]
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
     /\ UNCHANGED << leaseHolder, leaseRenewTick, leading, lookingForLease, 
                     lastRenew, lastCheck, stepDownAt, observedLeaseRenewTick, 
                     observedLeaseHolder, lastLeaseChange, lastLeaseRenewTick >>

Ticker == T

InstanceLoop(self) == /\ pc[self] = "InstanceLoop"
                      /\ \/ /\ pc' = [pc EXCEPT ![self] = "InstanceObserveLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "HolderRenewLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "MonitorDecidesStepDown"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "MonitorKeepsLeading"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "DeliverShutdown"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "MonitorHardStop"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryClaimEmptyLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryStartClaimOwnedLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryClaimOwnedLease"]
                         \/ /\ tick = MaxRounds
                            /\ TRUE
                            /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                      /\ UNCHANGED << leaseHolder, leaseRenewTick, leading, 
                                      lookingForLease, lastRenew, lastCheck, 
                                      stepDownAt, tick, observedLeaseRenewTick, 
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
                                              leading, lastRenew, lastCheck, 
                                              stepDownAt, tick >>

HolderRenewLease(self) == /\ pc[self] = "HolderRenewLease"
                          /\ /\ tick < MaxRounds
                             /\ leading[self]
                             /\ RenewPeriod <= tick - lastRenew[self]
                             /\ observedLeaseHolder[self] = self
                          /\ IF observedLeaseRenewTick[self] = leaseRenewTick /\ observedLeaseHolder[self] = leaseHolder
                                THEN /\ lastRenew' = [lastRenew EXCEPT ![self] = tick]
                                     /\ leaseRenewTick' = tick
                                     /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                                ELSE /\ TRUE
                                     /\ UNCHANGED << leaseRenewTick, lastRenew, 
                                                     observedLeaseRenewTick >>
                          /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                          /\ UNCHANGED << leaseHolder, leading, 
                                          lookingForLease, lastCheck, 
                                          stepDownAt, tick, 
                                          observedLeaseHolder, lastLeaseChange, 
                                          lastLeaseRenewTick >>

MonitorDecidesStepDown(self) == /\ pc[self] = "MonitorDecidesStepDown"
                                /\ /\ tick < MaxRounds
                                   /\ Overdue(self)
                                   /\ stepDownAt[self] = -1
                                /\ lastCheck' = [lastCheck EXCEPT ![self] = tick]
                                /\ stepDownAt' = [stepDownAt EXCEPT ![self] = tick]
                                /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                /\ UNCHANGED << leaseHolder, leaseRenewTick, 
                                                leading, lookingForLease, 
                                                lastRenew, tick, 
                                                observedLeaseRenewTick, 
                                                observedLeaseHolder, 
                                                lastLeaseChange, 
                                                lastLeaseRenewTick >>

MonitorKeepsLeading(self) == /\ pc[self] = "MonitorKeepsLeading"
                             /\ /\ tick < MaxRounds
                                /\ Overdue(self)
                                /\ stepDownAt[self] = -1
                                /\ \A m \in Nodes : ~Disagrees(m, self)
                             /\ lastCheck' = [lastCheck EXCEPT ![self] = tick]
                             /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                             /\ UNCHANGED << leaseHolder, leaseRenewTick, 
                                             leading, lookingForLease, 
                                             lastRenew, stepDownAt, tick, 
                                             observedLeaseRenewTick, 
                                             observedLeaseHolder, 
                                             lastLeaseChange, 
                                             lastLeaseRenewTick >>

DeliverShutdown(self) == /\ pc[self] = "DeliverShutdown"
                         /\ /\ tick < MaxRounds
                            /\ stepDownAt[self] /= -1
                         /\ leading' = [leading EXCEPT ![self] = FALSE]
                         /\ stepDownAt' = [stepDownAt EXCEPT ![self] = -1]
                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                         /\ UNCHANGED << leaseHolder, leaseRenewTick, 
                                         lookingForLease, lastRenew, lastCheck, 
                                         tick, observedLeaseRenewTick, 
                                         observedLeaseHolder, lastLeaseChange, 
                                         lastLeaseRenewTick >>

MonitorHardStop(self) == /\ pc[self] = "MonitorHardStop"
                         /\ /\ tick < MaxRounds
                            /\ stepDownAt[self] /= -1
                            /\ tick - lastRenew[self] + RenewPeriod >= LeaseDuration
                         /\ leading' = [leading EXCEPT ![self] = FALSE]
                         /\ stepDownAt' = [stepDownAt EXCEPT ![self] = -1]
                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                         /\ UNCHANGED << leaseHolder, leaseRenewTick, 
                                         lookingForLease, lastRenew, lastCheck, 
                                         tick, observedLeaseRenewTick, 
                                         observedLeaseHolder, lastLeaseChange, 
                                         lastLeaseRenewTick >>

CandidatePrimaryClaimEmptyLease(self) == /\ pc[self] = "CandidatePrimaryClaimEmptyLease"
                                         /\ /\ tick < MaxRounds
                                            /\ observedLeaseHolder[self] = ""
                                         /\ IF observedLeaseRenewTick[self] = leaseRenewTick /\ observedLeaseHolder[self] = leaseHolder
                                               THEN /\ lastRenew' = [lastRenew EXCEPT ![self] = tick]
                                                    /\ leaseHolder' = self
                                                    /\ observedLeaseHolder' = [observedLeaseHolder EXCEPT ![self] = self]
                                                    /\ leaseRenewTick' = tick
                                                    /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                                                    /\ leading' = [leading EXCEPT ![self] = TRUE]
                                               ELSE /\ TRUE
                                                    /\ UNCHANGED << leaseHolder, 
                                                                    leaseRenewTick, 
                                                                    leading, 
                                                                    lastRenew, 
                                                                    observedLeaseRenewTick, 
                                                                    observedLeaseHolder >>
                                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                         /\ UNCHANGED << lookingForLease, 
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
                                                              leading, 
                                                              lastRenew, 
                                                              lastCheck, 
                                                              stepDownAt, tick, 
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
                                                    /\ leaseHolder' = self
                                                    /\ observedLeaseHolder' = [observedLeaseHolder EXCEPT ![self] = self]
                                                    /\ leaseRenewTick' = tick
                                                    /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                                                    /\ lastLeaseChange' = [lastLeaseChange EXCEPT ![self] = -1]
                                                    /\ lastLeaseRenewTick' = [lastLeaseRenewTick EXCEPT ![self] = -1]
                                                    /\ lookingForLease' = [lookingForLease EXCEPT ![self] = FALSE]
                                                    /\ leading' = [leading EXCEPT ![self] = TRUE]
                                               ELSE /\ TRUE
                                                    /\ UNCHANGED << leaseHolder, 
                                                                    leaseRenewTick, 
                                                                    leading, 
                                                                    lookingForLease, 
                                                                    lastRenew, 
                                                                    observedLeaseRenewTick, 
                                                                    observedLeaseHolder, 
                                                                    lastLeaseChange, 
                                                                    lastLeaseRenewTick >>
                                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                         /\ UNCHANGED << lastCheck, stepDownAt, 
                                                         tick >>

Instance(self) == InstanceLoop(self) \/ InstanceObserveLease(self)
                     \/ HolderRenewLease(self)
                     \/ MonitorDecidesStepDown(self)
                     \/ MonitorKeepsLeading(self) \/ DeliverShutdown(self)
                     \/ MonitorHardStop(self)
                     \/ CandidatePrimaryClaimEmptyLease(self)
                     \/ CandidatePrimaryStartClaimOwnedLease(self)
                     \/ CandidatePrimaryClaimOwnedLease(self)

Next == Ticker
           \/ (\E self \in Nodes: Instance(self))

Spec == Init /\ [][Next]_vars

\* END TRANSLATION 
====
