---------------------- MODULE SecretConfidentiality ----------------------
EXTENDS Naturals, TLC

CONSTANTS NoSecret, SecretA, SecretB, Redacted

VARIABLES storedA, storedB, expectedA, expectedB,
          responseA, responseB, disclosureAllowed

vars == <<storedA, storedB, expectedA, expectedB,
          responseA, responseB, disclosureAllowed>>

Init ==
  /\ storedA = SecretA
  /\ storedB = SecretB
  /\ expectedA = SecretA
  /\ expectedB = SecretB
  /\ responseA = Redacted
  /\ responseB = Redacted
  /\ disclosureAllowed = FALSE

Read ==
  /\ responseA' = Redacted
  /\ responseB' = Redacted
  /\ disclosureAllowed' = FALSE
  /\ UNCHANGED <<storedA, storedB, expectedA, expectedB>>

UpdateOmitted ==
  /\ UNCHANGED <<storedA, storedB, expectedA, expectedB>>
  /\ responseA' = Redacted
  /\ responseB' = Redacted
  /\ disclosureAllowed' = FALSE

UpdateProvided ==
  /\ storedA' = SecretB
  /\ storedB' = SecretA
  /\ expectedA' = SecretB
  /\ expectedB' = SecretA
  /\ responseA' = Redacted
  /\ responseB' = Redacted
  /\ disclosureAllowed' = FALSE

Rotate ==
  /\ storedA' = SecretB
  /\ storedB' = SecretA
  /\ expectedA' = SecretB
  /\ expectedB' = SecretA
  /\ responseA' = SecretB
  /\ responseB' = SecretA
  /\ disclosureAllowed' = TRUE

Next == Read \/ UpdateOmitted \/ UpdateProvided \/ Rotate

Spec == Init /\ [][Next]_vars

StoredSecretPreserved ==
  /\ storedA = expectedA
  /\ storedB = expectedB

ReadResponsesAreSecretIndependent ==
  disclosureAllowed \/ responseA = responseB

SecretsAreWriteOnly ==
  disclosureAllowed \/
    /\ responseA \notin {SecretA, SecretB}
    /\ responseB \notin {SecretA, SecretB}

=============================================================================
