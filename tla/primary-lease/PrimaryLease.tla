---- MODULE PrimaryLease ----
\* This models what happens after a CNPG Primary lost his lease
\* when the fail-safe mode is active.
\*

EXTENDS Integers, TLC

CONSTANTS MaxRounds, LeaseDuration, RenewDeadline, Nodes, LivenessThreshold, RenewPeriod

ASSUME /\ MaxRounds > 0
       \* Timing relations enforced on the implementation side by the
       \* admission webhook (see docs/src/failover.md "Tuning the primary
       \* lease"): the take-over wait must exceed the holder's give-up
       \* deadline, which must exceed the renewal cadence.
       /\ LeaseDuration > RenewDeadline
       /\ RenewDeadline > RenewPeriod
       /\ RenewPeriod > 0

(* --algorithm PrimaryLease {

variables
  leaseHolder = "",
  leaseRenewTick = 0,

  watchdogTick = [n \in Nodes |-> 0],
  leading = [n \in Nodes |-> FALSE],

  lookingForLease = [n \in Nodes |-> FALSE],

  \* ASSUMPTION: no time rate skew
  tick = 0;

define {
  IsAlive(n) == tick - watchdogTick[n] <= LivenessThreshold

  NoTwoLeaders == \A a, b \in Nodes :
    leading[a] /\ leading[b] /\ IsAlive(a) /\ IsAlive(b) => a = b

  TypeOK ==
    /\ leaseHolder \in Nodes \cup {""}
    /\ leaseRenewTick \in 0 .. MaxRounds
    /\ watchdogTick \in [Nodes -> 0 .. MaxRounds]
    /\ leading \in [Nodes -> BOOLEAN]
    /\ lookingForLease \in [Nodes -> BOOLEAN]
    /\ tick \in 0 .. MaxRounds
}

process (Ticker = "tick") {
    T: while (TRUE) {
      if (tick < MaxRounds) {
        tick := tick + 1;
      }
    }
}

process (Instance \in Nodes)
variable observedLeaseRenewTick = 0,
  observedLeaseHolder = "",
  lastRenew=-1,
  lastLeaseChange=-1,
  lastLeaseRenewTick=-1;
{
  InstanceLoop: while (TRUE) {
    either {
      InstanceObserveLease:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ (observedLeaseHolder /= leaseHolder \/ observedLeaseRenewTick /= leaseRenewTick);

        if (lastLeaseChange /= -1 /\ observedLeaseRenewTick /= leaseRenewTick) {
          \* The holder renewed while we were watching: the observation
          \* window is void. Abandon the contest entirely instead of merely
          \* restarting the timer, so lookingForLease means "inside one
          \* unbroken observation window". Re-contesting requires a fresh
          \* CandidatePrimaryStartClaimOwnedLease.
          lastLeaseChange := -1;
          lastLeaseRenewTick := -1;
          lookingForLease[self] := FALSE;
        };

        observedLeaseHolder := leaseHolder;
        observedLeaseRenewTick := leaseRenewTick;
    } or {
      HolderRenewLeaseSucceeds:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ lastRenew /= -1
              /\ RenewPeriod <= tick - lastRenew
              /\ tick - lastRenew <= RenewDeadline
              /\ observedLeaseHolder = self;

        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          lastRenew := tick;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;
          leaseHolder := self;
          observedLeaseHolder := self;

          leading[self] := TRUE;
        };
        watchdogTick[self] := tick;
    } or {
      HolderRenewLeaseFail:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ lastRenew /= -1
              /\ RenewPeriod <= tick - lastRenew
              /\ tick - lastRenew <= RenewDeadline
              /\ observedLeaseHolder = self;

        watchdogTick[self] := tick;
    } or {
      PrimaryInstanceLostLeaseAndOtherNodesUnreachable:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ tick - observedLeaseRenewTick > RenewDeadline
              /\ leading[self];

        \* When we won't reach the other nodes,
        \* we just stop leading, but we can't unlock the lease.
        watchdogTick[self] := tick;
        leading[self] := FALSE;
    } or {
      PrimaryInstanceLostLeaseAndOtherNodesAgreeOnMe:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ leading[self]
              /\ tick - observedLeaseRenewTick > RenewDeadline
              /\ \A n \in Nodes : ~lookingForLease[n];

        \* We can reach all the other nodes, and they
        \* believe I'm the leader, but I can't touch the lease.
        watchdogTick[self] := tick;
    } or {
      PrimaryInstanceLostLeaseAndOtherNodesDoNotAgreeOnMe:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ leading[self]
              /\ tick - observedLeaseRenewTick > RenewDeadline
              /\ \E n \in Nodes : lookingForLease[n];

        \* We can reach all the other nodes, but at least one
        \* of them thinks that I'm not the leader. I'll stop
        \* leading but unfortunately I cannot unlock the lease.
        watchdogTick[self] := tick;
        leading[self] := FALSE;
    } or {
      CandidatePrimaryClaimEmptyLease:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ observedLeaseHolder = "";

        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          lastRenew := tick;
          leaseHolder := self;
          observedLeaseHolder := self;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;

          leading[self] := TRUE;
          watchdogTick[self] := tick;
        };
    } or {
      CandidatePrimaryStartClaimOwnedLease:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ observedLeaseHolder /= self
              /\ observedLeaseHolder /= ""
              /\ lastLeaseChange = -1;

        watchdogTick[self] := tick;
        lookingForLease[self] := TRUE;
        lastLeaseChange := tick;
        lastLeaseRenewTick := observedLeaseRenewTick;
    } or {
      CandidatePetWatchdogWhileWaitingLease:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ observedLeaseHolder /= self
              /\ observedLeaseHolder /= ""
              /\ lastLeaseChange /= -1;

        watchdogTick[self] := tick;
    } or {
      CandidatePrimaryClaimOwnedLease:
        await /\ IsAlive(self)
              /\ tick < MaxRounds
              /\ observedLeaseHolder /= self
              /\ observedLeaseHolder /= ""
              /\ lastLeaseChange /= -1
              /\ lastLeaseRenewTick = observedLeaseRenewTick
              /\ (tick - lastLeaseChange) > LeaseDuration;

        if (observedLeaseRenewTick = leaseRenewTick /\ observedLeaseHolder = leaseHolder) {
          lastRenew := tick;
          leaseHolder := self;
          observedLeaseHolder := self;
          leaseRenewTick := tick;
          observedLeaseRenewTick := tick;

          lastLeaseChange := -1;
          lastLeaseRenewTick := -1;
          lookingForLease[self] := FALSE;
          leading[self] := TRUE;

          watchdogTick[self] := tick;
        };
    } or {
      \* CapStutter: the simulation finished
      await tick = MaxRounds;
      skip;
    }
  };
}

} *)
\* BEGIN TRANSLATION (chksum(pcal) = "37827916" /\ chksum(tla) = "47725d48")
VARIABLES pc, leaseHolder, leaseRenewTick, watchdogTick, leading, 
          lookingForLease, tick

