export interface ControllerRuntimeStatus {
  status: string
  message?: string
}

interface ControllerRESTClient {
  getACPSessionInfo: (sessionId: string) => Promise<{ sessionId: string } | null>
  sendACPPrompt: (sessionId: string, acpSessionId: string, prompt: Array<{ type: 'text'; text: string }>, promptId: number) => Promise<void>
  sendSessionMessage: (sessionId: string, message: { content: string; type: 'user' }) => Promise<unknown>
}

interface ControllerACPServerClient {
  initialize: () => Promise<unknown>
  sendPrompt: (sessionId: string, prompt: Array<{ type: 'text'; text: string }>) => Promise<void>
}

export async function sendControllerCommand(options: {
  sessionId: string
  message: string
  globalACPEnabled: boolean
  restClient: ControllerRESTClient
  acpServerClient?: ControllerACPServerClient
  promptId?: number
}): Promise<void> {
  const prompt = [{ type: 'text' as const, text: options.message }]
  if (options.globalACPEnabled) {
    if (!options.acpServerClient) throw new Error('ACP Server clientが利用できません')
    await options.acpServerClient.initialize()
    await options.acpServerClient.sendPrompt(options.sessionId, prompt)
    return
  }

  const acpInfo = await options.restClient.getACPSessionInfo(options.sessionId)
  if (acpInfo) {
    await options.restClient.sendACPPrompt(options.sessionId, acpInfo.sessionId, prompt, options.promptId ?? Date.now())
    return
  }
  await options.restClient.sendSessionMessage(options.sessionId, { content: options.message, type: 'user' })
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
