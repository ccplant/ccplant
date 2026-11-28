------------------- MODULE SessionSecretAuthorization -------------------
EXTENDS TLC

CONSTANTS ScopeA, ScopeB, SecretA, SecretB, ValueA, ValueB, NoValue,
          SessionScope, AuthorizedSecret, AuthorizedValue,
          EnforceToken, EnforceGeneration, EnforceScope, EnforceSecret

VARIABLES tokenOK, generationOK, requestedScope, requestedSecret, response

vars == <<tokenOK, generationOK, requestedScope, requestedSecret, response>>

Gate(token, generation, scope, secret) ==
  /\ (~EnforceToken \/ token)
  /\ (~EnforceGeneration \/ generation)
  /\ (~EnforceScope \/ scope = SessionScope)
  /\ (~EnforceSecret \/ secret = AuthorizedSecret)

ValueFor(scope, secret) ==
  IF scope = SessionScope /\ secret = AuthorizedSecret THEN AuthorizedValue
  ELSE ValueB

Init ==
  /\ tokenOK = FALSE
  /\ generationOK = FALSE
  /\ requestedScope = ScopeA
  /\ requestedSecret = SecretA
  /\ response = NoValue

Request ==
  /\ tokenOK' \in BOOLEAN
  /\ generationOK' \in BOOLEAN
  /\ requestedScope' \in {ScopeA, ScopeB}
  /\ requestedSecret' \in {SecretA, SecretB}
  /\ response' = IF Gate(tokenOK', generationOK', requestedScope', requestedSecret')
                    THEN ValueFor(requestedScope', requestedSecret')
                    ELSE NoValue

Next == Request
Spec == Init /\ [][Next]_vars

AuthorizedDisclosure ==
  response = NoValue \/
    /\ tokenOK
    /\ generationOK
    /\ requestedScope = SessionScope
    /\ requestedSecret = AuthorizedSecret
    /\ response = AuthorizedValue

=============================================================================
