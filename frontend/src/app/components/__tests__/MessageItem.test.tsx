import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import MessageItem from '../MessageItem';

const mermaidRender = vi.fn();
const mermaidInitialize = vi.fn();

vi.mock('mermaid', () => ({
  default: {
    initialize: mermaidInitialize,
    render: mermaidRender,
  },
}));

describe('MessageItem ACP images', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders an image-only agent message in the chat', () => {
    render(
      <MessageItem
        message={{
          id: 1,
          role: 'agent',
          content: '',
          images: [{ mimeType: 'image/png', data: 'iVBORw0KGgo=' }],
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
      />,
    );

    expect(screen.getByRole('img', { name: 'Message image 1' })).toHaveAttribute(
      'src',
      'data:image/png;base64,iVBORw0KGgo=',
    );
  });

  it('renders an image returned by an agent tool', () => {
    render(
      <MessageItem
        message={{
          id: 1,
          role: 'agent',
          content: JSON.stringify({
            type: 'tool_use',
            name: 'GenerateImage',
            id: 'image-tool-1',
            input: {},
          }),
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
          toolUseId: 'image-tool-1',
        }}
        toolResult={{
          id: 2,
          role: 'tool_result',
          content: '',
          images: [{ mimeType: 'image/png', data: 'generated-image-data' }],
          time: '2026-07-25T00:00:01Z',
          type: 'normal',
          parentToolUseId: 'image-tool-1',
          status: 'success',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
      />,
    );

    expect(screen.getByRole('img', { name: 'Agent output image 1' })).toHaveAttribute(
      'src',
      'data:image/png;base64,generated-image-data',
    );
  });

  it('copies an agent output image to the clipboard', async () => {
    const write = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { write },
    });
    vi.stubGlobal('ClipboardItem', class {
      constructor(public items: Record<string, Blob>) {}
    });
    render(
      <MessageItem
        message={{
          id: 1,
          role: 'agent',
          content: '',
          images: [{ mimeType: 'image/png', data: 'aW1hZ2U=' }],
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Message image 1をコピー' }));

    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(screen.getByRole('button', { name: 'Message image 1をコピー' })).toHaveAttribute(
      'title',
      'コピーしました',
    );
  });
});

describe('MessageItem plan messages', () => {
  it('renders plan content as a regular message without the obsolete approval UI', () => {
    render(
      <MessageItem
        message={{
          id: 1,
          role: 'agent',
          content: '1. Inspect the code\n2. Make the change',
          time: '2026-07-25T00:00:00Z',
          type: 'plan',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
      />,
    );

    expect(screen.getByText(/Inspect the code/)).toBeInTheDocument();
    expect(screen.queryByText('A plan is ready for your review.')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /View plan details/i })).not.toBeInTheDocument();
  });
});

describe('MessageItem links', () => {
  it('allows a long URL in a prompt to wrap within the message', () => {
    const url = 'https://example.com/a/very/long/path/that/must/not/expand/the/chat/layout';

    render(
      <MessageItem
        message={{
          id: 1,
          role: 'user',
          content: url,
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
      />,
    );

    const link = screen.getByRole('link', { name: url });
    expect(link).toHaveClass('break-words');
    expect(link).not.toHaveClass('whitespace-nowrap');
  });
});

describe('MessageItem Mermaid diagrams', () => {
  afterEach(() => {
    mermaidRender.mockReset();
    mermaidInitialize.mockReset();
    document.documentElement.classList.remove('dark');
  });

  it('renders a Mermaid code block as an SVG diagram', async () => {
    mermaidRender.mockResolvedValue({ svg: '<svg><text>Rendered flowchart</text></svg>' });

    render(
      <MessageItem
        message={{
          id: 1,
          role: 'agent',
          content: '```mermaid\ngraph TD\n  A --> B\n```',
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
        isClaudeAgent
      />,
    );

    expect(await screen.findByText('Rendered flowchart')).toBeInTheDocument();
    expect(mermaidInitialize).toHaveBeenCalledWith(expect.objectContaining({
      securityLevel: 'strict',
      startOnLoad: false,
      theme: 'default',
    }));
    expect(mermaidRender).toHaveBeenCalledWith(expect.stringMatching(/^mermaid-/), 'graph TD\n  A --> B');
  });

  it('shows the Mermaid source when rendering fails', async () => {
    mermaidRender.mockRejectedValue(new Error('Invalid diagram'));

    render(
      <MessageItem
        message={{
          id: 2,
          role: 'agent',
          content: '```mermaid\nnot a diagram\n```',
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
        isClaudeAgent
      />,
    );

    expect(await screen.findByText('Mermaid ダイアグラムを描画できませんでした')).toBeInTheDocument();
    expect(screen.getByText('not a diagram')).toBeInTheDocument();
  });
});
