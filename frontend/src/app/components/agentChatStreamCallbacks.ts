import type { ACPSessionCallbacks } from '../../lib/agentapi-proxy-client';
import { ACPConfigOption } from '../../lib/agentapi-proxy-client';
import { PendingAction, SessionMessage } from '../../types/agentapi';
import type { useAgentChat } from './useAgentChat';

export function createAgentChatStreamCallbacks(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'setMessages'
    | 'applyACPConfigOptions'
    | 'setACPPendingPermission'
    | 'setPendingAction'
    | 'setShowQuestionModal'
    | 'setMessageSSEConnectionStatus'
  >,
): Omit<ACPSessionCallbacks, 'onError'> & { onError: (error: Event | Error) => void } {
  const {
    setMessages,
    applyACPConfigOptions,
    setACPPendingPermission,
    setPendingAction,
    setShowQuestionModal,
    setMessageSSEConnectionStatus,
  } = context;
  return {
    onMessage: (msg: SessionMessage) => {
      setMessages((prev) => [...prev, msg]);
    },
    onChunk: (msgId: number, text: string) => {
      setMessages((prev) => prev.map((m) => (m.id === msgId ? { ...m, content: m.content + text } : m)));
    },
    onImageChunk: (msgId: number, image: { mimeType: string; data: string }) => {
      setMessages((prev) => prev.map((m) => (m.id === msgId ? { ...m, images: [...(m.images ?? []), image] } : m)));
    },
    onThoughtChunk: (msgId: number, thought: string) => {
      setMessages((prev) => prev.map((m) => (m.id === msgId ? { ...m, thought: (m.thought ?? '') + thought } : m)));
    },
    onToolUpdate: (toolCallId: string, status: string) => {
      setMessages((prev) =>
        prev.map((m) =>
          m.toolUseId === toolCallId
            ? { ...m, content: JSON.stringify({ ...JSON.parse(m.content || '{}'), _status: status }) }
            : m,
        ),
      );
    },
    onToolInputUpdate: (
      toolCallId: string,
      input: unknown,
      title?: string,
      locations?: Array<{ path: string; line?: number }>,
    ) => {
      setMessages((prev) =>
        prev.map((m) => {
          if (m.toolUseId !== toolCallId) return m;
          try {
            const toolObj = JSON.parse(m.content || '{}');
            return {
              ...m,
              content: JSON.stringify({
                ...toolObj,
                input,
                ...(title != null ? { title } : {}),
                ...(locations != null ? { locations } : {}),
              }),
            };
          } catch {
            return m;
          }
        }),
      );
    },
    onModeUpdate: (modeId: string) => {
      console.log('[ACP] current_mode_update:', modeId);
    },
    onConfigOptionsUpdate: (configOptions: ACPConfigOption[]) => {
      applyACPConfigOptions(configOptions);
    },
    onPermission: (action: PendingAction, rpcId: number) => {
      setACPPendingPermission({ action, rpcId });
      setPendingAction(action);
      setShowQuestionModal(true);
    },
    onConnectionOpen: () => {
      setMessageSSEConnectionStatus('connected');
    },
    onConnectionConnecting: () => {
      setMessageSSEConnectionStatus('connecting');
    },
    onConnectionClosed: () => {
      setMessageSSEConnectionStatus('connecting');
    },
    onError: (err: Event | Error) => {
      console.error('[ACP] SSE error callback (from AgentAPIChat):', err);
    },
  };
}
