import { act, cleanup, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { ACPSessionInfo } from '../../../lib/agentapi-proxy-client';
import { useACPModelSelection } from '../useACPModelSelection';
import { useChatImages } from '../useChatImages';

afterEach(cleanup);

describe('chat model selection', () => {
  it('keeps advertised options first and deduplicates trimmed profile candidates', () => {
    const info: ACPSessionInfo = {
      sessionId: 'session',
      status: 'idle',
      configOptions: [{ id: 'model', category: 'model', currentValue: 'agent-model', options: ['agent-model'] }],
    };
    const { result } = renderHook(() => useACPModelSelection(info, [' agent-model ', '', 'extra', 'extra']));
    expect(result.current.acpModelOptions.map((option) => option.value)).toEqual(['agent-model', 'extra']);
    expect(result.current.selectedACPModel).toBe('agent-model');
    expect(result.current.acpSelectedModelIsKnown).toBe(true);
  });

  it('resets selection and feedback when server configuration changes', () => {
    const initial: ACPSessionInfo = { sessionId: 'session', status: 'idle', model: 'first' };
    const { result, rerender } = renderHook(({ info }) => useACPModelSelection(info, []), {
      initialProps: { info: initial },
    });
    act(() => {
      result.current.setSelectedACPModel('pending');
      result.current.setACPModelMessage('old feedback');
    });
    rerender({ info: { ...initial, model: 'second' } });
    expect(result.current.selectedACPModel).toBe('second');
    expect(result.current.acpModelMessage).toBeNull();
    expect(result.current.acpModelConfigIdForUpdate).toBe('model');
  });
});

describe('chat image attachments', () => {
  it('encodes only images and keeps at most four attachments in selection order', async () => {
    const { result } = renderHook(useChatImages);
    const files = Array.from({ length: 5 }, (_, index) => new File(['image'], `${index}.png`, { type: 'image/png' }));
    await act(async () => {
      await result.current.handleImageSelection([new File(['text'], 'notes.txt', { type: 'text/plain' }), ...files]);
    });
    expect(result.current.attachedImages.map((image) => image.name)).toEqual(['0.png', '1.png', '2.png', '3.png']);
    expect(result.current.attachedImages[0]).toEqual({ name: '0.png', mimeType: 'image/png', data: 'aW1hZ2U=' });
  });

  it('treats an empty file selection as a no-op', async () => {
    const { result } = renderHook(useChatImages);
    await act(async () => {
      await result.current.handleImageSelection(null);
    });
    expect(result.current.attachedImages).toEqual([]);
  });
});
