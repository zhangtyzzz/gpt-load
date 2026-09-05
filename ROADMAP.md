# GPT-Load Development Roadmap

The roadmap is ordered by agent execution waves and quality gates, not human
calendar estimates. A wave is complete only when its exit criteria are met.

## Wave 0 — Independent project and trust baseline

Status: the security and interface baseline was validated in `v1.6.0-beta.1`
and `v1.6.0-beta.2`; standalone release automation is implemented on the
current development line and awaits prerelease validation.

- Point images, release checks, documentation, issues, and project links to
  `zhangtyzzz/gpt-load`.
- Establish this repository as the sole implementation and release source of
  truth, with no automatic synchronization from another repository.
- Consolidate platform packaging behind one draft-release writer, align local,
  CI, release, and container builds on Node 24 LTS, and keep GitHub Actions on
  supported runtimes.
- Remove credentials from system, access, request, API, and CSV logs.
- Move legacy `request_logs.key_value` removal to bounded, cancellable
  background batches so large installations can upgrade without delaying
  readiness or replacing current fingerprints.
- Restrict normal startup cleanup to database-derived key-pool cache data.
- Establish the Apple-inspired theme, navigation, responsive shell, empty
  states, settings dirty protection, and accessibility baseline.
- Add repository-level engineering and interface rules in `AGENTS.md`.

Exit criteria:

- The recommended Compose path reports and runs this project's version.
- GitHub repository metadata points only to this repository or documentation
  maintained by this project.
- One release tag produces one draft with all platform binaries and checksums;
  a non-publishing dry run exercises the same packaging graph.
- A credential-shaped test value cannot be found in emitted logs or log APIs.
- Startup remains responsive while legacy request-log credentials are removed
  in bounded batches, and shutdown can interrupt the sweep safely.
- A master restart preserves pending request logs and task state while rebuilding
  the key pool.
- Dashboard, Keys, Logs, Settings, and Login pass desktop/mobile light/dark
  browser review.
- Backend and frontend verification gates pass.

GitHub fork-network detachment is prepared but externally gated. It is complete
only after repository metadata and release assets are backed up, the maintainer
explicitly accepts GitHub's irreversible metadata-loss warning, GHCR continuity
is verified, and post-detach release validation succeeds. Detachment is not a
normal code-change or release step.

## v1.6.0-beta.3 — Generic HTTP transparency and stateless Hosted MCP

Status: implemented for `v1.6.0-beta.3`; prerelease image, upgrade, and
compatibility validation remain release gates before stable promotion.

- Make Generic HTTP the protocol-neutral proxy path: preserve method, path,
  query, body, end-to-end headers, upstream status, multi-value headers, and
  streaming response behavior by default.
- Express credential injection, validation, response classification, and retry
  eligibility as optional declarative policies. Provider and protocol presets
  only populate those ordinary configuration fields.
- Keep unknown or unconfigured application state pass-through: it cannot
  penalize a key, alter route health, or trigger replay.
- Keep Hosted MCP on the same transparent HTTP path, with no MCP method, tool,
  payload, or session-state branches. The prerelease supports stateless
  Streamable HTTP; a concrete stateful service is required before expanding
  that boundary.
- When a Generic aggregate child has no selectable key or a safe retry exhausts
  its attempted keys, continue to a healthy sibling without replaying unsafe
  methods or reusing a key already attempted by the request.

Exit criteria:

- Integration tests cover arbitrary methods, nested paths, repeated query and
  response headers, binary and JSON bodies, non-2xx pass-through, chunked/SSE
  streaming, cancellation, and upstream truncation without protocol branches.
- Retry and accounting tests prove that unknown, missing, malformed, and
  unconfigured application state neither penalizes a key nor replays a request.
- Aggregate tests cover a preferred child in cooldown, a retryable failure in a
  high-weight child, attempted-key exclusion, healthy sibling fallback, and the
  no-replay boundary for unsafe methods.
- Preset equivalence tests prove that applying a preset produces the same
  persisted configuration and runtime behavior as entering those fields by
  hand; choosing Custom restores the generic pass-through path.
- A code-path audit finds no vendor, MCP method, tool-name, or JSON-RPC branch in
  the proxy core, and the backend/frontend verification gates pass.

## Wave 1 — Proxy reliability foundation

- Isolate proxy concurrency from health and administrative routes.
- Add configurable request, response, and decompression limits.
- Make retries endpoint- and idempotency-aware; expose ambiguous outcomes.
- Make failure counts atomic and reconcile DB/cache projections.
- Replace lossy request-log queue behavior with acknowledge-after-commit flow.
- Add versioned configuration convergence after node reconnect.
- Tune connection pools by database driver and split liveness/readiness checks.
- Support an opt-in serverless database idle mode that removes periodic database
  wakeups while keeping health checks and on-demand operations available.
- Add an active-key membership set for O(1) affinity checks and a generation or
  distributed rebuild lock so cluster startup cannot overwrite concurrent key
  changes.
- Reconcile unreachable stale key hashes and mark interrupted global tasks as
  aborted or resumable instead of leaving them running until TTL.
- Cache normalized Generic HTTP configuration in immutable group snapshots and
  maintain an ID index so aggregate routing does not reparse every child or
  linearly scan the full group list on each request.
- Preserve valid, invalid, and inconclusive key-validation outcomes through
  manual-task reporting and cron scheduling; a wholly inconclusive run must not
  be reported as invalid or advance the successful-validation timestamp.
- Re-evaluate session affinity only when a real stateful HTTP/MCP service shows
  that transparent forwarding plus its own session token is insufficient. Start
  with response-learned reuse of the existing key-affinity mapping, restricted
  to a standard group with one upstream. Add complete child/upstream/key route
  persistence only if a concrete aggregate or multi-upstream failure proves it
  necessary, with an end-to-end reproduction and lifecycle design first.
- Add a measured frontend bundle budget and split the remaining roughly 231 KB
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