(* define statement *)
IsAlive(n) == tick - watchdogTick[n] <= LivenessThreshold

NoTwoLeaders == \A a, b \in Nodes :
  leading[a] /\ leading[b] /\ IsAlive(a) /\ IsAlive(b) => a = b

TypeOK ==
  /\ leaseHolder \in Nodes \cup {""}
  /\ leaseRenewTick \in 0 .. MaxRounds
  /\ watchdogTick \in [Nodes -> 0 .. MaxRounds]
  /\ leading \in [Nodes -> BOOLEAN]
  /\ lookingForLease \in [Nodes -> BOOLEAN]
  /\ tick \in 0 .. MaxRounds

VARIABLES observedLeaseRenewTick, observedLeaseHolder, lastRenew, 
          lastLeaseChange, lastLeaseRenewTick

vars == << pc, leaseHolder, leaseRenewTick, watchdogTick, leading, 
           lookingForLease, tick, observedLeaseRenewTick, observedLeaseHolder, 
           lastRenew, lastLeaseChange, lastLeaseRenewTick >>

ProcSet == {"tick"} \cup (Nodes)

Init == (* Global variables *)
        /\ leaseHolder = ""
        /\ leaseRenewTick = 0
        /\ watchdogTick = [n \in Nodes |-> 0]
        /\ leading = [n \in Nodes |-> FALSE]
        /\ lookingForLease = [n \in Nodes |-> FALSE]
        /\ tick = 0
        (* Process Instance *)
        /\ observedLeaseRenewTick = [self \in Nodes |-> 0]
        /\ observedLeaseHolder = [self \in Nodes |-> ""]
        /\ lastRenew = [self \in Nodes |-> -1]
        /\ lastLeaseChange = [self \in Nodes |-> -1]
        /\ lastLeaseRenewTick = [self \in Nodes |-> -1]
        /\ pc = [self \in ProcSet |-> CASE self = "tick" -> "T"
                                        [] self \in Nodes -> "InstanceLoop"]

