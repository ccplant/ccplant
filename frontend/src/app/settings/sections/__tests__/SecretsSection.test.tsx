import { describe, expect, it } from 'vitest'
import { buildSecretPayload, type SecretEntry } from '../SecretsSection'

describe('buildSecretPayload', () => {
  it('maps environment, file, and arbitrary key/value usages to the API payload', () => {
    const entries: SecretEntry[] = [
      { type: 'env', key: '', target: ' API_TOKEN ', value: 'env-value', permissions: '0600' },
      { type: 'file', key: '', target: ' /tmp/credential ', value: 'file-value', permissions: '0400' },
      { type: 'kv', key: ' bot-token ', target: '', value: 'kv-value', permissions: '0600' },
    ]

    expect(buildSecretPayload(' Runtime secrets ', entries)).toEqual({
      name: 'Runtime secrets',
      values: { API_TOKEN: 'env-value', '/tmp/credential': 'file-value', 'bot-token': 'kv-value' },
      projections: [
        { key: 'API_TOKEN', type: 'env', env_name: 'API_TOKEN' },
        { key: '/tmp/credential', type: 'file', path: '/tmp/credential', permissions: '0400' },
      ],
    })
  })
})
