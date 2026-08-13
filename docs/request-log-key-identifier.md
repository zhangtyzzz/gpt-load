# Request-log key identifier: research, design, and impact assessment

## 0. Problem statement

The request-log list shows a column "key identifier" (`logs.keyIdentifier`) whose value is a
non-reversible fingerprint such as `fp:6f9ef8c7ce8d`. Operators report that this value cannot be
matched against anything they can see elsewhere in the product, so when a row shows a `400` they
cannot tell *which* key in Keys management produced it.

Reported request: render the value the way Keys management renders a key (a `****` mask).

The underlying goal, which is what this change is actually measured against:

> Seeing a failing row in the request log, an operator must be able to tell which row in Keys
> management that key is.

"Use a mask" is the proposed *means*. Identifiability is the *end*.

---

## 1. Research findings

Every claim below cites `file:line` at the commit this document was written against. Anything that
could not be established from the repository is marked **Unconfirmed**.

### 1.1 Architecture and the path from request to log row

Stack: Go + Gin + GORM backend, Vue 3 + Naive UI frontend, SQLite/MySQL/PostgreSQL, Redis or
in-memory store (`CLAUDE.md:5-11`). Package layout is described in `CLAUDE.md:14-25`.

Request path, and where the log row is produced:

1. `internal/proxy/server.go` handles the proxied request; on completion it builds a
   `models.RequestLog` (`internal/proxy/server.go:842-855`).
2. The key identity fields are set only when a key was actually selected
   (`internal/proxy/server.go:867-872`):
   ```go
   logEntry.KeyHash = ps.encryptionSvc.Hash(apiKey.KeyValue)
   logEntry.KeyValue = utils.KeyFingerprint(logEntry.KeyHash)
   ```
3. `ps.requestLogService.Record(logEntry)` (`internal/proxy/server.go:885`) assigns an ID and
   timestamp, re-sanitizes, and either writes straight through or buffers into the store
   (`internal/services/request_log_service.go:337-359`).
4. Buffered rows are flushed by `flush()` → `writeLogsToDB()`
   (`internal/services/request_log_service.go:362-431`, `:434-572`).
5. Reads go through `LogService.GetLogsQuery` (`internal/services/log_service.go:103-105`) and the
   handler `respondWithLogs` (`internal/handler/log_handler.go:91-122`).

### 1.2 How the `fp:` identifier is generated, and whether it is reversible

`utils.KeyFingerprint` (`internal/utils/redaction_utils.go:299-308`):

```go
func KeyFingerprint(keyHash string) string {
    keyHash = strings.TrimSpace(keyHash)
    if keyHash == "" { return "" }
    if len(keyHash) > keyFingerprintLength { keyHash = keyHash[:keyFingerprintLength] }
    return "fp:" + strings.ToLower(keyHash)
}
```

`keyFingerprintLength = 12` (`internal/utils/redaction_utils.go:18`). So the fingerprint is
`"fp:"` + the first 12 hex characters of `keyHash`.

`keyHash` itself comes from `encryption.Service.Hash`:

- With an `ENCRYPTION_KEY` configured: HMAC-SHA256 keyed with the derived AES key
  (`internal/encryption/encryption.go:83-90`).
- Without one: plain SHA-256 (`internal/encryption/encryption.go:104-110`).

**It is not reversible.** Both branches are one-way hashes, and only a 12-character prefix of the
digest survives into the display value. There is no code path that recovers a key from a
fingerprint; `ParseKeyFingerprint` (`internal/utils/redaction_utils.go:312-327`) only validates the
shape and hands back the hex prefix for a `LIKE` lookup.

A brute-force inversion is also not practical for real keys: the hash is keyed (HMAC) whenever
`ENCRYPTION_KEY` is set, and API keys are high-entropy strings.

### 1.3 Is the value stored or computed?

**Both, redundantly.**

- Stored: `request_logs.key_value` (`internal/models/types.go:172`, `type:text`) is written with the
  fingerprint. `sanitizeRequestLog` overwrites the field unconditionally on every write path
  (`internal/services/request_log_service.go:580`):
  ```go
  log.KeyValue = utils.KeyFingerprint(log.KeyHash)
  ```
- Stored: `request_logs.key_hash` (`internal/models/types.go:173`,
  `type:varchar(128);index`) holds the full digest.
- Computed at read time: the API does **not** return the stored column. `newLogResponse`
  recomputes the fingerprint from `KeyHash` (`internal/handler/log_handler.go:52`):
  ```go
  KeyValue: utils.KeyFingerprint(logEntry.KeyHash),
  ```

So the read path already ignores `request_logs.key_value` entirely and derives the displayed
identifier from `key_hash`. This is important: the display value can be changed without touching
stored rows.

There is also a background job that **erases** `request_logs.key_value` for any row whose stored
value differs from the current fingerprint —
`purgeHistoricalKeyValueBatch` (`internal/services/request_log_service.go:157-169`):

```go
if row.KeyValue != "" && row.KeyValue != utils.KeyFingerprint(row.KeyHash) {
    legacyIDs = append(legacyIDs, row.ID)
}
...
Update("key_value", "")
```

It runs on every startup (`internal/services/request_log_service.go:76-79`). Consequence for the
design: **anything persisted into `key_value` that is not exactly the fingerprint will be wiped by
this sweep.** A design that persists a mask into that column would fight this job.

### 1.4 Keys management masking rules — backend vs frontend

There are two masking functions and **they do not agree**:

| | Location | Rule | Output for `sk-abcdefghijklmnop` |
|---|---|---|---|
| Backend | `internal/utils/string_utils.go:9-15` | `key[:4] + "****" + key[len-4:]` | `sk-a****mnop` |
| Frontend | `web/src/utils/display.ts:51-56` | `key[0:4] + "..." + key[len-4:]` | `sk-a...mnop` |

Same 4-and-4 window, different separator.

Both return the input unchanged when `len(key) <= 8`
(`internal/utils/string_utils.go:11-13`, `web/src/utils/display.ts:52-54`).

What Keys management actually renders is the **frontend** rule, i.e. `...` not `****`
(`web/src/components/keys/KeyTable.vue:329`):

```js
return key.is_visible ? key.key_value : maskKey(key.key_value);
```

Note also that Keys management prefers the note over the key when a note exists
(`web/src/components/keys/KeyTable.vue:325-330`).

So the reporter's mental model ("Keys management shows `****`") does not match the code: it shows
`...`. `MaskAPIKey` is the `****` one, and it is a Go-side logging helper.

**Backend has no involvement in the Keys-management mask**: the keys list endpoint decrypts and
returns the *complete* key to the browser (`internal/handler/key_handler.go:254-263`), and the
browser masks it for display. The full key is already available to any authenticated admin.

### 1.5 Are keys encrypted at rest, and is plaintext available when the log row is written?

- At rest: **encrypted**. `KeyService` encrypts before insert
  (`internal/services/key_service.go:122,131`) and stores the hash alongside
  (`internal/services/key_service.go:117`).