T == /\ pc["tick"] = "T"
     /\ IF tick < MaxRounds
           THEN /\ tick' = tick + 1
           ELSE /\ TRUE
                /\ tick' = tick
     /\ pc' = [pc EXCEPT !["tick"] = "T"]
     /\ UNCHANGED << leaseHolder, leaseRenewTick, watchdogTick, leading, 
                     lookingForLease, observedLeaseRenewTick, 
                     observedLeaseHolder, lastRenew, lastLeaseChange, 
                     lastLeaseRenewTick >>

Ticker == T

InstanceLoop(self) == /\ pc[self] = "InstanceLoop"
                      /\ \/ /\ pc' = [pc EXCEPT ![self] = "InstanceObserveLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "HolderRenewLeaseSucceeds"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "HolderRenewLeaseFail"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "PrimaryInstanceLostLeaseAndOtherNodesUnreachable"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "PrimaryInstanceLostLeaseAndOtherNodesAgreeOnMe"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "PrimaryInstanceLostLeaseAndOtherNodesDoNotAgreeOnMe"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryClaimEmptyLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryStartClaimOwnedLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePetWatchdogWhileWaitingLease"]
                         \/ /\ pc' = [pc EXCEPT ![self] = "CandidatePrimaryClaimOwnedLease"]
                         \/ /\ tick = MaxRounds
                            /\ TRUE
                            /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                      /\ UNCHANGED << leaseHolder, leaseRenewTick, 
                                      watchdogTick, leading, lookingForLease, 
                                      tick, observedLeaseRenewTick, 
                                      observedLeaseHolder, lastRenew, 
                                      lastLeaseChange, lastLeaseRenewTick >>

InstanceObserveLease(self) == /\ pc[self] = "InstanceObserveLease"
                              /\ /\ IsAlive(self)
                                 /\ tick < MaxRounds
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
                                              watchdogTick, leading, tick, 
                                              lastRenew >>

HolderRenewLeaseSucceeds(self) == /\ pc[self] = "HolderRenewLeaseSucceeds"
                                  /\ /\ IsAlive(self)
                                     /\ tick < MaxRounds
                                     /\ lastRenew[self] /= -1
                                     /\ RenewPeriod <= tick - lastRenew[self]
                                     /\ tick - lastRenew[self] <= RenewDeadline
                                     /\ observedLeaseHolder[self] = self
                                  /\ IF observedLeaseRenewTick[self] = leaseRenewTick /\ observedLeaseHolder[self] = leaseHolder
                                        THEN /\ lastRenew' = [lastRenew EXCEPT ![self] = tick]
                                             /\ leaseRenewTick' = tick
                                             /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                                             /\ leaseHolder' = self
                                             /\ observedLeaseHolder' = [observedLeaseHolder EXCEPT ![self] = self]
                                             /\ leading' = [leading EXCEPT ![self] = TRUE]
                                        ELSE /\ TRUE
                                             /\ UNCHANGED << leaseHolder, 
                                                             leaseRenewTick, 
                                                             leading, 
                                                             observedLeaseRenewTick, 
                                                             observedLeaseHolder, 
                                                             lastRenew >>
                                  /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                                  /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                  /\ UNCHANGED << lookingForLease, tick, 
                                                  lastLeaseChange, 
                                                  lastLeaseRenewTick >>

