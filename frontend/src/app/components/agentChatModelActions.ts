import { ACPConfigOption } from '../../lib/agentapi-proxy-client';
import { formatACPModelUpdateError } from './agentChatPresentation';
import type { useAgentChat } from './useAgentChat';

export async function handleSetACPModelAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'acpServerEnabled'
    | 'acpServerClientRef'
    | 'acpInfo'
    | 'acpModelConfigIdForUpdate'
    | 'acpAgentModelOptions'
    | 'acpCurrentModelValue'
    | 'selectedACPModel'
    | 'setIsSettingACPModel'
    | 'setACPModelMessage'
    | 'applyACPConfigOptions'
  >,
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    acpServerEnabled,
    acpServerClientRef,
    acpInfo,
    acpModelConfigIdForUpdate,
    acpAgentModelOptions,
    acpCurrentModelValue,
    selectedACPModel,
    setIsSettingACPModel,
    setACPModelMessage,
    applyACPConfigOptions,
  } = context;

  if (!sessionId || !acpModelConfigIdForUpdate || !selectedACPModel) return;
  if (selectedACPModel === acpCurrentModelValue) return;

  setIsSettingACPModel(true);
  setACPModelMessage(null);

  try {
    let result: { configOptions?: ACPConfigOption[] } | undefined;
    if (acpServerEnabled && acpServerClientRef.current) {
      result = await acpServerClientRef.current.setSessionConfigOption(
        sessionId,
        acpModelConfigIdForUpdate,
        selectedACPModel,
      );
    } else {
      if (!acpInfo || !agentAPIRef.current) return;
      result = await agentAPIRef.current.setACPSessionConfigOption(
        sessionId,
        acpInfo.sessionId,
        acpModelConfigIdForUpdate,
        selectedACPModel,
      );
    }

    if (result?.configOptions) {
      applyACPConfigOptions(result.configOptions);
    }
    setACPModelMessage('Model updated');
  } catch (err) {
    console.error('Failed to update ACP model:', err);
    setACPModelMessage(
      err instanceof Error
        ? formatACPModelUpdateError(err.message, acpAgentModelOptions, selectedACPModel)
        : 'Failed to update model',
    );
  } finally {
    setIsSettingACPModel(false);
  }
}

export async function handleSetACPEffortAction(
  context: Pick<
    ReturnType<typeof useAgentChat>,
    | 'sessionId'
    | 'agentAPIRef'
    | 'acpServerEnabled'
    | 'acpServerClientRef'
    | 'acpInfo'
    | 'acpEffortConfigId'
    | 'acpCurrentEffortValue'
    | 'selectedACPEffort'
    | 'setIsSettingACPEffort'
    | 'setACPEffortMessage'
    | 'applyACPConfigOptions'
  >,
): Promise<void> {
  const {
    sessionId,
    agentAPIRef,
    acpServerEnabled,
    acpServerClientRef,
    acpInfo,
    acpEffortConfigId,
    acpCurrentEffortValue,
    selectedACPEffort,
    setIsSettingACPEffort,
    setACPEffortMessage,
    applyACPConfigOptions,
  } = context;

  if (!sessionId || !acpEffortConfigId || !selectedACPEffort) return;
  if (selectedACPEffort === acpCurrentEffortValue) return;

  setIsSettingACPEffort(true);
  setACPEffortMessage(null);
  try {
    let result: { configOptions?: ACPConfigOption[] } | undefined;
    if (acpServerEnabled && acpServerClientRef.current) {
      result = await acpServerClientRef.current.setSessionConfigOption(sessionId, acpEffortConfigId, selectedACPEffort);
    } else {
      if (!acpInfo || !agentAPIRef.current) return;
      result = await agentAPIRef.current.setACPSessionConfigOption(
        sessionId,
        acpInfo.sessionId,
        acpEffortConfigId,
        selectedACPEffort,
      );
    }
    if (result?.configOptions) applyACPConfigOptions(result.configOptions);
    setACPEffortMessage('Effort updated');
  } catch (err) {
    console.error('Failed to update ACP effort:', err);
    setACPEffortMessage(err instanceof Error ? err.message : 'Failed to update effort');
  } finally {
    setIsSettingACPEffort(false);
  }
}
