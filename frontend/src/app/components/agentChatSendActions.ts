import { AgentAPIProxyError } from '../../lib/agentapi-proxy-client';
import { recentMessagesManager } from '../../utils/recentMessagesManager';
import type { useAgentChat } from './useAgentChat';

export async function sendMessageAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'acpServerEnabled'
    | 'acpServerClientRef'
    | 'setMessages'
    | 'inputValue'
    | 'setInputValue'
    | 'attachedImages'
    | 'setAttachedImages'
    | 'isLoading'
    | 'setIsLoading'
    | 'agentStatus'
    | 'setAgentStatus'
    | 'setError'
    | 'isConnected'
    | 'setShouldAutoScroll'
    | 'acpInfo'
    | 'acpNextPromptId'
    | 'scrollToBottom'
    | 'loadRecentMessages'
  >,
  messageType: 'user' | 'raw' = 'user',
  content?: string,
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    acpServerEnabled,
    acpServerClientRef,
    setMessages,
    inputValue,
    setInputValue,
    attachedImages,
    setAttachedImages,
    isLoading,
    setIsLoading,
    agentStatus,
    setAgentStatus,
    setError,
    isConnected,
    setShouldAutoScroll,
    acpInfo,
    acpNextPromptId,
    scrollToBottom,
    loadRecentMessages,
  } = context;

  const messageContent = content || inputValue.trim();

  if (!messageContent && attachedImages.length === 0 && messageType === 'user') return;
  if (isLoading || !isConnected) return;

  if (agentStatus?.status === 'running' && messageType === 'user') {
    setError('Agent is currently running. Please wait for it to become stable.');
    return;
  }

  setIsLoading(true);
  setError(null);

  try {
    if (sessionId) {
      // Send message via session
      if (!agentAPIRef.current) {
        setError('AgentAPI client not available');
        return;
      }

      if (acpInfo) {
        // ── ACP session: send prompt ──────────────────────────────────────
        const promptId = acpNextPromptId.current++;
        // Do NOT add the user message locally here.
        // The bridge broadcasts a synthetic user_message_chunk via SSE,
        // which will arrive via onMessage and be added to the message list.
        const prompt = [
          ...(messageContent ? [{ type: 'text' as const, text: messageContent }] : []),
          ...attachedImages.map((image) => ({
            type: 'image' as const,
            mimeType: image.mimeType,
            data: image.data,
          })),
        ];
        if (acpServerEnabled && acpServerClientRef.current) {
          // ACP server mode: global POST /acp endpoint with proxy session ID
          await acpServerClientRef.current.sendPrompt(sessionId!, prompt, promptId);
        } else {
          // Per-session bridge mode: POST /{proxySessionId}/rpc directly to the bridge.
          // acpInfo.sessionId is the bridge-internal ACP session ID.
          await agentAPIRef.current.sendACPPrompt(sessionId!, acpInfo.sessionId, prompt, promptId);
        }
      } else if (acpServerEnabled && acpServerClientRef.current) {
        // ── Global ACP server only (no per-session bridge) ────────────
        const promptId = acpNextPromptId.current++;
        const prompt = [
          ...(messageContent ? [{ type: 'text' as const, text: messageContent }] : []),
          ...attachedImages.map((image) => ({
            type: 'image' as const,
            mimeType: image.mimeType,
            data: image.data,
          })),
        ];
        await acpServerClientRef.current.sendPrompt(sessionId, prompt, promptId);
      } else {
        // ── Regular session ───────────────────────────────────────────
        const sessionMessage = await agentAPIRef.current.sendSessionMessage(sessionId, {
          content: messageContent,
          type: messageType,
        });

        // For user messages, add to messages
        if (messageType === 'user') {
          setMessages((prev) => [...prev, sessionMessage]);
        }
      }
    } else {
      setError('No session ID available. Cannot send message.');
      return;
    }

    if (messageType === 'user') {
      // 最近のメッセージに保存
      await recentMessagesManager.saveMessage(messageContent);
      await loadRecentMessages();

      setInputValue('');
      setAttachedImages([]);
      // メッセージ送信時は必ずスクロール
      setShouldAutoScroll(true);
      setTimeout(() => scrollToBottom(), 100);
    }
  } catch (err) {
    console.error('Failed to send message:', err);
    // Reset status to stable if the send failed so the UI doesn't get stuck at 'running'
    setAgentStatus((prev) => (prev?.status === 'running' ? { status: 'stable' } : prev));
    if (err instanceof AgentAPIProxyError) {
      // Handle timeout errors specially
      if (err.code === 'TIMEOUT_ERROR') {
        setError(`${err.message}`);
      } else {
        setError(`メッセージ送信に失敗しました: ${err.message} (セッション: ${sessionId})`);
      }
    } else {
      setError(`メッセージ送信に失敗しました: ${err instanceof Error ? err.message : '不明なエラー'}`);
    }
  } finally {
    setIsLoading(false);
  }
}
