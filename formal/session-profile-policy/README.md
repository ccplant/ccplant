# Session profile policy model

This model checks the safety contract between session-profile inheritance,
policy snapshots, and broker authorization. `Fast.cfg` deliberately uses the
smallest state space that can expose the concurrent `A -> B`, `B -> A` update
cycle. The graph revision is bounded because this is a finite-state safety
check, not a production capacity limit. Two revisions are sufficient to model
both commits in that concurrent-update scenario.

The implementation obligations represented by the model are:

- profile source changes are validated against a consistent tenant graph;
- commit uses a graph-wide revision/CAS, so a validated stale graph is rejected;
- inherited profiles always belong to the same tenant;
- a session snapshots its effective policy when it starts;
- the broker only authorizes actions in that snapshot.

Run the local check from the repository root:

```sh
make lint-policy
```

Set `TLA_TOOLS_JAR=/path/to/tla2tools.jar` to use an existing installation.
Otherwise the script downloads the pinned TLA+ tools release into the user's
cache directory. Java 11 or later is required.
