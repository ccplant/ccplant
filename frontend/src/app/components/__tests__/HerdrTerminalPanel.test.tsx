import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import type { AgentAPIProxyClient } from '../../../lib/agentapi-proxy-client'
import HerdrTerminalPanel from '../HerdrTerminalPanel'

function createClient(terminals: Array<{ address: string; name: string; state: string }>) {
  return {
    listHerdrTerminals: vi.fn().mockResolvedValue(terminals),
    readHerdrTerminal: vi.fn().mockResolvedValue('agent output'),
  } as unknown as AgentAPIProxyClient
}

describe('HerdrTerminalPanel', () => {
  it('renders nothing when there are no Herdr agents', async () => {
    const { container } = render(
      <HerdrTerminalPanel client={createClient([])} sessionId="session-1" selectedTerminal="" onSelectTerminal={vi.fn()} />,
    )

    await waitFor(() => expect(container).toBeEmptyDOMElement())
  })

  it('selects an agent and shows its output without adding another composer', async () => {
    const client = createClient([{ address: 'term-1', name: 'researcher', state: 'running' }])
    const onSelect = vi.fn()
    const { rerender } = render(
      <HerdrTerminalPanel client={client} sessionId="session-1" selectedTerminal="" onSelectTerminal={onSelect} />,
    )

    const agent = await screen.findByRole('button', { name: /researcher/ })
    fireEvent.click(agent)
    expect(onSelect).toHaveBeenCalledWith('term-1')
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()

    rerender(<HerdrTerminalPanel client={client} sessionId="session-1" selectedTerminal="term-1" onSelectTerminal={onSelect} />)
    expect(await screen.findByText('agent output')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /researcher/ }))
    expect(onSelect).toHaveBeenLastCalledWith('')
  })
})
