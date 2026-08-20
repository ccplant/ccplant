# KV value encryption design

## Decision

Encrypt every application KV document at the `kvstore.Store` boundary. Keep only
the metadata required to address, filter, migrate, and rotate a record in
plaintext. In particular, `Secret.Data`, `Secret.StringData`,
`ConfigMap.Data`, `ConfigMap.BinaryData`, annotations, and the rest of the
serialized Kubernetes object are ciphertext.

This is application-level encryption. Database encryption at rest and
Kubernetes encryption at rest are still recommended as independent controls.

## Why metadata and value are separated

The current libSQL row contains `kind`, `namespace`, `key`, `version`, and the
complete Kubernetes JSON object in `value`. The Kubernetes adapter implements a
label selector by loading every row, decoding `value`, and testing the object's
labels. Encrypting that column without another change would therefore make
filtering require decrypting every document.

The new storage-neutral record is conceptually:

```go
type Record struct {
    Kind       Kind
    Namespace  string
    Key        string
    Labels     map[string]string // plaintext, searchable
    Value      []byte            // plaintext above the encryption decorator
    Version    int64
}

type Query struct {
    Kind          Kind
    Namespace     string
    LabelSelector string
}
```

`kind`, `namespace`, `key`, `version`, and labels are plaintext. They already
describe the existence, ownership, and purpose of many resources, so callers
must not put credentials, tokens, email addresses, or other secrets in them.
Annotations are not searchable today and remain inside the encrypted value.

Labels should eventually be restricted to a documented allowlist for each
resource family. Until that is practical, retaining all existing labels
preserves selector compatibility but must be treated as metadata disclosure in
the threat model.

## Storage layers

Use a decorator so repositories and the Kubernetes compatibility adapter keep
working with plaintext documents:

```text
repository / Kubernetes adapter
            |
            | plaintext Record
            v
EncryptedStore (encrypt on Create/Update, decrypt on Get/List)
            |
            | metadata + encrypted envelope
            v
libSQL or another physical Store
```

For the Kubernetes physical backend, there are two supported deployment modes:

- `application` encryption uses the decorator and stores an encrypted envelope
  in an application-owned Secret. This gives the same application-level
  protection and key rotation behavior as libSQL.
- `provider` encryption delegates to Kubernetes encryption at rest and stores
  the normal object. This is retained only as an explicit compatibility mode;
  it does not protect values from Kubernetes API readers.

Encryption must be fail-closed. If encryption is configured, startup fails when
the active key cannot be loaded. A missing key, unknown envelope version, or
authentication failure never falls back to plaintext.

## Cryptographic envelope

Use envelope encryption rather than calling AWS KMS for the whole document:

1. Generate a random 256-bit data-encryption key (DEK) for each write.
2. Encrypt the serialized document using AES-256-GCM with a fresh 96-bit nonce.
3. Wrap the DEK with the configured key-encryption key (KEK), such as AWS KMS,
   or derive/wrap it with the configured local master key.
4. Store a versioned, self-describing envelope in `value`.

```json
{
  "format": "agentapi-kv-envelope/v1",
  "algorithm": "AES-256-GCM",
  "key_id": "kms-key-arn-or-local-fingerprint",
  "wrapped_dek": "base64...",
  "nonce": "base64...",
  "ciphertext": "base64..."
}
```

The GCM additional authenticated data (AAD) is a canonical encoding of:

```text
format || kind || namespace || key || version || canonical(labels)
```

This prevents moving ciphertext to a different record, changing its version,
or changing searchable labels without detection. The new optimistic version
must be known before encryption; therefore the physical store API must assign
the next version transactionally or accept an expected and next version. Do not
encrypt with the old version and then increment it afterward.

The existing `EncryptionService` is not sufficient for this layer: it accepts
strings, has no AAD parameter, and the KMS implementation encrypts the complete
plaintext directly. Introduce a KV-specific byte-oriented envelope interface,
for example `Seal(ctx, plaintext, aad)` and `Open(ctx, envelope, aad)`. Keep the
existing service for settings compatibility until it can be migrated
separately.

Cache only unwrapped DEKs, keyed by the digest of `wrapped_dek`, with a small
bounded size and short TTL. Never persist or log a plaintext DEK. Zero key
buffers where the Go implementation makes that meaningful.

## libSQL schema and filtering

Evolve the table without replacing the primary key:

```sql
ALTER TABLE agentapi_kv ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}';
ALTER TABLE agentapi_kv ADD COLUMN value_format TEXT NOT NULL DEFAULT 'plaintext-v1';
```

`metadata` contains a versioned canonical JSON object with labels only:

```json
{"format":"agentapi-kv-metadata/v1","labels":{"scope":"user"}}
```