HolderRenewLeaseFail(self) == /\ pc[self] = "HolderRenewLeaseFail"
                              /\ /\ IsAlive(self)
                                 /\ tick < MaxRounds
                                 /\ lastRenew[self] /= -1
                                 /\ RenewPeriod <= tick - lastRenew[self]
                                 /\ tick - lastRenew[self] <= RenewDeadline
                                 /\ observedLeaseHolder[self] = self
                              /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                              /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                              /\ UNCHANGED << leaseHolder, leaseRenewTick, 
                                              leading, lookingForLease, tick, 
                                              observedLeaseRenewTick, 
                                              observedLeaseHolder, lastRenew, 
                                              lastLeaseChange, 
                                              lastLeaseRenewTick >>

PrimaryInstanceLostLeaseAndOtherNodesUnreachable(self) == /\ pc[self] = "PrimaryInstanceLostLeaseAndOtherNodesUnreachable"
                                                          /\ /\ IsAlive(self)
                                                             /\ tick < MaxRounds
                                                             /\ tick - observedLeaseRenewTick[self] > RenewDeadline
                                                             /\ leading[self]
                                                          /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                                                          /\ leading' = [leading EXCEPT ![self] = FALSE]
                                                          /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                                          /\ UNCHANGED << leaseHolder, 
                                                                          leaseRenewTick, 
                                                                          lookingForLease, 
                                                                          tick, 
                                                                          observedLeaseRenewTick, 
                                                                          observedLeaseHolder, 
                                                                          lastRenew, 
                                                                          lastLeaseChange, 
                                                                          lastLeaseRenewTick >>

PrimaryInstanceLostLeaseAndOtherNodesAgreeOnMe(self) == /\ pc[self] = "PrimaryInstanceLostLeaseAndOtherNodesAgreeOnMe"
                                                        /\ /\ IsAlive(self)
                                                           /\ tick < MaxRounds
                                                           /\ leading[self]
                                                           /\ tick - observedLeaseRenewTick[self] > RenewDeadline
                                                           /\ \A n \in Nodes : ~lookingForLease[n]
                                                        /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                                                        /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                                        /\ UNCHANGED << leaseHolder, 
                                                                        leaseRenewTick, 
                                                                        leading, 
                                                                        lookingForLease, 
                                                                        tick, 
                                                                        observedLeaseRenewTick, 
                                                                        observedLeaseHolder, 
                                                                        lastRenew, 
                                                                        lastLeaseChange, 
                                                                        lastLeaseRenewTick >>

PrimaryInstanceLostLeaseAndOtherNodesDoNotAgreeOnMe(self) == /\ pc[self] = "PrimaryInstanceLostLeaseAndOtherNodesDoNotAgreeOnMe"
                                                             /\ /\ IsAlive(self)
                                                                /\ tick < MaxRounds
                                                                /\ leading[self]
                                                                /\ tick - observedLeaseRenewTick[self] > RenewDeadline
                                                                /\ \E n \in Nodes : lookingForLease[n]
                                                             /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                                                             /\ leading' = [leading EXCEPT ![self] = FALSE]
                                                             /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                                             /\ UNCHANGED << leaseHolder, 
                                                                             leaseRenewTick, 
                                                                             lookingForLease, 
                                                                             tick, 
                                                                             observedLeaseRenewTick, 
                                                                             observedLeaseHolder, 
                                                                             lastRenew, 
                                                                             lastLeaseChange, 
                                                                             lastLeaseRenewTick >>

CandidatePrimaryClaimEmptyLease(self) == /\ pc[self] = "CandidatePrimaryClaimEmptyLease"
                                         /\ /\ IsAlive(self)
                                            /\ tick < MaxRounds
                                            /\ observedLeaseHolder[self] = ""
                                         /\ IF observedLeaseRenewTick[self] = leaseRenewTick /\ observedLeaseHolder[self] = leaseHolder
                                               THEN /\ lastRenew' = [lastRenew EXCEPT ![self] = tick]
                                                    /\ leaseHolder' = self
                                                    /\ observedLeaseHolder' = [observedLeaseHolder EXCEPT ![self] = self]
                                                    /\ leaseRenewTick' = tick
                                                    /\ observedLeaseRenewTick' = [observedLeaseRenewTick EXCEPT ![self] = tick]
                                                    /\ leading' = [leading EXCEPT ![self] = TRUE]
                                                    /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                                               ELSE /\ TRUE
                                                    /\ UNCHANGED << leaseHolder, 
                                                                    leaseRenewTick, 
                                                                    watchdogTick, 
                                                                    leading, 
                                                                    observedLeaseRenewTick, 
                                                                    observedLeaseHolder, 
                                                                    lastRenew >>
                                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                         /\ UNCHANGED << lookingForLease, tick, 
                                                         lastLeaseChange, 
                                                         lastLeaseRenewTick >>

