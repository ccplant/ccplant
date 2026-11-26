# Session profile policy model implementation

This document maps the safety contract in `SessionProfilePolicy.tla` to the
libSQL application store. The graph-wide compare-and-swap described here is
enabled only when the configured store implements libSQL atomic mutations.
The Kubernetes persistence backend keeps its existing behavior.

| TLA+ model | libSQL implementation |
| --- | --- |
| `source` | `source_session_profile_id` in each session-profile record |
| `tenant` | profile scope plus user ID or team ID |
| `graphVersion` | version of `agentapi-session-profile-graph-revision` |
| `ValidateUpdate` | load a stable revision/profile snapshot and validate all source chains |
| `CommitUpdate` | update the profile and graph-revision records in one SQL transaction |
| `RejectStale` | a revision mismatch returns a conflict and causes revalidation or HTTP 409 |
| `NoCycles` | repository validation walks every source chain before commit |
| `TenantIsolation` | every source in a chain must have the same user or team owner |

## Write contract

Create, update, and delete operations mutate the profile record and the shared
graph-revision record in the same libSQL transaction. Before a write, the
repository reads the revision, lists the visible profile graph, then reads the
revision again. A changed revision discards that snapshot.

The transaction uses the record versions as compare-and-swap conditions. A
concurrent writer can therefore commit first, but a writer validated against
the old graph cannot partially update a profile. It must load and validate the
new graph before retrying. Deleting a profile that is still referenced is
rejected with a conflict.

The revision record is created lazily for existing installations. Deployments
must not run old writers that bypass the revision protocol concurrently with
the new implementation. Administrative storage migrations must likewise stop
API writers while changing session-profile records.

## Runtime policy contract

Session startup resolves the effective profile and policy before provisioning.
The provisioner receives the resulting policy snapshot, parses it into a
validated policy, and the CLI broker authorizes only operations present in that
snapshot. Later profile updates do not expand an already-running session's
broker policy.
