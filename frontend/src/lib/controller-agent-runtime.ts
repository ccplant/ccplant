export interface ControllerRuntimeStatus {
  status: string
  message?: string
}

interface WaitForControllerOptions {
  timeoutMs?: number
  pollIntervalMs?: number
  delay?: (milliseconds: number) => Promise<void>
}

export async function waitForControllerReady(
  getStatus: () => Promise<ControllerRuntimeStatus>,
  options: WaitForControllerOptions = {},
): Promise<void> {
  const timeoutMs = options.timeoutMs ?? 90_000
  const pollIntervalMs = options.pollIntervalMs ?? 2_000
  const delay = options.delay ?? ((milliseconds) => new Promise((resolve) => window.setTimeout(resolve, milliseconds)))
  const deadline = Date.now() + timeoutMs

  while (Date.now() < deadline) {
    const status = await getStatus()
    if (status.status === 'stable') return
    if (status.status === 'error') {
      throw new Error(status.message || 'Controller Sessionでエラーが発生しました')
    }
    await delay(pollIntervalMs)
  }

  throw new Error('Controller Sessionが処理中です。少し待ってから再度お試しください。')
}