- In the proxy: **plaintext is available.** The keypool decrypts on load
  (`internal/keypool/provider.go:198-213`):
  ```go
  decryptedKeyValue, err := p.encryptionSvc.Decrypt(encryptedKeyValue)
  ...
  KeyValue: decryptedKeyValue,
  ```
  So at `internal/proxy/server.go:870`, `apiKey.KeyValue` is the plaintext key. A mask *could* be
  computed at write time.

### 1.6 How log query and the "search full key or key identifier" box work

Matching happens in `logFiltersScope` (`internal/services/log_service.go:66-73`):

```go
if filter.KeyValue != "" {
    if hashPrefix, ok := utils.ParseKeyFingerprint(filter.KeyValue); ok {
        db = db.Where("key_hash LIKE ?", hashPrefix+"%")
    } else {
        keyHash := s.EncryptionSvc.Hash(filter.KeyValue)
        db = db.Where("key_hash = ?", keyHash)
    }
}
```

Two accepted inputs, both resolved against `request_logs.key_hash`:

1. An `fp:` fingerprint → prefix `LIKE` on `key_hash`.
2. Anything else → treated as a complete key, hashed, exact match on `key_hash`.

Transport split: `GET /logs` rejects anything that is not a fingerprint so a complete key cannot
land in a URL (`internal/handler/log_handler.go:150-154`), while `POST /logs/search` accepts a
complete key in the JSON body (`internal/handler/log_handler.go:82-89`). The frontend always uses
the POST form (`web/src/api/logs.ts:7-9`), so the browser path reaches the `else` branch above for
any non-`fp:` input.

**Anything that is neither a fingerprint nor a complete key silently matches zero rows.** There is
no "unrecognized input" signal.

### 1.7 What export contains

`StreamLogKeysToCSV` (`internal/services/log_service.go:108-160`). Header is
`key_identifier, group_name, status_code` (`internal/services/log_service.go:114`), deduplicated to
the newest row per `key_hash` by a window function (`internal/services/log_service.go:124-139`),
and the first column is the fingerprint (`internal/services/log_service.go:150`):

```go
utils.KeyFingerprint(record.KeyHash),
```

So yes — export carries the same identifier, and it is the only key-ish column.

### 1.8 Frontend rendering and i18n

- Column definition: `web/src/components/logs/LogTable.vue:350-356`, `key: "key_value"`,
  title `t("logs.keyIdentifier")`, `render: renderKeyIdentifier`.
- Cell renderer with the copy button: `web/src/components/logs/LogTable.vue:239-262`. It renders
  `row.key_value` and copies that same string.
- Detail modal shows it again: `web/src/components/logs/LogTable.vue:891-911`.
- Search box bound to `filters.key_value` with placeholder `t("logs.keySearchPlaceholder")`:
  `web/src/components/logs/LogTable.vue:623-630`.
- Type: `RequestLog.key_value?: string` (`web/src/types/models.ts:189`); the log table aliases it as
  `LogRow` (`web/src/components/logs/LogTable.vue:43`).
- i18n entries exist in all three locales:
  - `web/src/locales/zh-CN.ts:673-675` — `keyIdentifier`, `keySearchPlaceholder`, `copyKeyIdentifier`
  - `web/src/locales/en-US.ts:698-700`
  - `web/src/locales/ja-JP.ts:696-698`

### 1.9 Additional finding: `key_hash` is not rewritten on key rotation

`internal/commands/migrate.go` re-encrypts and re-hashes **`api_keys` only**
(`internal/commands/migrate.go:516-546`); `grep` for `request_log` in that file returns nothing.
Likewise the `v1.1.0` migration backfills `api_keys.key_hash` only
(`internal/db/migrations/v1_1_0_AddKeyHashColumn.go:16-18,51-61`), and it does so with
`encryption.NewService("")` (`:27`), i.e. an unkeyed SHA-256 regardless of the deployment's
`ENCRYPTION_KEY`.

Consequences (these are pre-existing, not introduced here):

- After an `ENCRYPTION_KEY` rotation, `request_logs.key_hash` of pre-rotation rows was computed with
  the old key and no longer equals the new `api_keys.key_hash` for the same key.
- The same key can therefore appear under two different fingerprints across a rotation. The export
  dedup comment (`internal/services/log_service.go:123`) alludes to exactly this.

**Unconfirmed:** whether any deployment relies on the `v1.1.0` unkeyed backfill remaining unkeyed; I
did not find a follow-up migration that re-hashes those rows with the configured key.

---

## 2. Regression baseline (captured before any edit)

Go — `GOPROXY=off go test -count=1 ./internal/...`: **all packages pass**, no failures.
Packages with no test files: `commands`, `container`, `db/migrations`, `encryption`, `errors`,
`failover`, `i18n`, `i18n/locales`, `models`, `response`, `router`, `syncer`, `types`, `version`.

Frontend — `cd web && npx --no-install vitest run`: **16 files / 66 tests passed**.

---

## 3. Design

### 3.1 Chosen approach: resolve the mask at read time by joining on `key_hash`

The decisive observation from §1.3 and §1.1: **`api_keys.key_hash` and `request_logs.key_hash` are
produced by the same function over the same plaintext** —
`internal/services/key_service.go:117` (`Hash(trimmedKey)`) and
`internal/proxy/server.go:870` (`Hash(apiKey.KeyValue)`, plaintext per §1.5) — and **both columns are
indexed** (`internal/models/types.go:146`, `:173`).

So a log row can be resolved to its `api_keys` row with no schema change at all:

```
request_logs.key_hash ──(indexed equality)──> api_keys.key_hash
                                                   │
                                          api_keys.key_value (encrypted)
                                                   │ Decrypt
                                              plaintext key
                                                   │ mask
                                            "sk-a****mnop"
```

The display value is computed in `LogService`, batched per page, and never persisted.

#### Options considered

| Option | Persist new data? | Historical rows | Consistency with Keys mgmt | Verdict |
|---|---|---|---|---|
| **A. Persist mask at write time** into `request_logs.key_value` (plaintext available per §1.5) | Yes — 8 plaintext chars per row, permanently | No benefit; old rows stay `fp:` | Same rule, but a second copy that can drift | **Rejected** |
| **B. Resolve at read time via `key_hash` → `api_keys`** | **No** | **Retroactively fixed** | Same decrypted source Keys mgmt uses | **Chosen** |
| C. Add `request_logs.key_id` FK + migration | Yes (a column) + migration | Not backfillable (no key identity to recover) | Same | Rejected: strictly worse than B, plus migration cost |

Why B wins on every axis that matters here:

1. **Nothing new is written to the database.** The invariant enforced by
   `sanitizeRequestLog` and the historical purge job (§1.3) stays exactly as it is: the only key
   material in `request_logs` remains a one-way hash and its fingerprint. Option A would have
   required weakening `sanitizeRequestLog:580` *and* rewriting the purge predicate at
   `request_log_service.go:158` — that predicate is a security control, and I would rather not
   loosen it.
2. **Historical rows are fixed for free.** Any old row that still has a `key_hash` matching a live
   key resolves to a mask immediately, with no backfill. Option A only helps rows written after
   deployment — which would leave the reporter's actual complaint ("I can't investigate afterwards")
   unsolved for exactly the data they want to investigate.
