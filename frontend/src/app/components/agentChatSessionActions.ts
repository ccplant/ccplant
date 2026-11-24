import { AgentAPIProxyError } from '../../lib/agentapi-proxy-client';
import type { useAgentChat } from './useAgentChat';

export async function sendStopSignalAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    'sessionId' | 'agentAPIRef' | 'acpServerEnabled' | 'acpServerClientRef' | 'setError' | 'agentType' | 'acpInfo'
  >,
): Promise<void> {
  const { sessionId, agentAPIRef, acpServerEnabled, acpServerClientRef, setError, agentType, acpInfo } = context;

  if (!sessionId || !agentAPIRef.current) {
    setError('セッションが利用できません');
    return;
  }

  try {
    if (acpInfo) {
      // ACP セッション: cancel
      if (acpServerEnabled && acpServerClientRef.current) {
        // ACP server mode: global POST /acp endpoint
        await acpServerClientRef.current.cancelSession(sessionId);
        console.log('Stop signal sent via ACP session/cancel (global ACP server)');
      } else {
        // Per-session bridge mode: POST /{proxySessionId}/rpc directly
        await agentAPIRef.current.cancelACPSession(sessionId, acpInfo.sessionId);
        console.log('Stop signal sent via ACP session/cancel (per-session bridge)');
      }
    } else if (acpServerEnabled && acpServerClientRef.current) {
      // グローバル ACP サーバーモード (per-session bridge なし): ACP cancel を使用
      await acpServerClientRef.current.cancelSession(sessionId);
      console.log('Stop signal sent via ACP session/cancel (global ACP server)');
    } else if (agentType === 'claude' || agentType === 'codex') {
      // agentapi ベースのエージェント（claude, codex）: /action エンドポイントを使用
      await agentAPIRef.current.sendAction(sessionId, { type: 'stop_agent' });
      console.log('Stop signal sent via /action endpoint (agent type:', agentType, ')');
    } else {
      // デフォルト・素のシェルセッション: Ctrl+C を送信
      await agentAPIRef.current.sendSessionMessage(sessionId, {
        content: '\x03', // Ctrl+C
        type: 'raw',
      });
      console.log('Stop signal sent via Ctrl+C (raw message)');
    }
  } catch (err) {
    console.error('Failed to send stop signal:', err);
    setError('停止シグナルの送信に失敗しました');
  }
}

export async function registerOneTimeSecretAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'setError'
    | 'setShowSecretModal'
    | 'secretValue'
    | 'setSecretValue'
    | 'secretLifetime'
    | 'setIsRegisteringSecret'
    | 'setSecretFeedback'
  >,
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    setError,
    setShowSecretModal,
    secretValue,
    setSecretValue,
    secretLifetime,
    setIsRegisteringSecret,
    setSecretFeedback,
  } = context;

  if (!sessionId || !agentAPIRef.current || !secretValue) return;
  setIsRegisteringSecret(true);
  setSecretFeedback(null);
  try {
    await agentAPIRef.current.createSessionSecret(sessionId, secretValue, secretLifetime);
    setSecretValue('');
    setShowSecretModal(false);
    setSecretFeedback('秘匿情報を登録しました。取得後は明示的に再取得を許可できます。');
  } catch (err) {
    setError(`秘匿情報の登録に失敗しました: ${err instanceof Error ? err.message : '不明なエラー'}`);
  } finally {
    setIsRegisteringSecret(false);
  }
}

export async function loadSessionSecretsAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    'sessionId' | 'agentAPIRef' | 'setError' | 'setSessionSecrets' | 'setIsLoadingSessionSecrets'
  >,
): Promise<void> {
  const { sessionId, agentAPIRef, setError, setSessionSecrets, setIsLoadingSessionSecrets } = context;

  if (!sessionId || !agentAPIRef.current) return;
  setIsLoadingSessionSecrets(true);
  try {
    const response = await agentAPIRef.current.listSessionSecrets(sessionId);
    setSessionSecrets(response.secrets || []);
  } catch (err) {
    setError(`秘匿情報の状態取得に失敗しました: ${err instanceof Error ? err.message : '不明なエラー'}`);
  } finally {
    setIsLoadingSessionSecrets(false);
  }
}

export async function reauthorizeOneTimeSecretAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'setError'
    | 'setShowSecretModal'
    | 'secretLifetime'
    | 'setIsRegisteringSecret'
    | 'setSecretFeedback'
  >,
  secretId: string,
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    setError,
    setShowSecretModal,
    secretLifetime,
    setIsRegisteringSecret,
    setSecretFeedback,
  } = context;

  if (!sessionId || !agentAPIRef.current) return;
  setIsRegisteringSecret(true);
  setSecretFeedback(null);
  try {
    await agentAPIRef.current.reauthorizeSessionSecret(sessionId, secretId, secretLifetime);
    setShowSecretModal(false);
    setSecretFeedback('同じ秘匿情報の再取得を1回許可しました。');
  } catch (err) {
    setError(`再取得の許可に失敗しました: ${err instanceof Error ? err.message : '不明なエラー'}`);
  } finally {
    setIsRegisteringSecret(false);
  }
}

export async function deleteSessionAction(
  context: Pick<ReturnType<typeof useAgentChat>, 'router' | 'sessionId' | 'agentAPIRef' | 'setError' | 'setIsDeleting'>,
): Promise<void> {
  const { router, sessionId, agentAPIRef, setError, setIsDeleting } = context;

  if (!sessionId) return;

  const confirmed = window.confirm('このセッションを削除しますか？この操作は元に戻せません。');
  if (!confirmed) return;

  setIsDeleting(true);
  setError(null);

  try {
    if (!agentAPIRef.current) {
      setError('AgentAPI client not available');
      return;
    }
    await agentAPIRef.current.delete(sessionId);
    // セッション削除後、conversation画面にリダイレクト
    router.push('/chats');
  } catch (err) {
    console.error('Failed to delete session:', err);
    if (err instanceof AgentAPIProxyError) {
      setError(`セッションの削除に失敗しました: ${err.message}`);
    } else {
      setError('セッションの削除に失敗しました');
    }
  } finally {
    setIsDeleting(false);
  }
}
