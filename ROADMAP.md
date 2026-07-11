# GPT-Load Development Roadmap

The roadmap is ordered by agent execution waves and quality gates, not human
calendar estimates. A wave is complete only when its exit criteria are met.

## Wave 0 — Independent distribution and trust baseline

Status: implemented and verified; introduced for public validation in
`v1.6.0-beta.1`.

- Point images, release checks, documentation, issues, and project links to
  `zhangtyzzz/gpt-load`.
- Remove credentials from system, access, request, API, and CSV logs.
- Move legacy `request_logs.key_value` removal to bounded, cancellable
  background batches so large installations can upgrade without delaying
  readiness or replacing current fingerprints.
- Restrict normal startup cleanup to database-derived key-pool cache data.
- Establish the Apple-inspired theme, navigation, responsive shell, empty
  states, settings dirty protection, and accessibility baseline.
- Add repository-level engineering and interface rules in `AGENTS.md`.

Exit criteria:

- The recommended Compose path reports and runs this fork's version.
- A credential-shaped test value cannot be found in emitted logs or log APIs.
- Startup remains responsive while legacy request-log credentials are removed
  in bounded batches, and shutdown can interrupt the sweep safely.
- A master restart preserves pending request logs and task state while rebuilding
  the key pool.
- Dashboard, Keys, Logs, Settings, and Login pass desktop/mobile light/dark
  browser review.
- Backend and frontend verification gates pass.

## Wave 1 — Proxy reliability foundation

- Isolate proxy concurrency from health and administrative routes.
- Add configurable request, response, and decompression limits.
- Make retries endpoint- and idempotency-aware; expose ambiguous outcomes.
- Make failure counts atomic and reconcile DB/cache projections.
- Replace lossy request-log queue behavior with acknowledge-after-commit flow.
- Add versioned configuration convergence after node reconnect.
- Tune connection pools by database driver and split liveness/readiness checks.
- Add an active-key membership set for O(1) affinity checks and a generation or
  distributed rebuild lock so cluster startup cannot overwrite concurrent key
  changes.
- Reconcile unreachable stale key hashes and mark interrupted global tasks as
  aborted or resumable instead of leaving them running until TTL.
- Add a measured frontend bundle budget and split the remaining roughly 219 KB
  gzip application shell when cold-start telemetry shows the best boundary.
- Migrate the end-of-support `vue-i18n` 9 line to 11 and refresh deprecated
  development-tool transitive dependencies behind compatibility tests.

Exit criteria:

- Saturated long streams do not make health or administration unavailable.
- Master restarts and transient Redis/DB failures do not lose accepted logs.
- Concurrent failure tests cannot diverge database and cache key state.
- The integration matrix covers streaming and non-streaming OpenAI, Anthropic,
  and Gemini paths with memory and Redis stores.

## Wave 2 — Operator intelligence

- Add first-run setup, provider presets, connection tests, and a generated proxy
  credential workflow.
- Add settings search, effective-config preview, diff, history, and rollback.
- Add request/retry timelines with request IDs, TTFT, transfer outcome, token and
  cost metadata, without exposing credentials.
- Add p50/p95/p99 latency, in-flight requests, retry amplification, cooldown,
  blacklist, queue, and upstream-health metrics.
- Publish Prometheus metrics and OpenTelemetry traces.

Exit criteria:

- A new operator can complete the first successful proxy request from an empty
  installation without external instructions.
- Every production incident can be traced from group to attempt chain and final
  outcome without querying raw database rows.
- Configuration changes are previewable, attributable, and reversible.

## Wave 3 — Platform capabilities

- Separate control-plane and data-plane deployment roles.
- Add users, RBAC, OIDC, scoped API tokens, and immutable audit events.
- Add tenant quotas, budgets, provider circuit breakers, bulkheads, and adaptive
  routing.
- Add envelope encryption backed by an external KMS and rotation workflows.
- Add configuration-as-code, backup, restore, and disaster-recovery drills.

Exit criteria:

- Tenant data, budgets, credentials, and administrative privileges are isolated.
- Recovery objectives and service-level objectives are measured continuously.
- New provider adapters can be added without weakening reliability or security
  invariants from earlier waves.

## Deferred until evidence exists

- Additional provider adapters that do not unlock a demonstrated user workflow.
- Decorative animation or customization without a usability outcome.
- Complex adaptive routing before baseline metrics and retry correctness exist.
- Enterprise packaging before identity, security, and recovery gates pass.