3. **Consistency is structural, not by convention.** The mask is derived from
   `api_keys.key_value` decrypted with `EncryptionSvc` — byte-for-byte the same input Keys
   management masks in the browser (`internal/handler/key_handler.go:254-263`). There is no second
   copy that can drift.

Cost: one extra indexed query per page (≤ page size distinct hashes; default page size 15) plus a
few AES-GCM decrypts. Negligible relative to a proxied LLM request.

### 3.2 Mask rule, and how consistency with Keys management is guaranteed

Canonical rule, `first 4 + "****" + last 4`, with one addition over today's helpers: **short keys
are never revealed**.

```
len(key) == 0        -> ""            (no key on this row)
len(key) <= 8        -> "****"        (NEW: today's helpers return the key verbatim)
otherwise            -> key[:4] + "****" + key[len-4:]
```

Implemented once per language and used by both screens:

- Go: `utils.MaskKeyIdentifier` in `internal/utils/string_utils.go`, used by the log read path.
- TS: `maskKey` in `web/src/utils/display.ts`, used by Keys management, changed from `...` to
  `****` and given the same `<= 8` rule.

The `<= 8` change matters: `MaskAPIKey` and `maskKey` both return a short key **in full**
(§1.4). For Keys management that is cosmetic — the browser already holds the complete key. For the
request log it would be a real leak, because a short key would be emitted verbatim into the log API
response and the CSV export. `MaskKeyIdentifier` therefore collapses short keys to `****`.

`MaskAPIKey` is left untouched: it is a separate Go logging helper with its own callers, and
changing it is out of scope.

**Consequence, stated plainly:** Keys management display changes from `sk-a...mnop` to
`sk-a****mnop`. This is a deliberate, user-visible change. It is what makes "the same as Keys
management" literally true rather than approximately true, and `****` is the form the reporter
asked for. No existing test asserts the `...` form (verified: no `display.test.ts` exists, and
`web/src/components/keys/KeyTable.test.ts` contains no mask assertion).

### 3.3 What the log column shows, in priority order

> **Revised in §7.1.** Case 1 below was originally the bare mask. Measurement showed that colliding
> masks made two distinct keys render identically, so case 1 now carries a discriminator. The rest of
> this section stands.

```
1. identifier of the live key  e.g. "sk-a****mnop#b91b0e612994" (key_hash resolves to a live key)
2. fingerprint                 e.g. "fp:6f9ef8c7ce8d"   (key_hash present, but no live key)
3. ""                          (no key_hash at all — row logged without a selected key)
```

Case 2 is the honest answer for a row whose key has since been deleted, or whose `key_hash`
predates an `ENCRYPTION_KEY` rotation (§1.9): the key it points at is not in Keys management any
more, so there is no Keys-management row to be "consistent" with. Showing a mask would be a
fabrication; showing the fingerprint preserves the one thing that is still true — rows sharing a
fingerprint used the same key. The UI labels this case so it is not mistaken for a bug (§3.6).

### 3.4 Masks are not unique

> **Superseded by §7.1.** The mitigation described here was measured and found insufficient: it left
> the list column ambiguous, which is the exact failure this change exists to remove. §7.1 records
> the collision probabilities, the measured output, and the fix. Kept here to show what was
> originally shipped and why it was not enough.

A mask exposes 8 characters, and provider key prefixes are shared: every OpenAI project key starts
`sk-p`, so in practice only the last 4 characters distinguish keys within a provider. Two keys in
one group **can** collide.

The reporter's requested representation is therefore not, by itself, a unique identifier. Rather
than silently ship an ambiguous column, the change keeps the unique value available:

- `LogResponse` gains `key_fingerprint`, carrying the `fp:` value. This is not new exposure — it is
  exactly what `key_value` carries today.
- The list column shows the mask (recognizable). The detail modal shows **both** the mask and the
  fingerprint, each independently copyable.
- Search accepts the mask **and** the fingerprint **and** the complete key (§3.5). When a mask is
  ambiguous, the fingerprint gives an exact query.

What this missed: the fingerprint being available *elsewhere* does not help an operator scanning a
list of rows that all read `sk-p****9z7q`. The list value itself has to be unique.

### 3.5 Search: keeping the copy-and-paste path working

The reporter's workflow is literally "copy the column, paste it into the box". After this change the
column contains a mask, and a mask reaches the `else` branch of §1.6, is hashed, and matches
**nothing** — a silent empty result. That is a regression and must be handled, not documented away.

`logFiltersScope` becomes a three-way decision:

```
input looks like "AAAA****ZZZZ"        -> resolve to api_keys with that prefix/suffix,
  or "AAAA****ZZZZ#hhhh"                  narrowed by the hash prefix when present, then
                                          key_hash IN (their hashes); no match -> match nothing
input looks like "fp:<12 hex>"         -> key_hash LIKE prefix%          (unchanged)
otherwise                              -> key_hash = Hash(input)         (unchanged, complete key)
```

Identifier detection is deliberately strict — exactly 12 mask characters with `****` at offsets
4..8, plus an optional `#` and exactly 4 hex characters — so it cannot swallow a complete key. A real
key of exactly that shape would be pathological; the risk of mis-routing one is far smaller than the
certainty of breaking copy-and-paste.

Resolution scans `api_keys` in batches of `chunkSize` (500, the existing constant at
`internal/services/key_service.go:20`), decrypts each key, and keeps hashes whose plaintext matches
the prefix and suffix. When a discriminator is present the scan is first narrowed by an indexed
`key_hash LIKE` prefix, which is what the column now shows and which reduced measured decryptions
from 400 to 1 (§7.2). Matches are capped (1000 keys) so a degenerate bare mask cannot build an
unbounded `IN` clause; the cap is logged when hit rather than silently truncating.

The bare mask stays accepted so an identifier copied from the previous build still resolves; it
returns every key that masks that way rather than guessing one.

The placeholder text is also updated in all three locales to say masks are accepted.

### 3.6 Export

Yes, export changes, for the same reason the column does: the export exists so operators can take
key identifiers away and act on them, and an export that disagrees with the screen is a trap.

`key_identifier` keeps the same priority order as the column (mask → fingerprint → empty), and a
second column `key_fingerprint` is added so the unique value is always present even when the first
column holds a mask. Header becomes:

```
key_identifier, key_fingerprint, group_name, status_code
```

Adding a column rather than replacing one keeps every existing consumer's first column meaningful.

### 3.7 Security assessment

The `fp:` scheme is clearly deliberate, not accidental: there is a dedicated redaction module
(`internal/utils/redaction_utils.go`), a write-path hard gate (`sanitizeRequestLog:580`), a
startup job that erases legacy `key_value` (`request_log_service.go:217-261`), a GET/POST transport
split so keys stay out of URLs (`log_handler.go:148-154`), and an explicit comment that even the
one-way hash must never be serialized (`internal/handler/log_handler.go:19-21`). Any change here
owes an argument.