Initially, evaluate Kubernetes label selectors against `metadata` in the store
after reading candidate rows by kind and namespace. This avoids decryption and
is behaviorally compatible, while keeping the migration simple. If cardinality
makes that scan expensive, add a normalized label index later:

```sql
CREATE TABLE agentapi_kv_labels (
  kind TEXT NOT NULL,
  namespace TEXT NOT NULL,
  key TEXT NOT NULL,
  label_key TEXT NOT NULL,
  label_value TEXT NOT NULL,
  PRIMARY KEY (kind, namespace, key, label_key)
);
```

Create/update/delete of a row and its label rows must share one transaction.
Do not add hashes of arbitrary value fields as a general search mechanism:
low-entropy values are vulnerable to dictionary attacks and range, prefix, and
substring searches still do not work. If a future use case requires equality
lookup on a sensitive field, add a deliberately scoped blind index using a
separate HMAC key and document its leakage.

`List` applies the selector before decrypting and returns decrypted values only
for matching rows. Callers that only need names should use a new metadata-only
list method so they never receive plaintext unnecessarily. Pagination must also
be introduced before large collections are expected.

## Key management and rotation

- Production should use a KMS-backed KEK. Grant encrypt/wrap to writers and
  decrypt/unwrap only to processes that read values.
- Local development may use a 32-byte base64 master key from a mounted Secret.
  The key must not appear in config files, Helm values, logs, or API responses.
- `key_id` selects the decrypting key. Configuration contains one active key for
  writes and a keyring of previous keys for reads.
- Online rotation first deploys the new keyring, changes the active key, and
  then rewraps DEKs in batches. Rewrapping does not re-encrypt document data and
  must use optimistic version checks.
- A key cannot be removed from the keyring until a scan reports zero envelopes
  using it. Expose counts by envelope format and key ID, never record names.

For local AES KEKs, rotation requires unwrapping with the old master key and
wrapping with the new one. KMS rotation of key material behind an unchanged KMS
key ID needs no row rewrite; changing KMS keys does.

## Migration and rollout

Use a mixed-reader, single-writer-format rollout:

1. Add schema columns and deploy readers that understand both `plaintext-v1`
   and `agentapi-kv-envelope/v1`; writes remain plaintext.
2. Backfill `metadata` from plaintext documents. Validate that stored labels
   match decoded object labels.
3. Configure the keyring and switch new writes to encrypted envelopes.
4. Encrypt existing rows in bounded, resumable batches using optimistic version
   checks. A concurrent change is skipped and retried, never overwritten.
5. Verify every row is decryptable and its authenticated metadata matches.
6. Disable plaintext reads. A later release may reject or remove
   `plaintext-v1` entirely.

The migration command must support dry-run, progress counters, restart from a
cursor, a rate limit, and per-row error reporting without printing plaintext.
The existing primary/secondary KV migration and verification commands must
compare logical plaintext documents, not ciphertext, because randomized
encryption intentionally produces different bytes for the same value.

During rollback, binaries must retain the new decryptor and keyring. Rolling
back to a version that only understands plaintext after step 3 is unsafe.

## Failure behavior and observability

- `Get`: return a typed decryption error; never return a partial object.
- `List`: fail the request if any selected record cannot be decrypted. Silently
  omitting it creates incorrect authorization and reconciliation behavior.
- Replication: encrypt once above `ReplicatedStore` so both stores receive the
  same envelope, or compare plaintext at that layer. Do not independently
  encrypt each replica and then compare raw bytes.
- Backups contain ciphertext plus disclosure-prone metadata. Back up the KEK
  recovery procedure separately; loss of all decrypting keys is data loss.
- Metrics may include operation, result, envelope format, and key ID. Logs and
  traces must never contain plaintext, ciphertext, wrapped DEKs, nonces, or full
  metadata.

## Security properties and non-goals

This design protects document contents if the database or a backup is exposed.
It detects ciphertext and plaintext-metadata substitution when a value is read.
It does not hide record count, kind, namespace, key, labels, sizes, access
patterns, or update timing. It also does not protect plaintext after a process
with decrypt permission reads it, and it does not replace authorization,
transport encryption, database access control, or backup policy.

## Acceptance criteria

- No application document value is present in plaintext in libSQL after the
  migration completes.
- Exact existing Kubernetes label-selector behavior is preserved.
- Nonmatching rows in a filtered list cause no decrypt or KMS operation.
- Swapping envelopes or modifying identity, version, or labels causes GCM
  authentication failure.
- New and old keys can decrypt concurrently, and rotation can resume safely.
- Missing keys and corrupt envelopes fail closed without leaking record data.
- Migration, replication verification, backup restore, and rollback paths have
  integration tests.
