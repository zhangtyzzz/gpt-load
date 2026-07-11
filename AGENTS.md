# GPT-Load Maintainer Guide

This repository is an independently maintained distribution of
`zhangtyzzz/gpt-load`. These instructions apply to every future agent and
contributor working in this repository.

## Source of truth

1. Runtime code and executable tests are the source of truth.
2. This file defines the intended engineering and product constraints.
3. README and roadmap text must be updated when behavior changes, but must not
   override what the code actually does.
4. When documentation and implementation disagree, verify the implementation,
   fix the inconsistency, and add a regression test.

## Product identity

- Official repository: `https://github.com/zhangtyzzz/gpt-load`.
- Official container image: `ghcr.io/zhangtyzzz/gpt-load`.
- Release checks, issue links, contribution links, badges, examples, and default
  deployment files must point to this fork.
- Repository-owned GitHub Actions targets must derive their package namespace
  from `${{ github.repository }}`. Do not hard-code the owner there: this repo
  publishes to `zhangtyzzz/gpt-load`, while downstream forks must publish to
  their own namespace.
- Upstream material may be referenced only when explicitly labelled as upstream
  attribution or migration context. It must never look like this project's
  official support or release channel.

## Product character

The console should feel calm, precise, and trustworthy. It is an operations
tool, so clarity and control come before decoration.

- Show health, required attention, and the next useful action first.
- Common tasks stay visible; advanced configuration is progressively disclosed.
- Every action has immediate feedback and an observable completion state.
- Empty, loading, partial, error, and permission-denied states are designed
  states, not accidental blanks.
- Never render a control that appears actionable but silently does nothing.
- Preserve user agency with undo where practical, narrow confirmations for
  irreversible actions, and dirty-state protection for editable forms.

## Apple-inspired interface system

Apple-inspired means fluid behavior, spatial consistency, restraint, and craft.
It does not mean applying glass effects to every surface.

### Visual foundations

- Define reusable color, spacing, radius, shadow, typography, and motion tokens
  centrally under `web/src/theme/` and `web/src/assets/style.css`.
- Use the platform system font stack and enable optical sizing. Tighten tracking
  only for large headings; body copy stays near neutral tracking.
- Use translucent material only for floating chrome such as navigation, sticky
  toolbars, popovers, and sheets. Data cards and dense tables use stable solid
  surfaces for legibility.
- Support light and dark appearance from the same semantic tokens. Do not copy
  raw light-mode colors into component files.
- Avoid generic purple gradients, gratuitous glow, decorative noise, or visual
  effects unrelated to system state.
- Prefer deliberate whitespace and alignment over adding borders between every
  region.

### Motion and input

- Feedback begins on pointer-down. Pressed controls should respond immediately.
- Motion must start from the current on-screen value and remain interruptible.
- Default movement is critically damped with no decorative bounce. Bounce is
  reserved for interactions that inherit user momentum.
- Animate compositor-friendly `transform` and `opacity`; avoid `transition: all`.
- Enter and exit along the same spatial path, anchored to the triggering control.
- Do not use infinite float, bounce, or pulse animations for decoration. A pulse
  is allowed only when it communicates a live, ongoing state.
- Implement `prefers-reduced-motion`, `prefers-reduced-transparency`, and
  `prefers-contrast` fallbacks for every shared visual primitive.

### Accessibility and responsiveness

- Every icon-only control requires an accessible name. Tooltip text is not a
  replacement for `aria-label`.
- All interactive controls need visible keyboard focus and a logical tab order.
- Do not encode status using color alone; pair it with text or iconography.
- Text must remain usable at increased browser text size without clipping.
- Desktop layouts should support dense operator workflows. Mobile layouts should
  prioritize the primary action, avoid horizontal page overflow, and preserve a
  comfortable touch target.
- Validate at minimum at 1440x900, 1024x768, and 390x844 in both light and dark
  appearance.

### Vue and Naive UI implementation

- Use `NConfigProvider` with typed global theme overrides for semantic theming.
- Import Naive UI components directly in each SFC, or register an explicit small
  subset. Do not install the entire component library globally.
- Keep route views focused on composition. Extract domain state and reusable
  presentation units before a component grows beyond a reviewable size.
- New user-visible text must go through i18n for Chinese, English, and Japanese.
- Forms validate inline, show save progress, expose dirty state, and guard route
  changes plus browser unload when edits are unsaved.
- Global loading uses reference counting or operation-scoped state, never one
  shared boolean that concurrent requests can clear incorrectly.

## Security invariants

- Secrets never appear in application logs, access logs, request logs, API
  responses, CSV exports, browser storage diagnostics, or error messages.
- Centralize credential redaction. Cover authorization headers and query/body
  fields such as `key`, `api_key`, `token`, `access_token`, and equivalents.
- Treat upstream error bodies as hostile. Before logging or returning them,
  remove the exact credential used for that attempt and its encoded variants;
  field-name-based redaction alone is insufficient when an upstream echoes a
  key as unlabelled text.
- Operational correlation uses a non-reversible fingerprint and a short mask,
  never the original credential.
- Request-log writes must pass through `sanitizeRequestLog`; the legacy
  `key_value` field is display compatibility only and may contain a
  `utils.KeyFingerprint(key_hash)`, never an encrypted or plaintext key.
- Do not decrypt or export upstream credentials from request logs. Full-key
  search may hash the input, while display and CSV use only the fingerprint.
- Credentials and full-key search values must travel in authenticated request
  bodies, never URL paths or query strings. Browser downloads use the shared
  authenticated HTTP client and Blob URLs; they never append the console key.
