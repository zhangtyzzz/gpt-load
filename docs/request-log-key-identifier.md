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

```
1. mask of the live key        e.g. "sk-a****mnop"   (key_hash resolves to an api_keys row)
2. fingerprint                 e.g. "fp:6f9ef8c7ce8d" (key_hash present, but no live key)
3. ""                          (no key_hash at all — row logged without a selected key)
```

Case 2 is the honest answer for a row whose key has since been deleted, or whose `key_hash`
predates an `ENCRYPTION_KEY` rotation (§1.9): the key it points at is not in Keys management any
more, so there is no Keys-management row to be "consistent" with. Showing a mask would be a
fabrication; showing the fingerprint preserves the one thing that is still true — rows sharing a
fingerprint used the same key. The UI labels this case so it is not mistaken for a bug (§3.6).

### 3.4 Masks are not unique — mitigation

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

### 3.5 Search: keeping the copy-and-paste path working

The reporter's workflow is literally "copy the column, paste it into the box". After this change the
column contains a mask, and a mask reaches the `else` branch of §1.6, is hashed, and matches
**nothing** — a silent empty result. That is a regression and must be handled, not documented away.

`logFiltersScope` becomes a three-way decision:

```
input looks like "AAAA****ZZZZ"  -> resolve to api_keys with that prefix/suffix,
                                    then key_hash IN (their hashes); no match -> match nothing
input looks like "fp:<12 hex>"   -> key_hash LIKE prefix%          (unchanged)
otherwise                        -> key_hash = Hash(input)         (unchanged, complete key)
```

Mask detection is deliberately strict — exactly 12 characters with `****` at offsets 4..8 — so it
cannot swallow a complete key. A real key of exactly that shape would be pathological; the risk of
mis-routing one is far smaller than the certainty of breaking copy-and-paste.

Mask resolution scans `api_keys` in batches of `chunkSize` (500, the existing constant at
`internal/services/key_service.go:20`), decrypts each key, and keeps hashes whose plaintext matches
the prefix and suffix. Matches are capped (1000 keys) so a degenerate mask cannot build an unbounded
`IN` clause; the cap is logged when hit rather than silently truncating.

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

- `internal/utils/string_utils.go` — add `MaskKeyIdentifier` (never reveals short keys).
- `internal/utils/redaction_utils.go` — add `ParseKeyMask` (strict `AAAA****ZZZZ` shape).
- `internal/services/log_service.go` — add `ResolveKeyIdentifiers` (batched `key_hash` → mask) and
  `resolveMaskedKeyHashes`; wire mask branch into `logFiltersScope`; export gains
  `key_fingerprint`.
- `internal/handler/log_handler.go` — `LogResponse.key_fingerprint`; resolve masks per page.

Frontend:

- `web/src/utils/display.ts` — `maskKey` uses `****` and masks short keys.
- `web/src/types/models.ts` — `RequestLog.key_fingerprint?`.
- `web/src/components/logs/LogTable.vue` — render mask, show fingerprint in detail modal.
- `web/src/locales/{zh-CN,en-US,ja-JP}.ts` — reworded placeholder + `keyFingerprint`,
  `copyKeyFingerprint`.

Tests and verification:

- `internal/utils/string_utils_test.go`, `internal/utils/redaction_utils_test.go`
- `internal/services/log_service_test.go`, `internal/handler/log_handler_test.go`
- `web/src/utils/display.test.ts`
- `internal/services/keyidentifier_verification_test.go` — runnable side-by-side proof (§6).

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
has been deleted. Actual output:

```
=========================================================================
 PART 1  Key management  vs  request-log identifier
=========================================================================
LOG ROW           KEY MGMT SHOWS  LOG COLUMN      MATCH?
-------------------------------------------------------------------------
log-alpha-bad     sk-p****7d4e    sk-p****7d4e    yes
log-alpha-ok      sk-p****7d4e    sk-p****7d4e    yes
log-bravo-bad     sk-p****6a2b    sk-p****6a2b    yes
log-charlie-ok    sk-a****4b81    sk-a****4b81    yes

3 distinct keys -> 3 distinct identifiers (each key is individually recognizable)

One key, two outcomes:  200 row -> "sk-p****7d4e"   400 row -> "sk-p****7d4e"

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
SEARCH INPUT                        KIND                        ROWS
----------------------------------------------------------------------
sk-p****7d4e                        mask (copied column)        2
sk-proj-alpha-9f3c2b1a7d4e          complete key                2
fp:b91b0e612994                     fingerprint                 2
fp:7b1e4a90c3d2                     fingerprint (deleted key)   1
zzzz****zzzz                        mask matching no key        0

=========================================================================
 PART 4  CSV export
=========================================================================
key_identifier,key_fingerprint,group_name,status_code
sk-p****6a2b,fp:55c1f6292cf4,primary,400
sk-a****4b81,fp:77ec4dce8c08,primary,200
fp:7b1e4a90c3d2,fp:7b1e4a90c3d2,primary,401
sk-p****7d4e,fp:b91b0e612994,primary,200

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

Note what Part 1 also demonstrates about the ambiguity discussed in §3.4: `sk-proj-alpha-…` and
`sk-proj-bravo-…` both mask to a `sk-p` head and are told apart only by their tails. Distinct here,
but that is the margin — which is exactly why `key_fingerprint` is retained.

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