CandidatePrimaryStartClaimOwnedLease(self) == /\ pc[self] = "CandidatePrimaryStartClaimOwnedLease"
                                              /\ /\ IsAlive(self)
                                                 /\ tick < MaxRounds
                                                 /\ observedLeaseHolder[self] /= self
                                                 /\ observedLeaseHolder[self] /= ""
                                                 /\ lastLeaseChange[self] = -1
                                              /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                                              /\ lookingForLease' = [lookingForLease EXCEPT ![self] = TRUE]
                                              /\ lastLeaseChange' = [lastLeaseChange EXCEPT ![self] = tick]
                                              /\ lastLeaseRenewTick' = [lastLeaseRenewTick EXCEPT ![self] = observedLeaseRenewTick[self]]
                                              /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                              /\ UNCHANGED << leaseHolder, 
                                                              leaseRenewTick, 
                                                              leading, tick, 
                                                              observedLeaseRenewTick, 
                                                              observedLeaseHolder, 
                                                              lastRenew >>

CandidatePetWatchdogWhileWaitingLease(self) == /\ pc[self] = "CandidatePetWatchdogWhileWaitingLease"
                                               /\ /\ IsAlive(self)
                                                  /\ tick < MaxRounds
                                                  /\ observedLeaseHolder[self] /= self
                                                  /\ observedLeaseHolder[self] /= ""
                                                  /\ lastLeaseChange[self] /= -1
                                               /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                                               /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                               /\ UNCHANGED << leaseHolder, 
                                                               leaseRenewTick, 
                                                               leading, 
                                                               lookingForLease, 
                                                               tick, 
                                                               observedLeaseRenewTick, 
                                                               observedLeaseHolder, 
                                                               lastRenew, 
                                                               lastLeaseChange, 
                                                               lastLeaseRenewTick >>

CandidatePrimaryClaimOwnedLease(self) == /\ pc[self] = "CandidatePrimaryClaimOwnedLease"
                                         /\ /\ IsAlive(self)
                                            /\ tick < MaxRounds
                                            /\ observedLeaseHolder[self] /= self
                                            /\ observedLeaseHolder[self] /= ""
                                            /\ lastLeaseChange[self] /= -1
                                            /\ lastLeaseRenewTick[self] = observedLeaseRenewTick[self]
                                            /\ (tick - lastLeaseChange[self]) > LeaseDuration
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
                                                    /\ watchdogTick' = [watchdogTick EXCEPT ![self] = tick]
                                               ELSE /\ TRUE
                                                    /\ UNCHANGED << leaseHolder, 
                                                                    leaseRenewTick, 
                                                                    watchdogTick, 
                                                                    leading, 
                                                                    lookingForLease, 
                                                                    observedLeaseRenewTick, 
                                                                    observedLeaseHolder, 
                                                                    lastRenew, 
                                                                    lastLeaseChange, 
                                                                    lastLeaseRenewTick >>
                                         /\ pc' = [pc EXCEPT ![self] = "InstanceLoop"]
                                         /\ tick' = tick

Instance(self) == InstanceLoop(self) \/ InstanceObserveLease(self)
                     \/ HolderRenewLeaseSucceeds(self)
                     \/ HolderRenewLeaseFail(self)
                     \/ PrimaryInstanceLostLeaseAndOtherNodesUnreachable(self)
                     \/ PrimaryInstanceLostLeaseAndOtherNodesAgreeOnMe(self)
                     \/ PrimaryInstanceLostLeaseAndOtherNodesDoNotAgreeOnMe(self)
                     \/ CandidatePrimaryClaimEmptyLease(self)
                     \/ CandidatePrimaryStartClaimOwnedLease(self)
                     \/ CandidatePetWatchdogWhileWaitingLease(self)
                     \/ CandidatePrimaryClaimOwnedLease(self)

Next == Ticker
           \/ (\E self \in Nodes: Instance(self))

Spec == Init /\ [][Next]_vars

\* END TRANSLATION
====
