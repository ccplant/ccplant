import { AgentAPIProxyError } from '../../lib/agentapi-proxy-client';
import type { useAgentChat } from './useAgentChat';

export async function handleAnswerSubmitAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'acpServerEnabled'
    | 'acpServerClientRef'
    | 'setError'
    | 'pendingAction'
    | 'setPendingAction'
    | 'setShowQuestionModal'
    | 'acpInfo'
    | 'acpPendingPermission'
    | 'setACPPendingPermission'
  >,
  answers: Record<string, string | string[]>,
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    acpServerEnabled,
    acpServerClientRef,
    setError,
    pendingAction,
    setPendingAction,
    setShowQuestionModal,
    acpInfo,
    acpPendingPermission,
    setACPPendingPermission,
  } = context;

  if (!sessionId || !agentAPIRef.current || !pendingAction) return;

  try {
    if (acpInfo && acpPendingPermission) {
      // ── ACP: reply to session/request_permission ──
      // answers format: { "0": "selectedOptionLabel" }
      const selectedLabel = Object.values(answers)[0] as string | undefined;
      const options = acpPendingPermission.action.content?.questions?.[0]?.options ?? [];
      const matched = options.find((o) => o.label === selectedLabel);
      const optionId = matched ? matched.label : (selectedLabel ?? '');
      if (acpServerEnabled && acpServerClientRef.current) {
        await acpServerClientRef.current.sendPermissionResponse(sessionId, acpPendingPermission.rpcId, optionId);
      } else {
        await agentAPIRef.current.replyToACPPermission(sessionId, acpPendingPermission.rpcId, optionId);
      }
      setACPPendingPermission(null);
    } else {
      // ── Regular session ───────────────────────────────────────────────
      await agentAPIRef.current.sendAction(sessionId, {
        type: 'answer_question',
        answers,
      });
    }

    // Clear pending action and close modal
    setPendingAction(null);
    setShowQuestionModal(false);
  } catch (err) {
    console.error('Failed to submit answers:', err);
    if (err instanceof AgentAPIProxyError) {
      setError(`Failed to submit answers: ${err.message}`);
    }
  }
}