**What is newly exposed:** 8 characters of each key (first 4, last 4) appear in the
`GET/POST /logs` response and in the CSV export.

**What is not:**

- *Not in the database.* Masks are computed at read time; `request_logs` still stores only
  `key_hash` and the `fp:` value. `sanitizeRequestLog` and the historical purge job are unchanged.
  A database dump, a replica, or a backup leaks no more than before — this is the property Option A
  would have given up.
- *Not to a new audience.* Both endpoints sit behind the same admin auth as
  `GET /keys`, which already returns **complete plaintext keys** to the browser
  (`internal/handler/key_handler.go:254-263`). Anyone who can read the log page can already read
  every key in full. The mask grants an authenticated admin no capability they lack.
- *Not enough to reconstruct a key.* 8 of ~40+ characters, with no way to obtain the remainder.

**Genuine residual costs, and how they are handled:**

1. **CSV files.** Exports get downloaded, mailed, and pasted into tickets, and now carry partial key
   material. This is the real delta, since a CSV escapes the auth boundary. Accepted because a
   masked identifier is only correlatable by someone who *already* has the key list; mitigated by
   keeping `key_fingerprint` in the export so a caller who wants a shareable, non-reversible
   identifier still has one, and can drop the masked column.
2. **Short keys.** Today's mask helpers would emit a key of ≤ 8 characters verbatim.
   `MaskKeyIdentifier` collapses those to `****` (§3.2), so the log path cannot print a whole key.
3. **Search-side scanning.** Mask search decrypts `api_keys` in memory. Plaintext is never returned
   or logged — only `key_hash` values leave the resolver — and the batch cap bounds the work.

**Judgement:** acceptable. The exposure is 8 characters, to an audience that already holds the full
keys, and — critically — it does not weaken any at-rest or write-path control. The alternative
(persisting masks) would have traded away exactly those controls for a strictly worse outcome.

---

## 4. Impact assessment against existing behaviour

Each row verified in code, not assumed.

| Area | Verified effect | Evidence |
|---|---|---|
| **Write path** | **Unchanged.** No new field is persisted; `sanitizeRequestLog` still forces `key_value = KeyFingerprint(key_hash)` | `internal/services/request_log_service.go:580` untouched |
| **Historical purge job** | **Unchanged, and still correct.** Its predicate compares stored `key_value` against `KeyFingerprint(key_hash)`; since writes still store the fingerprint, no new row is considered legacy. Had a mask been persisted, this job would have erased it | `internal/services/request_log_service.go:157-169` |
| **DB schema / migration** | **None.** No column added, changed, or dropped; the join uses existing indexed `key_hash` on both tables. An old database upgrading to this build needs no migration and behaves identically on day one | `internal/models/types.go:146,173` |
| **Read path** | `respondWithLogs` resolves masks once per page and passes the map into `newLogResponse` | `internal/handler/log_handler.go:91-122`, `:44-66` |
| **Missing `api_keys` table / DB error** | Resolver returns an empty map instead of failing the request, so every row falls back to the fingerprint. This keeps the endpoint working on a partially-migrated database — and is why the existing handler tests, whose in-memory DB migrates `RequestLog` only, keep passing unchanged | `internal/handler/log_handler_test.go:34-36`, `:115-117`, `:197-199` |
| **Fingerprint search** | **Unchanged.** `ParseKeyFingerprint` branch is untouched and still checked before the complete-key branch | `internal/services/log_service.go:67-68` |
| **Complete-key search** | **Unchanged.** Strict mask shape (12 chars, `****` at 4..8) cannot match a real key, so keys still fall to the hash-equality branch | `internal/services/log_service.go:70-71` |
| **Mask search** | New third branch; unresolvable mask matches nothing explicitly rather than by accident | new code in `logFiltersScope` |
| **GET endpoint hardening** | **Unchanged.** `GET /logs` still admits fingerprints only, so a complete key still cannot enter a URL. Masks are intentionally *not* admitted on GET either — narrower is safer, and the UI uses POST | `internal/handler/log_handler.go:150-154` |
| **Export** | Gains `key_fingerprint` column; `key_identifier` now prefers the mask. Row count and dedup logic unchanged | `internal/services/log_service.go:114,148-157` |
| **Frontend rendering** | List cell and detail modal read the new fields; copy button copies the displayed string, which search now accepts | `web/src/components/logs/LogTable.vue:239-262`, `:350-356`, `:891-911` |
| **Keys management** | Mask changes `...` → `****`; no logic change. No existing test asserts the old form | `web/src/components/keys/KeyTable.vue:329`, `web/src/utils/display.ts:51-56` |
| **Group proxy keys display** | Also changes `...` → `****`, because `maskProxyKeys` delegates to `maskKey`. Intentional: one mask rule across the product | `web/src/utils/display.ts:63-71`, `web/src/components/keys/GroupInfoCard.vue:83` |
| **i18n** | `keySearchPlaceholder` reworded and two new keys added in all three locales | `zh-CN.ts:673-675`, `en-US.ts:698-700`, `ja-JP.ts:696-698` |
| **`MaskAPIKey` callers** | Untouched — new function added rather than the existing one changed | `internal/utils/string_utils.go:9-15` |

### 4.1 Existing test whose policy this change revises

One existing test encodes the old *display policy* rather than a bug:

- `TestStreamLogKeysToCSVExportsFingerprintOnly`
  (`internal/services/log_service_test.go:188-249`) asserts the export's first column equals
  `utils.KeyFingerprint(keyHash)`.

Its in-memory database migrates `RequestLog` and `GroupHourlyStat` only
(`internal/services/log_service_test.go:22-32`), so no `api_keys` row exists, the resolver falls
back, and **the assertion still holds unmodified**. It was left untouched; its name now reads as
"fingerprint when the key cannot be resolved", which is still exactly what it verifies. New tests
cover the resolvable case. No existing test was modified, skipped, or weakened for this change.

---

## 5. Files changed

Backend:

- `internal/utils/string_utils.go` — add `MaskKeyIdentifier` (never reveals short keys) and
  `KeyIdentifier` (mask + hash discriminator; added in §7.1).
- `internal/utils/redaction_utils.go` — add `ParseKeyIdentifier` (strict `AAAA****ZZZZ` shape with an
  optional `#hhhh` discriminator) and `KeyMatchesMask`.
- `internal/services/log_service.go` — add `ResolveKeyIdentifiers` (batched `key_hash` → identifier)
  and `resolveMaskedKeyHashes`; wire the identifier branch into `logFiltersScope`; export gains
  `key_fingerprint`.
- `internal/handler/log_handler.go` — `LogResponse.key_fingerprint`; resolve identifiers per page.

Frontend:

- `web/src/utils/display.ts` — `maskKey` uses `****` and masks short keys; exports
  `KEY_MASK_MARKER`.
- `web/src/types/models.ts` — `RequestLog.key_fingerprint?`.
- `web/src/components/logs/LogTable.vue` — render the identifier, show the fingerprint in the detail
  modal, tooltip on unresolved rows.
- `web/src/locales/{zh-CN,en-US,ja-JP}.ts` — reworded placeholder + `keyFingerprint`,
  `copyKeyFingerprint`, `keyIdentifierUnresolved`.

