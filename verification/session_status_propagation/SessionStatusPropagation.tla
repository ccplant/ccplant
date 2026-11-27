--------------------- MODULE SessionStatusPropagation ---------------------
EXTENDS Integers, Sequences, TLC

CONSTANT Replicas

CanonicalStatuses == {"creating", "starting", "active", "running",
                      "suspending", "suspended", "resuming", "stopped",
                      "terminating", "terminated", "unhealthy", "error",
                      "timeout", "unknown"}

Events == [
    e1 |-> [status |-> "active",  version |-> 1, valid |-> TRUE],
    e2 |-> [status |-> "running", version |-> 2, valid |-> TRUE],
    e3 |-> [status |-> "surprise", version |-> 3, valid |-> FALSE]
]

VARIABLES replicaStatus, replicaVersion, maxSeenVersion
vars == <<replicaStatus, replicaVersion, maxSeenVersion>>

Init ==
    /\ replicaStatus = [r \in Replicas |-> "starting"]
    /\ replicaVersion = [r \in Replicas |-> 0]
    /\ maxSeenVersion = [r \in Replicas |-> 0]

Deliver(r, event) ==
    LET accepted == event.valid /\ event.version > replicaVersion[r]
    IN /\ replicaStatus' = [replicaStatus EXCEPT
               ![r] = IF accepted THEN event.status ELSE @]
       /\ replicaVersion' = [replicaVersion EXCEPT
               ![r] = IF accepted THEN event.version ELSE @]
       /\ maxSeenVersion' = [maxSeenVersion EXCEPT
               ![r] = IF event.valid /\ event.version > @ THEN event.version ELSE @]

EventSet == {Events.e1, Events.e2, Events.e3}

Next == \E r \in Replicas, event \in EventSet : Deliver(r, event)

VersionNeverRegresses == \A r \in Replicas : replicaVersion[r] = maxSeenVersion[r]
OnlyCanonicalStatusPropagates == \A r \in Replicas : replicaStatus[r] \in CanonicalStatuses

Spec == Init /\ [][Next]_vars

=============================================================================
