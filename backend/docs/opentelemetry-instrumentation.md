# OpenTelemetry instrumentation design

## Goal

Add useful traces and metrics without scattering SDK lifecycle code throughout
the application. Instrument boundaries first, then add a small number of
business-operation spans where a boundary span cannot explain the work.

## Design

Instrumentation is split into three layers:

1. **Process setup** (`pkg/telemetry.Setup`) owns providers, exporters,
   resources, propagation, sampling, runtime metrics, and shutdown.
2. **Boundary instrumentation** owns protocol details. Echo middleware creates
   server spans and HTTP RED metrics. The shared HTTP client factory installs
   `otelhttp.Transport`, which creates client spans and injects trace context for
   all consumers of that factory.
3. **Business operations** use `telemetry.Operation`. This helper owns span
   start/end, context propagation, error events, and error status. Application
   code supplies only a stable operation name, the function, and safe
   attributes.

This keeps the domain model and repository ports independent of OpenTelemetry.
Infrastructure clients can be instrumented at construction time, while only a
  few method boundaries need an explicit operation wrapper. The duration of the
  emitted span is the method's elapsed time.

```text
Echo middleware (server span)
  -> telemetry.Operation("session.LaunchUseCase.Launch")
       -> repository / service
            -> otelhttp.Transport (client span + context injection)
```

The session launch workflow is the first business operation using this pattern.
It covers HTTP, schedule, webhook, Slack, and worker callers because all of them
enter through the same use case.

## Rules

- Use stable names in `<package>.<type>.<method>` form, such as
  `session.LaunchUseCase.Launch`.
- Instrument public use-case methods and I/O methods. Do not instrument small
  private helpers: their time is already visible as the parent's self time.
- Pass the returned `context.Context` into all downstream calls.
- Put IDs and other high-cardinality values only on traces, never metric labels.
- Never record tokens, messages, request/response bodies, webhook payloads,
  environment values, or repository URLs that may contain credentials.
- Prefer bounded attributes such as scope, provider, result, and operation.
- Let boundary instrumentation use OpenTelemetry semantic conventions; do not
  duplicate HTTP status, method, or URL attributes in business spans.
- Errors are recorded by `telemetry.Operation`; callers should not record the
  same error again on the same span.

## Rollout order

1. Route all newly-created HTTP clients through `pkg/utils.NewHTTPClient`. For
   clients with custom transports, wrap that transport with
   `otelhttp.NewTransport` at construction time.
2. Add supported instrumentation where clients are centrally constructed:
   Kubernetes `rest.Config.Wrap`, Redis hooks, database drivers, AWS SDK
   middleware, and background job entry points.
3. Add `telemetry.Operation` to public use-case methods visible to operators:
   session create/delete/stop, session allocation, schedule execution, webhook
   dispatch, notification delivery, and state upload/restore. This produces a
   per-method call tree without adding SDK lifecycle code to each method.
4. Derive service-level metrics from boundary spans where the backend supports
   span metrics. Add explicit counters/histograms only for business facts that
   cannot be derived, with a reviewed label budget.

Before each rollout, define the operational question it answers and verify one
successful trace, one failed trace, parent/child continuity, and the absence of
sensitive or unbounded attributes.

## Reading method time

For one request, use the trace waterfall. Each operation span shows wall-clock
elapsed time. Child spans show time spent in downstream methods or remote I/O;
the difference between the parent duration and its children is application
self time.

For trends and percentiles, enable Grafana Tempo's metrics-generator (or an
OpenTelemetry Collector span-metrics connector) and group span metrics by
`service.name`, `span.name`, and `status.code`. This yields request rate, errors,
and p50/p95/p99 latency per instrumented method without maintaining a second
duration histogram in application code. Trace sampling and span-metrics
generation should happen after the collector receives spans if accurate rates
are required.

## Deliberate non-goals

- Replacing every `http.Client` in one change. Existing special-purpose clients
  have timeout, proxy, streaming, or signed-URL behavior that must be preserved.
- Globally replacing `http.DefaultTransport`. Doing so can instrument OTLP
  exporter traffic itself and can surprise libraries and tests.
- Reflection-based repository decorators or generated wrappers for every port.
  They create many low-value spans and obscure the operations operators care
  about.