Tests and verification:

- `internal/utils/string_utils_test.go`, `internal/utils/redaction_utils_test.go`
- `internal/services/log_service_test.go`, `internal/handler/log_handler_test.go`
- `web/src/utils/display.test.ts`
- `internal/services/keyidentifier_verification_test.go` — runnable side-by-side proof (§6)

Added by the audit pass (§7):

- `internal/handler/log_identifier_cost_test.go` — SQL-statement and decryption counters; per-page
  and per-search-input cost measurement (§7.2)
- `internal/services/keyidentifier_audit_test.go` — mask collision, key lifecycle including a real
  `ENCRYPTION_KEY` rotation, and the removed-key misconfiguration probe (§7.1, §7.3, §7.4)
- `internal/handler/log_export_audit_test.go` — real export bytes cross-checked against the page
  (§7.6)
- `web/src/components/logs/LogTable.test.ts` — mounted render path, no double masking (§7.5)

---

## 6. Verification

### 6.1 Executable end-to-end check

Beyond unit tests, a runnable scenario proves the reporter's requirement end to end. See
`TestVerifyLogKeyIdentifierMatchesKeyManagement` in
`internal/services/keyidentifier_verification_test.go`:

```bash
cd repos/gpt-load
GOPROXY=off go test ./internal/services/ -run TestVerifyLogKeyIdentifierMatchesKeyManagement -v
```

It builds an in-memory database with encryption enabled (keys AES-GCM encrypted at rest, correlated
by HMAC-SHA256), several distinct keys, both success and `400` rows, and one historical row whose key
has been deleted. Actual output (regenerated after the §7.1 fix):

```
=========================================================================
 PART 1  Key management  vs  request-log identifier
=========================================================================
LOG ROW           KEY MGMT SHOWS  LOG COLUMN           MASK MATCHES?
------------------------------------------------------------------------------
log-alpha-bad     sk-p****7d4e    sk-p****7d4e#b91b0e612994    yes
log-alpha-ok      sk-p****7d4e    sk-p****7d4e#b91b0e612994    yes
log-bravo-bad     sk-p****6a2b    sk-p****6a2b#55c1f6292cf4    yes
log-charlie-ok    sk-a****4b81    sk-a****4b81#77ec4dce8c08    yes

3 distinct keys -> 3 distinct identifiers (each key is individually recognizable)

One key, two outcomes:  200 row -> "sk-p****7d4e#b91b0e612994"   400 row -> "sk-p****7d4e#b91b0e612994"

=========================================================================
 PART 2  Historical row whose key was deleted from key management
=========================================================================
log row        : log-historical-deleted
key mgmt shows : (nothing - the key no longer exists)
log column     : fp:7b1e4a90c3d2
reason         : nothing remains but a one-way hash, so a mask cannot be
                 derived; the fingerprint is shown instead of a fabricated one.

=========================================================================
 PART 3  Search paths
=========================================================================
SEARCH INPUT                        KIND                                    ROWS
----------------------------------------------------------------------------------
sk-p****7d4e#b91b0e612994           displayed identifier (copied column)    2
sk-p****7d4e                        bare mask (legacy form)                 2
sk-proj-alpha-9f3c2b1a7d4e          complete key                            2
fp:b91b0e612994                     fingerprint                             2
fp:7b1e4a90c3d2                     fingerprint (deleted key)               1
zzzz****zzzz                        mask matching no key                    0

=========================================================================
 PART 4  CSV export
=========================================================================
key_identifier,key_fingerprint,group_name,status_code
sk-p****6a2b#55c1f6292cf4,fp:55c1f6292cf4,primary,400
sk-a****4b81#77ec4dce8c08,fp:77ec4dce8c08,primary,200
fp:7b1e4a90c3d2,fp:7b1e4a90c3d2,primary,401
sk-p****7d4e#b91b0e612994,fp:b91b0e612994,primary,200

=========================================================================
 PART 5  What the request_logs table actually stores
=========================================================================
LOG ROW                   STORED key_value        CONTAINS KEY MATERIAL?
--------------------------------------------------------------------------
log-alpha-bad             fp:b91b0e612994         no
log-alpha-ok              fp:b91b0e612994         no
log-bravo-bad             fp:55c1f6292cf4         no
log-charlie-ok            fp:77ec4dce8c08         no
log-historical-deleted    fp:7b1e4a90c3d2         no

Masks are derived at read time from api_keys, so the log table keeps
storing only a one-way hash and its fingerprint - unchanged by this work.

--- PASS: TestVerifyLogKeyIdentifierMatchesKeyManagement (0.01s)
```

Note what Part 1 shows about the collision risk of §7.1: `sk-proj-alpha-…` and `sk-proj-bravo-…`
share the `sk-p` head and are told apart only by their tails. The discriminator is what guarantees
they stay distinct even when the tails coincide too.

### 6.2 Test suite comparison against the §2 baseline

| Suite | Baseline | After | Delta |
|---|---|---|---|
| Go (`go test -count=1 ./...`) | all pass | all pass | no new failures; 14 packages ok |
| Frontend (`vitest run`) | 16 files / 66 tests pass | 17 files / 73 tests pass | +1 file, +7 tests (all new); no new failures |

Difference set: **empty**. No pre-existing test failed, was modified, skipped, or weakened.

Also clean: `go build ./...`, `go vet ./internal/...`, `gofmt -l` on every changed Go file,
`eslint` and `prettier --check` on every changed frontend file, and `vue-tsc --noEmit`.

`gofmt -l internal/` still reports `internal/keypool/affinity.go`,
`internal/keypool/affinity_test.go` and `internal/models/setting_info.go`. These are pre-existing and
untouched by this change; they were left alone rather than reformatted as unrelated churn.

---

## 7. Adversarial self-audit

The sections above were largely reasoned from reading code. This section replaces that reasoning
with measurement. Everything below is produced by tests in the repository, and the numbers and
outputs are copied from actual runs.

One real defect was found and fixed. It is described first.

### 7.1 Defect found: colliding masks were indistinguishable — FIXED

**The claim §3.4 made:** masks are not unique, mitigated by keeping the fingerprint in the detail
modal and in export.

**What measurement showed:** that mitigation does nothing for the problem an operator actually hits.
Two keys differing only in the middle produced *byte-identical* list values:

```
COMPLETE KEY                BARE MASK       LOG COLUMN SHOWS
sk-proj-AAAAAAAAAAAA9z7q    sk-p****9z7q    sk-p****9z7q
sk-proj-BBBBBBBBBBBB9z7q    sk-p****9z7q    sk-p****9z7q
   -> two distinct keys render identically as "sk-p****9z7q"
```

An operator reading two such rows has no way to know they are different keys, and will read one
key's failure as the other's. That is precisely the failure mode the whole change exists to remove,
so §3.4's "documented limitation" framing was wrong: it was a defect.

**How likely it is.** A collision needs the same first four *and* last four characters. The first
four are effectively constant within a provider (`sk-p` for every OpenAI project key), so the
entropy is the trailing four. Birthday bound over base62⁴ = 14,776,336:

