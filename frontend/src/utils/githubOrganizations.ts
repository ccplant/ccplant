export function parseGitHubOrganizations(value: string): string[] {
  return value.split(',').map((organization) => organization.trim()).filter(Boolean)
}
