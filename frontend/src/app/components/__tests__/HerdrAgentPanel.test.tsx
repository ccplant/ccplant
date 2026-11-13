import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import type { AgentAPIProxyClient } from '../../../lib/agentapi-proxy-client';
import HerdrAgentPanel from '../HerdrAgentPanel';

const agent = { server_id: 'server', server_name: 'issue-1', runtime: 'herdr', pane_id: 'w1:p1', terminal_id: 'term_1', agent: 'claude', status: 'idle', label: 'implementer', generation: 'one', can_input: true };
function setup(status = 'idle') {
  const client = {
    listHerdrAgents: vi.fn().mockResolvedValue({ agents: [{ ...agent, status, can_input: status === 'idle' }], unavailable_servers: 0 }),
    readHerdrOutput: vi.fn().mockResolvedValue({ text: '<script>screen text</script>' }),
    sendHerdrInput: vi.fn().mockResolvedValue({ sent: true }),
  };
  render(<HerdrAgentPanel sessionId="session" client={client as unknown as AgentAPIProxyClient} />);
  return client;
}
async function select() {
  fireEvent.click(screen.getByRole('button', { name: 'Pod内エージェント' }));
  await screen.findByRole('option', { name: /implementer/ });
  fireEvent.change(screen.getByLabelText('エージェントを選択'), { target: { value: 'server/w1:p1/one' } });
  await screen.findByText('<script>screen text</script>');
}
describe('HerdrAgentPanel', () => {
  it('does not read until opened and selected, escapes output, requires confirmation, and sends only once', async () => {
    const client = setup();
    expect(client.listHerdrAgents).not.toHaveBeenCalled();
    await select();
    expect(document.querySelector('script')).toBeNull();
    fireEvent.change(screen.getByLabelText('エージェントへの入力'), { target: { value: 'hello' } });
    const send = screen.getByRole('button', { name: '選択したエージェントに送信' });
    expect(send).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox'));
    await act(async () => { fireEvent.click(send); fireEvent.click(send); });
    expect(client.sendHerdrInput).toHaveBeenCalledTimes(1);
    expect(client.sendHerdrInput).toHaveBeenCalledWith('session', { server_id: 'server', pane_id: 'w1:p1', generation: 'one' }, 'hello');
  });
  it('refuses input to a working agent', async () => {
    setup('working'); await select();
    expect(screen.getByLabelText('エージェントへの入力')).toBeDisabled();
  });
  it('keeps uncertain delivery visible and never automatically retries', async () => {
    const client = setup(); client.sendHerdrInput.mockRejectedValue(new Error('timeout'));
    await select();
    fireEvent.click(screen.getByRole('checkbox'));
    fireEvent.change(screen.getByLabelText('エージェントへの入力'), { target: { value: 'hello' } });
    fireEvent.click(screen.getByRole('button', { name: '選択したエージェントに送信' }));
    await screen.findByText(/自動再送はしません/);
    expect(client.sendHerdrInput).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('checkbox')).not.toBeChecked();
    expect(screen.getByLabelText('エージェントへの入力')).toHaveValue('hello');
  });
  it('reports unsupported runtimes instead of displaying stale agents', async () => {
    const client = setup(); client.listHerdrAgents.mockRejectedValue(new Error('404'));
    fireEvent.click(screen.getByRole('button', { name: 'Pod内エージェント' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('AGENTAPI_HERDR_ENABLED=1'));
    expect(client.readHerdrOutput).not.toHaveBeenCalled();
  });
});