- One-way hashes are internal correlation data. API DTOs expose only the short
  fingerprint and must not serialize the full `key_hash`.
- GORM logging must keep `ParameterizedQueries: true` at every log level.
- Never log a complete configuration struct. Emit an explicit allow-listed
  summary of non-sensitive fields instead.
- Log files and secret-bearing local data use least-privilege filesystem modes.
- Client-side credential generation must use Web Crypto `getRandomValues`; never
  use `Math.random`. Secret inputs are masked by default with an explicit,
  accessible reveal action.
- Any change touching authentication or logging includes a regression test that
  asserts the original secret is absent from output.
- Treat wildcard CORS, disabled encryption, request-body logging, and debug mode
  as explicit deployment risks, not invisible defaults.

## Data and cache semantics

- The database is the source of truth for groups, keys, and settings.
- Redis and the memory store contain both derived caches and transient
  coordination data. Do not call a namespace-wide clear during normal startup.
- Derived key-pool data may be deleted and reconstructed from the database.
- Buffered request logs, pending queues, running task state, and other data that
  has not reached its durable destination must survive a master restart.
- Historical credential cleanup must run asynchronously without delaying
  readiness, in bounded and cancellable batches. It must preserve current
  non-reversible `fp:*` identifiers, yield between batches, and retry without
  exposing database values or blocking normal startup.
- New store keys must document whether they are derived, ephemeral, coordinating,
  or pending-durable, together with their expiry and recovery behavior.
- Cross-node invalidation must converge after reconnect; Pub/Sub alone is not a
  durable configuration version mechanism.

## Proxy correctness

- Bound request bodies, error bodies, decompression, and model-list responses.
- Separate data-plane concurrency from health and administrative control-plane
  capacity.
- Automatic retries must account for method safety and whether the upstream may
  already have accepted the request. Represent ambiguous outcomes explicitly.
- Preserve multi-value response headers and remove hop-by-hop headers.
- Classify client cancellation, upstream truncation, and completed responses
  separately in metrics and request logs.
- Database updates are authoritative. Cache projection must be idempotent and
  recoverable after partial failure.

## Verification gates

Backend changes should pass:

```sh
gofmt -w <changed-go-files>
go vet ./...
go test ./...
go test -race ./internal/...
```

Frontend changes should pass:

```sh
cd web
npm run lint:check
npm run format:check
npm run type-check
npm run build
```

For UI changes, run the application with isolated temporary data and verify the
critical flows in a real browser at the viewport sizes listed above. Check
empty, populated, loading, error, and dirty-form states. Do not use production
credentials or mutate the maintainer's existing data during verification.

Security-sensitive release branches also run `govulncheck`, `gosec`, and
`npm audit`, with findings triaged for reachability rather than copied blindly.

## Release discipline

- Every release starts with a pull request. Push the agent branch, let all
  required CI checks pass, review the final diff and compatibility notes, and
  merge before creating a release tag. Wait for the resulting `main` commit's
  CI to pass as well; never tag an unmerged or unverified feature branch.
- Treat Git tags as immutable release identities. Create an annotated tag on
  the verified `main` merge commit and never force-move or reuse a published
  version.
- Pin Docker and release builders to an exact, currently supported Go security
  patch. Check the official Go release history and run `govulncheck` with that
  same toolchain before tagging; a floating or locally stale compiler is not
  release evidence.
- Cross-cutting UI rewrites, security changes, data cleanup, migrations, and
  compatibility changes ship through `vMAJOR.MINOR.PATCH-beta.N` before stable.
  Do not create a stable tag merely to test whether packaging works.
- The `beta` channel is for upgrade and packaging validation. The current
  Docker workflow publishes an exact prerelease tag plus `beta` for tags that
  contain `-beta`; it must not update `latest`. Publish the exact image first,
  validate that immutable digest, and promote the moving `beta` alias only
  after smoke tests pass. Test deployments should pin the exact version rather
  than the moving `beta` alias.
- A beta is promotable only after the tag workflows succeed and both a fresh
  install and an upgrade from the previous stable release pass against isolated
  data. Exercise real proxy traffic, affinity, restart behavior, historical
  cleanup, request-log redaction, and the supported database/storage matrix in
  proportion to the change.
- After all assets are attached, publish the beta GitHub draft as a
  pre-release. Release notes must call out incompatible API behavior,
  irreversible cleanup, backup requirements, rollback limits, and the exact
  image tag or digest used for validation.
- Only a stable semantic-version tag may advance `latest`. Create it from the
  commit that passed beta validation (or from a later reviewed fix commit),
  verify the multi-architecture image and promote its tested digest to
  `latest`, then publish the GitHub Release as stable. `docker-compose.yml`
  continues to target `latest` for normal users.
- Keep the Docker upgrade-smoke baseline set to the actual previous stable tag
  before creating a release PR. A passing fresh-install test is not a substitute
  for exercising the persisted data path from that stable version.
- Record the PR, merge commit, tag, workflow results, image digest, smoke-test
  evidence, and known limitations in the release handoff. If any required gate
  fails, fix through another PR and increment `beta.N`; never patch a published
  image in place.

## Change discipline

- Use `codex/` branch names for agent-led work.
- Keep unrelated user changes intact and never use destructive Git recovery.
- Prefer small, independently verifiable changes even when they share a branch.
- Add a test for every fixed bug when the behavior can be exercised reliably.
- Do not add a dependency for styling or a small utility that existing platform
  APIs can provide.
- Update `ROADMAP.md` when a milestone is completed or reprioritized.
