export function parseGitHubOrganizations(value: string): string[] {
  return value.split(/[\s,]+/).map((organization) => organization.trim()).filter(Boolean)
}
