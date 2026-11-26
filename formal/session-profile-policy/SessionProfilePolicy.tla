------------------------ MODULE SessionProfilePolicy ------------------------
EXTENDS FiniteSets, Naturals

CONSTANTS Profiles, Tenants, Operations, Sessions, Actions, None, NoPolicy,
          MaxVersion

VARIABLES source, tenant, localPolicy, graphVersion,
          phase, proposedProfile, proposedSource, proposedPolicy, observedVersion,
          started, sessionPolicy, allowed

vars == <<source, tenant, localPolicy, graphVersion,
          phase, proposedProfile, proposedSource, proposedPolicy, observedVersion,
          started, sessionPolicy, allowed>>

TypeOK ==
    /\ source \in [Profiles -> Profiles \cup {None}]
    /\ tenant \in [Profiles -> Tenants]
    /\ localPolicy \in [Profiles -> (SUBSET Actions) \cup {NoPolicy}]
    /\ graphVersion \in 0..MaxVersion
    /\ phase \in [Operations -> {"idle", "validated"}]
    /\ proposedProfile \in [Operations -> Profiles]
    /\ proposedSource \in [Operations -> Profiles \cup {None}]
    /\ proposedPolicy \in [Operations -> (SUBSET Actions) \cup {NoPolicy}]
    /\ observedVersion \in [Operations -> 0..MaxVersion]
    /\ started \in [Sessions -> BOOLEAN]
    /\ sessionPolicy \in [Sessions -> SUBSET Actions]
    /\ allowed \in [Sessions -> SUBSET Actions]

\* The fast model intentionally has two profiles. This is the smallest graph
\* that exposes two concurrent, individually valid updates forming a cycle.
NoInheritanceCycle(s) ==
    \A p \in Profiles:
        s[p] # p /\ ~(s[p] \in Profiles /\ s[s[p]] = p)

TenantSafe(s) ==
    \A p \in Profiles: s[p] \in Profiles => tenant[s[p]] = tenant[p]

PolicyOf(p) ==
    IF localPolicy[p] # NoPolicy
    THEN localPolicy[p]
    ELSE IF source[p] \in Profiles /\ localPolicy[source[p]] # NoPolicy
         THEN localPolicy[source[p]]
         ELSE {}

Init ==
    /\ source = [p \in Profiles |-> None]
    /\ tenant \in [Profiles -> Tenants]
    /\ localPolicy = [p \in Profiles |-> NoPolicy]
    /\ graphVersion = 0
    /\ phase = [o \in Operations |-> "idle"]
    /\ proposedProfile \in [Operations -> Profiles]
    /\ proposedSource = [o \in Operations |-> None]
    /\ proposedPolicy = [o \in Operations |-> NoPolicy]
    /\ observedVersion = [o \in Operations |-> 0]
    /\ started = [s \in Sessions |-> FALSE]
    /\ sessionPolicy = [s \in Sessions |-> {}]
    /\ allowed = [s \in Sessions |-> {}]

ValidateUpdate(o, p, newSource, newPolicy) ==
    LET candidate == [source EXCEPT ![p] = newSource]
    IN  /\ phase[o] = "idle"
        /\ NoInheritanceCycle(candidate)
        /\ TenantSafe(candidate)
        /\ phase' = [phase EXCEPT ![o] = "validated"]
        /\ proposedProfile' = [proposedProfile EXCEPT ![o] = p]
        /\ proposedSource' = [proposedSource EXCEPT ![o] = newSource]
        /\ proposedPolicy' = [proposedPolicy EXCEPT ![o] = newPolicy]
        /\ observedVersion' = [observedVersion EXCEPT ![o] = graphVersion]
        /\ UNCHANGED <<source, tenant, localPolicy, graphVersion,
                        started, sessionPolicy, allowed>>

CommitUpdate(o, p) ==
    /\ phase[o] = "validated"
    \* Compare-and-swap is the implementation obligation: validation and
    \* commit may be separate, but no graph update may occur between them.
    /\ observedVersion[o] = graphVersion
    /\ graphVersion < MaxVersion
    /\ p = proposedProfile[o]
    /\ source' = [source EXCEPT ![p] = proposedSource[o]]
    /\ localPolicy' = [localPolicy EXCEPT ![p] = proposedPolicy[o]]
    /\ graphVersion' = graphVersion + 1
    /\ phase' = [phase EXCEPT ![o] = "idle"]
    /\ UNCHANGED <<tenant, proposedProfile, proposedSource, proposedPolicy, observedVersion,
                    started, sessionPolicy, allowed>>

RejectStale(o) ==
    /\ phase[o] = "validated"
    /\ observedVersion[o] # graphVersion
    /\ phase' = [phase EXCEPT ![o] = "idle"]
    /\ UNCHANGED <<source, tenant, localPolicy, graphVersion,
                    proposedProfile, proposedSource, proposedPolicy, observedVersion,
                    started, sessionPolicy, allowed>>

StartSession(s, p) ==
    /\ ~started[s]
    /\ started' = [started EXCEPT ![s] = TRUE]
    /\ sessionPolicy' = [sessionPolicy EXCEPT ![s] = PolicyOf(p)]
    /\ UNCHANGED <<source, tenant, localPolicy, graphVersion, phase,
                    proposedProfile, proposedSource, proposedPolicy, observedVersion, allowed>>

Authorize(s, a) ==
    /\ started[s]
    /\ a \in sessionPolicy[s]
    /\ allowed' = [allowed EXCEPT ![s] = @ \cup {a}]
    /\ UNCHANGED <<source, tenant, localPolicy, graphVersion, phase,
                    proposedProfile, proposedSource, proposedPolicy, observedVersion,
                    started, sessionPolicy>>

Next ==
    \/ \E o \in Operations, p \in Profiles,
          newSource \in Profiles \cup {None},
          newPolicy \in (SUBSET Actions) \cup {NoPolicy}:
          ValidateUpdate(o, p, newSource, newPolicy)
    \/ \E o \in Operations, p \in Profiles: CommitUpdate(o, p)
    \/ \E o \in Operations: RejectStale(o)
    \/ \E s \in Sessions, p \in Profiles: StartSession(s, p)
    \/ \E s \in Sessions, a \in Actions: Authorize(s, a)

Spec == Init /\ [][Next]_vars

NoCycles == NoInheritanceCycle(source)
TenantIsolation == TenantSafe(source)
AuthorizationSoundness == \A s \in Sessions: allowed[s] \subseteq sessionPolicy[s]
=============================================================================