| keys in one group | P(at least one colliding pair) |
|---|---|
| 100 | 0.03% |
| 500 | 0.84% |
| 1,000 | 3.33% |
| 5,000 | 57.08% |
| 10,000 | 96.61% |

gpt-load exists to rotate large key pools, so the thousands column is ordinary usage, not a tail
case. And the measured search fixture is starker still: 400 keys generated with a shared prefix and
a shared suffix format produced **one single mask for all 400** — real key formats are not uniformly
random in their trailing characters either.

**The fix.** The displayed identifier is now the mask plus a four-hex-character discriminator taken
from the key hash — `utils.KeyIdentifier` in `internal/utils/string_utils.go`:

```
sk-p****9z7q#2cbb4afc57aa        (key one)
sk-p****9z7q#75a3ef835247        (key two)
```

Properties, all asserted by tests:

- **Unique per key**, so the list is never ambiguous.
- **Stable**: derived only from the row's own key hash, so the same key always renders the same
  string regardless of what else is on the page. A page-local collision check was rejected for
  exactly this reason — it would have made one key render two different ways.
- **Still matches key management**: the mask is an exact prefix, so the recognizable part is
  unchanged and the two screens remain comparable by eye. Tests assert `HasPrefix`, not just
  equality of some derived value.
- **No new exposure**: those four characters are the leading characters of the fingerprint this API
  has always published.
- **Free**: no extra query, no extra decryption.

`ParseKeyIdentifier` accepts both the new form and the bare mask, so an identifier copied from the
previous build still resolves.

**Unexpected second benefit, measured:** because the discriminator is an indexed `key_hash` prefix,
searching the displayed value stopped being a table scan. See §7.2.

### 7.2 Cost of read-time resolution — measured, not estimated

`internal/handler/log_identifier_cost_test.go` instruments a real `GET /logs` with a GORM logger that
records every executed statement and an `encryption.Service` wrapper that counts `Decrypt` calls.
Fixture: 120 distinct keys, 3 log rows each.

```
PAGE SIZE   SQL TOTAL   request_logs   api_keys   DECRYPTS   DISTINCT KEYS ON PAGE
15          3           2              1          5          5
30          3           2              1          10         10
60          3           2              1          20         20
```

Statements executed for the default page size of 15:

```
1. SELECT count(*) FROM `request_logs`
2. SELECT * FROM `request_logs` ORDER BY timestamp desc LIMIT 15
3. SELECT `key_hash`,`key_value` FROM `api_keys` WHERE key_hash IN ("44c2…","5d0b…", …)
```

- **SQL is constant in page size**: 3 statements always. Two of them (count, find) are pre-existing;
  this feature adds exactly one batched `IN` query against an indexed column. **Not N+1.**
- **Decryption is per distinct key, never per row.** A separate test drives 50 rows that all share
  one key: **1 decrypt, 3 statements**. Deduplication before the lookup is what makes this hold, and
  the test fails if it is removed.

Search cost by input kind, 400 keys (all sharing one bare mask by construction):

```
KIND                        SQL   DECRYPTS   ROWS MATCHED
displayed identifier        2     1          1
bare mask (legacy form)     2     400        400
fingerprint                 1     0          1
complete key                1     0          1
```

- Fingerprint and complete-key search never touch `api_keys` at all — unchanged from before.
- A bare mask must compare plaintext, so it decrypts the table. This is the one genuinely expensive
  path the feature adds, it is bounded by `maskSearchKeyLimit`, and it is no longer what the column
  shows.
- The displayed identifier narrows by an indexed hash prefix first: **400 decryptions down to 1**.

### 7.3 Key lifecycle — three states, actual output

`TestAuditKeyLifecycleNeverMisattributes` performs a real `ENCRYPTION_KEY` rotation the way
`internal/commands/migrate.go` does (re-encrypt and re-hash `api_keys`, leave `request_logs` alone):

```
LOG ROW                 KEY STATE                       COLUMN SHOWS         KIND
life-deleted            deleted from key management     fp:e001a49e36c2      fingerprint
life-historical         never resolvable (hash only)    fp:0f1e2d3c4b5a      fingerprint
life-rotated-after      live key, post-rotation row     sk-r****6789#eb55f55967fd    masked identifier
life-rotated-before     live key, pre-rotation row      fp:72e7d6c7d2dd      fingerprint
life-survivor           live key, pre-rotation row      fp:948a35d81d86      fingerprint
```

The test asserts, for every unresolvable row, that the identifier (a) is exactly the fingerprint,
(b) contains no mask marker, and (c) **is not the identifier of any live key** — the last being the
direct check that key A's failure is never attributed to key B.

**A correction to §1.9 and §3.3.** Round 1 described the rotation gap as affecting "rows written
before the rotation" and treated it as a corner case. Measurement shows the blast radius is wider
and worth stating plainly: since the rotation re-hashes every key, **every pre-rotation log row
loses its mask — including rows belonging to keys that are still present and untouched.**
`life-survivor` above is exactly that case, and my first version of this test asserted it would
still resolve. It does not. The cost is recognizability on historical rows after a rotation, not
correctness: those rows show a fingerprint and remain searchable by it (verified below). This is a
pre-existing gap in the rotation tooling that this feature inherits; closing it would mean
re-hashing `request_logs`, which is out of scope here.

Search after rotation:

```
SEARCH INPUT                    KIND                       ROWS   WHICH
sk-s****ijkl#02d3be29e1e0       survivor, no post-rot row  0
sk-r****6789#eb55f55967fd       rotated key, post-rot row  1      life-rotated-after
fp:948a35d81d86                 survivor pre-rotation fp   1      life-survivor
fp:e001a49e36c2                 deleted key fingerprint    1      life-deleted
fp:0f1e2d3c4b5a                 historical row fingerprint 1      life-historical
```

Pre-rotation rows stay reachable by fingerprint. That is the concrete reason the fingerprint is
retained alongside the mask rather than replaced by it.

### 7.4 Misconfiguration probed: `ENCRYPTION_KEY` removed without migrating

Not asked for, but found while working through §7.3 and worth recording because it is the one state
where the join succeeds but decryption does not. If `ENCRYPTION_KEY` is removed while `api_keys`
still holds ciphertext and pre-existing hashes, the hash still matches, and `Decrypt` becomes a
no-op that returns the ciphertext.

```
LOG ROW             KEY MGMT SHOWS  LOG COLUMN SHOWS
misconfig-one       fe10****6d97    fe10****6d97#9c85e386c715
misconfig-two       975d****87f8    975d****87f8#ab6cce1b29f0
```

Verified properties: the two keys stay distinct, no plaintext appears, and the log column still
begins with exactly what key management shows — because both screens decrypt through the same
service and therefore fail identically. So an operator can still pair a row with a key row; the
characters simply are not the real key. This state already mis-renders key management
(`internal/handler/key_handler.go:254-263` decrypts the same way), so it is a pre-existing
consequence of the misconfiguration rather than something the log identifier introduces. Recorded
rather than "fixed" because the fix belongs in the key migration tooling, not here.

