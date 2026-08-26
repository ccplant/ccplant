import { getAuthSessionFromCookie } from './cookie-auth'
import { getBackendUrl } from './server-backend-url'
import type { ProxyUserInfo } from '@/types/user'

export async function getAuthenticatedRouteOwner(): Promise<string | null> {
  const authSession = await getAuthSessionFromCookie()
  if (!authSession) return null

  try {
    // Route ownership is always verified by the default trusted backend, not by
    // the user-configured destination currently serving the request hostname.
    const response = await fetch(getBackendUrl('/user/info'), {
      headers: {
        Authorization: `Bearer ${authSession.access_token}`,
        Accept: 'application/json',
      },
      cache: 'no-store',
      signal: AbortSignal.timeout(10000),
    })
    if (!response.ok) return null
    const user = await response.json() as ProxyUserInfo
    return user.username || null
  } catch {
    return null
  }
}

