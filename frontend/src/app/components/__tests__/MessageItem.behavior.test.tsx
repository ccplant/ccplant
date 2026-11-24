import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import type { SessionMessage } from '../../../types/agentapi';
import MessageItem from '../MessageItem';

const presentation = { formatTimestamp: () => '00:00', fontSettings: { fontSize: 14, fontFamily: 'sans-serif' } };
const message = (changes: Partial<SessionMessage> = {}): SessionMessage => ({
  id: 1,
  role: 'agent',
  content: 'answer',
  time: '2026-11-23T00:00:00Z',
  type: 'normal',
  ...changes,
});

describe('message rendering behavior', () => {
  beforeEach(cleanup);
  afterEach(cleanup);

  it('preserves thought expansion when a streamed answer changes', () => {
    const value = message({ thought: 'thinking details' });
    const view = render(<MessageItem {...presentation} message={value} />);
    expect(screen.queryByText('thinking details')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /思考を表示/ }));
    expect(screen.getByText('thinking details')).toBeInTheDocument();
    view.rerender(<MessageItem {...presentation} message={{ ...value, content: 'updated answer' }} />);
    expect(screen.getByRole('button', { name: /思考を隠す/ })).toBeInTheDocument();
    expect(screen.getByText('thinking details')).toBeInTheDocument();
  });

  it('renders markdown only when the caller enables it', () => {
    const value = message({ content: '**bold answer**' });
    const view = render(<MessageItem {...presentation} message={value} isClaudeAgent />);
    expect(screen.getByText('bold answer').tagName).toBe('STRONG');
    view.rerender(<MessageItem {...presentation} message={value} isClaudeAgent={false} />);
    expect(screen.getByText('**bold answer**')).toBeInTheDocument();
  });

  it('falls back to normal rendering for malformed tool content', () => {
    render(
      <MessageItem {...presentation} message={message({ content: 'invalid tool payload', toolUseId: 'tool-1' })} />
    );
    expect(screen.getByText('invalid tool payload')).toBeInTheDocument();
    expect(screen.getByText('AgentAPI')).toBeInTheDocument();
  });

  it('expands and collapses standalone tool results', () => {
    const content = 'Long result line. '.repeat(8);
    render(<MessageItem {...presentation} message={message({ role: 'tool_result', content, status: 'success' })} />);
    expect(screen.queryByText(content.trim())).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button'));
    expect(screen.getByText(content.trim())).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button'));
    expect(screen.queryByText(content.trim())).not.toBeInTheDocument();
  });
});