### 7.5 No double processing between backend and frontend — asserted

The risk: the backend now returns an already-masked value while `web/src/utils/display.ts` still
exports a masking function, so a second application would corrupt the display.

`web/src/components/logs/LogTable.test.ts` mounts the real component with a backend-shaped response
and asserts:

- the cell contains the backend identifier **verbatim** (`sk-l****mnop#b91b0e612994`);
- the double-masked form (`maskKey("sk-l****mnop#b91b0e612994")` → `sk-l****2994`) is **absent**. The test
  first asserts these two strings differ, so it cannot pass vacuously;
- the key management value for the same key (`maskKey(plaintext)` → `sk-l****mnop`) is an exact
  prefix of the log value, and is present in the rendered output;
- a fallback row renders its fingerprint and contains no `****` at all;
- two colliding-mask rows render two different strings.

Confirmed by code reading and now by assertion: `renderKeyIdentifier`
(`web/src/components/logs/LogTable.vue:245`) renders `row.key_value` directly and never calls
`maskKey`. The key column and the log column each mask exactly once — the key column in the browser
because it receives plaintext, the log column on the server because the browser never receives the
key.

### 7.6 Export bytes — generated and cross-checked

`internal/handler/log_export_audit_test.go` generates the real export and compares it against what
`GET /logs` puts on the page for the same rows. Actual bytes:

```
key_identifier,key_fingerprint,group_name,status_code
sk-o****ijkl#6c53b782fb8f,fp:6c53b782fb8f,primary,400
fp:9a8b7c6d5e4f,fp:9a8b7c6d5e4f,primary,401
sk-p****9z7q#ba3a1325aa83,fp:ba3a1325aa83,primary,400
sk-p****9z7q#f16c39e3b759,fp:f16c39e3b759,primary,400
```

```
EXPORT key_identifier   PAGE key_value          SAME?
sk-o****ijkl#6c53b782fb8f    sk-o****ijkl#6c53b782fb8f    yes
fp:9a8b7c6d5e4f              fp:9a8b7c6d5e4f              yes
sk-p****9z7q#ba3a1325aa83    sk-p****9z7q#ba3a1325aa83    yes
sk-p****9z7q#f16c39e3b759    sk-p****9z7q#f16c39e3b759    yes
```

Asserted on the bytes: identifier column equals the page value for every row; no complete key
appears; no full `key_hash` appears; every row has exactly four columns. Rows 3 and 4 are the
colliding pair — distinguishable in the export as well as on screen.

### 7.7 Confirmed unchanged

Verified in this pass and found sound, with the evidence that settles each:

| Claim | Evidence |
|---|---|
| Nothing new is persisted to `request_logs` | Part 5 of the verification test reloads every row and prints stored `key_value`: all `fp:…`. Asserted no mask marker is ever stored |
| `sanitizeRequestLog` and the purge job unaffected | Untouched; `TestPurgeHistoricalKeyValuesRemovesReversibleCredentials` and `TestRecordReplacesSuppliedCredentialWithFingerprint` pass unmodified |
| Complete keys are never diverted into mask resolution | `TestParseKeyIdentifierRejectsCompleteKeysAndOtherIdentifiers` covers 14 malformed inputs including complete keys and malformed discriminators |
| Fingerprint and complete-key search unchanged | Measured: 1 SQL statement, 0 decryptions, `api_keys` never touched (§7.2) |
| Missing `api_keys` degrades instead of failing | `TestResolveKeyIdentifiersFallBackWhenAPIKeysUnavailable`; the three pre-existing handler tests still pass against a DB that migrates `RequestLog` only |
| Short keys never render verbatim | `TestMaskKeyIdentifierNeverRevealsShortKey` |
| One key renders the same on its success and failure rows | Verification test Part 1 compares the 200 row and the 400 row explicitly |

### 7.8 Test suite comparison for this pass

| Suite | Round-1 baseline (§2) | After round 2 | Delta |
|---|---|---|---|
| Go (`go test -count=1 ./...`) | all pass | all pass | no new failures; 14 packages ok |
| Frontend (`vitest run`) | 16 files / 66 tests | 18 files / 77 tests | +2 files, +11 tests (all new) |

Difference set against the original baseline: **empty**. No pre-existing repository test failed, was
modified, skipped, or weakened in either round.

Tests changed in this pass were all written by me in round 1, and they changed because round 1's
display policy was wrong, not to make failures disappear:

- `TestResolveKeyIdentifiersMatchKeyManagementMask` — asserted the identifier *equals* the key
  management mask; now asserts the mask is an exact prefix and that the identifier equals
  `utils.KeyIdentifier`, plus that two keys never share an identifier.
- `TestStreamLogKeysToCSVExportsMaskAndFingerprint` — same adjustment for the export column.
- `TestVerifyLogKeyIdentifierMatchesKeyManagement` — Part 1 compares by prefix; the distinctness
  check now counts identifiers rather than masks.
- `ParseKeyMask` → `ParseKeyIdentifier` and `ResolveKeyMasks` → `ResolveKeyIdentifiers` renames.

Also clean: `go build ./...`, `go vet ./internal/...`, `gofmt -l` on all changed Go files, `eslint .`
(whole project), `prettier --check`, and `vue-tsc --noEmit`.

### 7.9 How to re-run the audit

```bash
cd repos/gpt-load

# Cost: statements and decryptions per page, and per search input kind
GOPROXY=off go test ./internal/handler/ -v \
  -run 'TestLogPageResolutionCostIsBounded|TestLogPageDeduplicatesRepeatedKeyBeforeLookup|TestKeySearchCostByInputKind'

# Collision, key lifecycle, and the misconfiguration probe
GOPROXY=off go test ./internal/services/ -v -run 'TestAudit'

# Export bytes cross-checked against the page
GOPROXY=off go test ./internal/handler/ -v -run TestExportBytesMatchPageDisplay

# Side-by-side key management vs log column
GOPROXY=off go test ./internal/services/ -v -run TestVerifyLogKeyIdentifierMatchesKeyManagement

# Frontend render path (no double masking)
cd web && npx --no-install vitest run src/components/logs/LogTable.test.ts
```

---

## 8. Scale audit of the discriminator

§7.1 introduced the discriminator to stop colliding masks from rendering identically. That fix
answered the mask question but raised its own: the discriminator was four hex characters, so it could
collide too. This section measures that at the scale §7.1 itself argued was ordinary, and measures
what happens when the whole displayed value is pasted into the search box.

Two more real defects were found. Both are fixed.

### 8.1 Defect: a four-character discriminator collided at group scale — FIXED

The discriminator was 4 hex characters = 65,536 values. It only has to separate keys that already
share a mask — but §7.1's own measurement showed a mask group can contain *every key in a group*, so
that is the population it must survive.

`TestKeyIdentifierUniquenessAtGroupScale` builds 5000 keys that all share the head `sk-p` and the
tail `9z7q` (verified: exactly one distinct bare mask), then counts how many end up sharing a full
identifier at three discriminator widths:

