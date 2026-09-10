import { describe, expect, it } from 'vitest'
import { parseGitHubOrganizations } from '../githubOrganizations'

describe('parseGitHubOrganizations', () => {
  it('parses multiple comma-separated organizations', () => {
    expect(parseGitHubOrganizations('example-org, another-org')).toEqual([
      'example-org',
      'another-org',
    ])
  })

  it('ignores whitespace and empty entries', () => {
    expect(parseGitHubOrganizations(' example-org, , another-org,')).toEqual([
      'example-org',
      'another-org',
    ])
  })
})
