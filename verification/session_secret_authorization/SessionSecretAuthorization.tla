------------------- MODULE SessionSecretAuthorization -------------------
EXTENDS TLC, Integers

CONSTANTS ScopeA, ScopeB, SecretA, SecretB, ValueA, ValueB, NoValue,
          SessionScope, EnforceToken, EnforceGeneration, EnforceScope,
          EnforceSecret, AgentCanExpand

Secrets == {SecretA, SecretB}
Scopes == {ScopeA, ScopeB}
Generations == {0, 1}
Values == {ValueA, ValueB, NoValue}

VARIABLES profileSecrets, launchSnapshot, capabilitySecrets,
          currentGeneration, tokenOK, requestedGeneration,
          requestedScope, requestedSecret, response

vars == <<profileSecrets, launchSnapshot, capabilitySecrets,
          currentGeneration, tokenOK, requestedGeneration,
          requestedScope, requestedSecret, response>>

ValueFor(secret) == IF secret = SecretA THEN ValueA ELSE ValueB

Gate(token, requestGeneration, generation, scope, secret, allowedSecrets) ==
  /\ (~EnforceToken \/ token)
  /\ (~EnforceGeneration \/ requestGeneration = generation)
  /\ (~EnforceScope \/ scope = SessionScope)
  /\ (~EnforceSecret \/ secret \in allowedSecrets)

Init ==
  /\ profileSecrets = {SecretA}
  /\ launchSnapshot = profileSecrets
  /\ capabilitySecrets = launchSnapshot
  /\ currentGeneration = 0
  /\ tokenOK = FALSE
  /\ requestedGeneration = 0
  /\ requestedScope = ScopeA
  /\ requestedSecret = SecretA
  /\ response = NoValue

\* A user may edit the profile while the session is running. This must not
\* change the active session capability until a new generation is launched.
UserChangesProfile ==
  /\ profileSecrets' \in SUBSET Secrets
  /\ UNCHANGED <<launchSnapshot, capabilitySecrets, currentGeneration,
                  tokenOK, requestedGeneration, requestedScope,
                  requestedSecret>>
  /\ response' = NoValue

\* Restart is the only action that snapshots a changed profile into the
\* session capability. Toggling a finite generation keeps the model bounded.
Restart ==
  /\ launchSnapshot' = profileSecrets
  /\ capabilitySecrets' = profileSecrets
  /\ currentGeneration' = 1 - currentGeneration
  /\ tokenOK' = FALSE
  /\ requestedGeneration' = currentGeneration'
  /\ requestedScope' = ScopeA
  /\ requestedSecret' = SecretA
  /\ response' = NoValue
  /\ UNCHANGED profileSecrets

Request ==
  /\ tokenOK' \in BOOLEAN
  /\ requestedGeneration' \in Generations
  /\ requestedScope' \in Scopes
  /\ requestedSecret' \in Secrets
  /\ capabilitySecrets' =
       IF AgentCanExpand
       THEN capabilitySecrets \cup {requestedSecret'}
       ELSE capabilitySecrets
  /\ response' =
       IF Gate(tokenOK', requestedGeneration', currentGeneration,
               requestedScope', requestedSecret', capabilitySecrets')
       THEN ValueFor(requestedSecret')
       ELSE NoValue
  /\ UNCHANGED <<profileSecrets, launchSnapshot, currentGeneration>>

Next == UserChangesProfile \/ Restart \/ Request
Spec == Init /\ [][Next]_vars

TypeOK ==
  /\ profileSecrets \subseteq Secrets
  /\ launchSnapshot \subseteq Secrets
  /\ capabilitySecrets \subseteq Secrets
  /\ currentGeneration \in Generations
  /\ tokenOK \in BOOLEAN
  /\ requestedGeneration \in Generations
  /\ requestedScope \in Scopes
  /\ requestedSecret \in Secrets
  /\ response \in Values

\* The agent cannot mutate the capability snapshot through its requests.
CapabilityIsLaunchSnapshot == capabilitySecrets = launchSnapshot

\* A returned value is authorized only by the immutable launch snapshot and
\* all runtime gates, never by the agent's requested ID or current profile.
AuthorizedDisclosure ==
  response = NoValue \/
    /\ tokenOK
    /\ requestedGeneration = currentGeneration
    /\ requestedScope = SessionScope
    /\ requestedSecret \in launchSnapshot
    /\ response = ValueFor(requestedSecret)

=============================================================================
