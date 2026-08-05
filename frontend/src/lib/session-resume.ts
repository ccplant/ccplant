type ResumeResult = { session_id: string; status: string }

export interface SessionResumeClient {
  resumeSession(sessionId: string): Promise<ResumeResult>
}

export interface SessionMessagesClient {
  getSessionMessages(sessionId: string, params?: { limit?: number; direction?: 'head' | 'tail' }): Promise<unknown>
}

interface ResumeOptions {
  intervalMs?: number
  maxAttempts?: number
  wait?: (milliseconds: number) => Promise<void>
}

export async function waitForSessionResume(
  client: SessionResumeClient,
  sessionId: string,
  options: ResumeOptions = {},
): Promise<ResumeResult> {
  const intervalMs = options.intervalMs ?? 1000
  const maxAttempts = options.maxAttempts ?? 120
  const wait = options.wait ?? ((milliseconds: number) => new Promise(resolve => setTimeout(resolve, milliseconds)))

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    const result = await client.resumeSession(sessionId)
    if (result.status !== 'restoring') return result
    if (attempt + 1 < maxAttempts) await wait(intervalMs)
  }

  throw new Error('Timed out waiting for the session workload to resume')
}

export async function waitForSessionMessages(
  client: SessionMessagesClient,
  sessionId: string,
  options: ResumeOptions = {},
): Promise<unknown> {
  const intervalMs = options.intervalMs ?? 1000
  const maxAttempts = options.maxAttempts ?? 120
  const wait = options.wait ?? ((milliseconds: number) => new Promise(resolve => setTimeout(resolve, milliseconds)))

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    try {
      return await client.getSessionMessages(sessionId, { limit: 50, direction: 'tail' })
    } catch (error) {
      const status = typeof error === 'object' && error !== null && 'status' in error
        ? Number(error.status)
        : undefined
      if (status !== undefined && ![404, 502, 503].includes(status)) throw error
      if (attempt + 1 >= maxAttempts) break
      await wait(intervalMs)
    }
  }

  throw new Error('Timed out waiting for session messages after resume')
}
