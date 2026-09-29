# Scoped branch-key encryption

The `aws-kms-branch-scoped` KV encryption provider amortizes AWS KMS calls while
limiting each plaintext branch key to one ownership scope. It is available only
with the `libsql-encrypted` backend.

```yaml
api:
  kvStore:
    backend: ""
    primary:
      backend: libsql-encrypted
      databaseUrlSecretRef:
        name: agentapi-kv
        key: database-url
      encryption:
        provider: aws-kms-branch-scoped
        activeKeyId: primary
        kmsRegion: ap-northeast-1
        kmsKeys:
          primary: arn:aws:kms:ap-northeast-1:123456789012:key/example
        branchCacheTTLSeconds: 900
        branchCacheMaxEntries: 128
```

The scope resolver prefers existing team and user ownership labels. It hashes
the selected label and value before storing the scope in the branch-key table or
KMS encryption context. Records without an ownership label share a namespace
scope. Each record still receives a unique data-encryption key.

## Compatibility and rollout

Readers accept direct-KMS values and branch formats v1, v2, and v3. The scoped
provider writes v3; the existing `aws-kms-branch` provider continues to write v2.

Use an expand/contract rollout:

1. Deploy the version that understands v3 while retaining the old provider.
2. Change the provider to `aws-kms-branch-scoped` after every reader is updated.
3. Run the key rotation command during a writer maintenance window to rewrap
   legacy data-encryption keys. Value ciphertext and nonces are not changed.
4. Verify no legacy envelopes remain before retiring legacy branch material.

Do not roll back to a binary that predates v3 support after enabling v3 writes.
The cache TTL controls how long plaintext branch keys remain in process memory;
it does not revoke a branch key that an attacker has already copied.