```
HEX WIDTH     DISCRIM. SPACE      DISTINCT      KEYS COLLIDING      EXPECTED PAIRS
4             65536               4805          386                 190.697
8             4294967296          5000          0                   0.003
12            281474976710656     5000          0                   0.000
```

**386 of 5000 keys shared an identifier with another key** — ~193 colliding pairs, matching the
birthday prediction of 190.7. So the round-2 fix was insufficient at exactly the scale that motivated
it: it moved the collision from the mask to the discriminator instead of removing it.

**The fix.** The discriminator is now the full fingerprint body — `keyFingerprintLength` (12) hex
characters, the same 12 the `fp:` identifier has always published:

```
sk-p****9z7q#2cbb4afc57aa
sk-p****9z7q#75a3ef835247
```

The point of choosing 12 rather than 8 is not the extra headroom, it is that it removes the need for
a probability argument at all. The identifier now collides *if and only if* the fingerprint collides,
so it inherits the uniqueness of the identifier this system has always used for exact correlation,
and contributes no new collision risk of its own. `TestKeyIdentifierCarriesWholeFingerprintBody`
pins that property directly: the discriminator equals `strings.TrimPrefix(KeyFingerprint(hash),
"fp:")`.

Eight characters would have left a 0.3% residual at 5000 keys and ~1.2% at 10,000. Choosing it would
have meant shipping another "unlikely enough" limitation — the same mistake §7.1 corrected.

**Operational reading.** No, an operator cannot now be misled by a discriminator collision, because
there is no scale at which one occurs before the fingerprint itself collides. Should that ever
happen, both keys would already be indistinguishable under the pre-existing `fp:` identifier too, so
it is not a regression introduced here.

**Cost of the change:** the identifier grew from 17 to 25 characters. The log column was widened
(200→280px, ellipsis 150→230px) so the value is not truncated. Nothing else changed:
`ParseKeyIdentifier` accepts any suffix from 4 to 12 hex characters, so a truncated paste still
narrows the search instead of failing.

### 8.2 Defect: the bare-mask scan silently stopped after one batch — FIXED

Found by running the paste-search test at 2000 keys. Searching the bare mask returned **500 rows** —
exactly `chunkSize`.

Cause: `resolveMaskedKeyHashes` iterates `api_keys` with `FindInBatches`, which paginates by primary
key. The query selected only `key_hash` and `key_value`, so GORM had no primary key to page on and
the iteration stopped after the first batch. Every bare-mask search therefore considered only the
first 500 keys and silently under-reported.

Round 2's measurement used 400 keys — under the batch size — so it could not see this. That is the
concrete reason this round's instruction to test at real scale mattered.

Fix: select the primary key as well (`Select("id", "key_hash", "key_value")`).
`TestAuditBareMaskScanReachesBeyondFirstBatch` is the regression test, deliberately sized at 600 keys
— above `chunkSize` (500) and below `maskSearchKeyLimit` (1000) — so a truncated scan shows up as a
wrong count rather than being masked by the cap:

```
Bare-mask scan across 600 same-mask keys (chunkSize=500): matched 600 rows
```

### 8.3 Pasting the whole displayed value into the search box

`TestSearchByPastedIdentifierAtScale` drives `POST /logs/search` — the endpoint the frontend actually
uses (`web/src/api/logs.ts:7-9`) — with 2000 keys that all share one bare mask. The displayed value
is read back *from the API* rather than recomputed, so the input is exactly what an operator would
copy.

```
POST /logs/search with 2000 keys that all share the bare mask "sk-p****9z7q"
displayed column value for the target row: sk-p****9z7q#e2b0d7d72d76

SEARCH INPUT (as pasted)        KIND                      ROWS     MATCHED ROW
sk-p****9z7q#e2b0d7d72d76       pasted column value       1        paste-1000
 sk-p****9z7q#e2b0d7d72d76      pasted with whitespace    1        paste-1000
sk-p****9z7q                    bare mask only (capped)   1000     paste-0921
fp:e2b0d7d72d76                 fingerprint               1        paste-1000
sk-p00000000010009z7q           complete key              1        paste-1000
```

**The pasted value works and is exact**: one row out of 2000 sharing the mask, and it is the right
row. Leading and trailing whitespace is tolerated, which matters because copying from a table often
picks it up.

Supporting checks:

- The frontend passes the pasted string through unmodified. `LogTable.test.ts` sets the value on the
  key-search input, triggers the search, and asserts the exact string — `#` suffix included — arrives
  at `logApi.getLogs`. Without that suffix the search would silently widen to every key with the same
  mask, so it is asserted explicitly rather than assumed.
- The bare mask is capped at `maskSearchKeyLimit` (1000 of 2000 here) and logs a warning. This is the
  designed bound on the `IN` clause, it applies only to a form the column no longer shows, and it is
  reported rather than silent.
- `GET /logs?key_value=<identifier>` returns **400**, and the rejection does not echo the value.
  That endpoint accepts fingerprints only, by design — which is the right behaviour here for a reason
  worth recording: `#` starts a URL fragment, so an identifier in a query string would arrive with
  the discriminator stripped and would silently widen to the bare mask. Rejecting it avoids that
  class of bug entirely. The UI is unaffected because it searches over POST.

### 8.4 Test suite comparison for this pass

| Suite | Round-1 baseline (§2) | After round 3 | Delta |
|---|---|---|---|
| Go (`go test -count=1 ./...`) | all pass | all pass | no new failures; 14 packages ok |
| Frontend (`vitest run`) | 16 files / 66 tests | 18 files / 78 tests | +2 files, +12 tests (all new) |

Difference set against the original baseline: **empty**. No pre-existing repository test failed, was
modified, skipped, or weakened in any of the three passes.

Tests adjusted in this pass were all mine, and all because the discriminator width changed — they
assert against `utils.KeyIdentifier` rather than hard-coded strings, so most needed no change at all:

- `TestParseKeyIdentifierAcceptsDiscriminatedIdentifier` — now uses a full-width suffix.
- `TestParseKeyIdentifierRejectsCompleteKeysAndOtherIdentifiers` — "too long" case updated, since a
  5-character suffix is now legitimately accepted as a truncated prefix.
- `TestParseKeyIdentifierAcceptsTruncatedDiscriminator` — new, covering the 4..12 range.
- `LogTable.test.ts` fixtures use full-width identifiers.

Also clean: `go build ./...`, `go vet ./internal/...`, `gofmt -l` on all changed Go files,
`eslint .`, `prettier --check src/`, and `vue-tsc --noEmit`.

### 8.5 Re-running the scale audit

```bash
cd repos/gpt-load

# Discriminator uniqueness at 5000 same-mask keys, across widths
GOPROXY=off go test ./internal/utils/ -v -run 'TestKeyIdentifier'

# Pasted-identifier search at 2000 same-mask keys, through POST /logs/search
GOPROXY=off go test ./internal/handler/ -v -run TestSearchByPastedIdentifierAtScale

# Bare-mask scan across the batch boundary
GOPROXY=off go test ./internal/services/ -v -run TestAuditBareMaskScanReachesBeyondFirstBatch
```


