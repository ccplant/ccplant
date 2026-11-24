import type { ProfileEditorState } from './profileEditorState'

export function buildProfileDocker(state: ProfileEditorState) {
  const { dockerEnabled, dockerRegistries } = state
  // Build docker config if enabled
  let dockerConfig:
    | {
        enabled: boolean
        registries?: {
          server?: string
          username?: string
          password?: string
          secret_name?: string
          insecure?: boolean
        }[]
      }
    | undefined
  if (dockerEnabled) {
    const regs = dockerRegistries
      .filter((r) => r.server || r.username || r.secretName)
      .map((r) => ({
        ...(r.server ? { server: r.server } : {}),
        ...(r.secretName ? { secret_name: r.secretName } : {}),
        ...(r.username && !r.secretName ? { username: r.username, password: r.password } : {}),
        ...(r.insecure ? { insecure: true } : {}),
      }))
    dockerConfig = { enabled: true, ...(regs.length > 0 ? { registries: regs } : {}) }
  }

  return dockerConfig
}
