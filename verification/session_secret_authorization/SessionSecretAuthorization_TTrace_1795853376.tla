---- MODULE SessionSecretAuthorization_TTrace_1795853376 ----
EXTENDS Sequences, TLCExt, SessionSecretAuthorization, Toolbox, Naturals, TLC

_expression ==
    LET SessionSecretAuthorization_TEExpression == INSTANCE SessionSecretAuthorization_TEExpression
    IN SessionSecretAuthorization_TEExpression!expression
----

_trace ==
    LET SessionSecretAuthorization_TETrace == INSTANCE SessionSecretAuthorization_TETrace
    IN SessionSecretAuthorization_TETrace!trace
----

_inv ==
    ~(
        TLCGet("level") = Len(_TETrace)
        /\
        requestedSecret = (SecretA)
        /\
        tokenOK = (TRUE)
        /\
        generationOK = (FALSE)
        /\
        response = (ValueA)
        /\
        requestedScope = (ScopeA)
    )
----

_init ==
    /\ requestedScope = _TETrace[1].requestedScope
    /\ generationOK = _TETrace[1].generationOK
    /\ tokenOK = _TETrace[1].tokenOK
    /\ requestedSecret = _TETrace[1].requestedSecret
    /\ response = _TETrace[1].response
----

_next ==
    /\ \E i,j \in DOMAIN _TETrace:
        /\ \/ /\ j = i + 1
              /\ i = TLCGet("level")
        /\ requestedScope  = _TETrace[i].requestedScope
        /\ requestedScope' = _TETrace[j].requestedScope
        /\ generationOK  = _TETrace[i].generationOK
        /\ generationOK' = _TETrace[j].generationOK
        /\ tokenOK  = _TETrace[i].tokenOK
        /\ tokenOK' = _TETrace[j].tokenOK
        /\ requestedSecret  = _TETrace[i].requestedSecret
        /\ requestedSecret' = _TETrace[j].requestedSecret
        /\ response  = _TETrace[i].response
        /\ response' = _TETrace[j].response

\* Uncomment the ASSUME below to write the states of the error trace
\* to the given file in Json format. Note that you can pass any tuple
\* to `JsonSerialize`. For example, a sub-sequence of _TETrace.
    \* ASSUME
    \*     LET J == INSTANCE Json
    \*         IN J!JsonSerialize("SessionSecretAuthorization_TTrace_1795853376.json", _TETrace)

=============================================================================

 Note that you can extract this module `SessionSecretAuthorization_TEExpression`
  to a dedicated file to reuse `expression` (the module in the 
  dedicated `SessionSecretAuthorization_TEExpression.tla` file takes precedence 
  over the module `SessionSecretAuthorization_TEExpression` below).

---- MODULE SessionSecretAuthorization_TEExpression ----
EXTENDS Sequences, TLCExt, SessionSecretAuthorization, Toolbox, Naturals, TLC

expression == 
    [
        \* To hide variables of the `SessionSecretAuthorization` spec from the error trace,
        \* remove the variables below.  The trace will be written in the order
        \* of the fields of this record.
        requestedScope |-> requestedScope
        ,generationOK |-> generationOK
        ,tokenOK |-> tokenOK
        ,requestedSecret |-> requestedSecret
        ,response |-> response
        
        \* Put additional constant-, state-, and action-level expressions here:
        \* ,_stateNumber |-> _TEPosition
        \* ,_requestedScopeUnchanged |-> requestedScope = requestedScope'
        
        \* Format the `requestedScope` variable as Json value.
        \* ,_requestedScopeJson |->
        \*     LET J == INSTANCE Json
        \*     IN J!ToJson(requestedScope)
        
        \* Lastly, you may build expressions over arbitrary sets of states by
        \* leveraging the _TETrace operator.  For example, this is how to
        \* count the number of times a spec variable changed up to the current
        \* state in the trace.
        \* ,_requestedScopeModCount |->
        \*     LET F[s \in DOMAIN _TETrace] ==
        \*         IF s = 1 THEN 0
        \*         ELSE IF _TETrace[s].requestedScope # _TETrace[s-1].requestedScope
        \*             THEN 1 + F[s-1] ELSE F[s-1]
        \*     IN F[_TEPosition - 1]
    ]

=============================================================================



Parsing and semantic processing can take forever if the trace below is long.
 In this case, it is advised to uncomment the module below to deserialize the
 trace from a generated binary file.

\*
\*---- MODULE SessionSecretAuthorization_TETrace ----
\*EXTENDS IOUtils, SessionSecretAuthorization, TLC
\*
\*trace == IODeserialize("SessionSecretAuthorization_TTrace_1795853376.bin", TRUE)
\*
\*=============================================================================
\*

---- MODULE SessionSecretAuthorization_TETrace ----
EXTENDS SessionSecretAuthorization, TLC

trace == 
    <<
    ([requestedSecret |-> SecretA,tokenOK |-> FALSE,generationOK |-> FALSE,response |-> NoValue,requestedScope |-> ScopeA]),
    ([requestedSecret |-> SecretA,tokenOK |-> TRUE,generationOK |-> FALSE,response |-> ValueA,requestedScope |-> ScopeA])
    >>
----


=============================================================================

---- CONFIG SessionSecretAuthorization_TTrace_1795853376 ----
CONSTANTS
    ScopeA = ScopeA
    ScopeB = ScopeB
    SecretA = SecretA
    SecretB = SecretB
    ValueA = ValueA
    ValueB = ValueB
    NoValue = NoValue
    SessionScope = ScopeA
    AuthorizedSecret = SecretA
    AuthorizedValue = ValueA
    EnforceToken = TRUE
    EnforceGeneration = FALSE
    EnforceScope = TRUE
    EnforceSecret = TRUE
    ValueA = ValueA
    ScopeA = ScopeA
    SecretB = SecretB
    ScopeB = ScopeB
    ValueB = ValueB
    SecretA = SecretA
    NoValue = NoValue

INVARIANT
    _inv

CHECK_DEADLOCK
    \* CHECK_DEADLOCK off because of PROPERTY or INVARIANT above.
    FALSE

INIT
    _init

NEXT
    _next

CONSTANT
    _TETrace <- _trace

ALIAS
    _expression
=============================================================================
\* Generated on Sat Nov 28 08:09:37 UTC 2026