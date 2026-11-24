'use client'

import { ACPConfigOption, ACPSessionInfo } from '../../lib/agentapi-proxy-client'
import { handleSetACPEffortAction, handleSetACPModelAction } from './agentChatModelActions'
import { useACPModelSelection } from './useACPModelSelection'
import { useAgentChatClients } from './useAgentChatClients'

interface ModelHandlerInput {
  sessionId: string | null
  clients: ReturnType<typeof useAgentChatClients>
  acpInfo: ACPSessionInfo | null
  setACPInfo: React.Dispatch<React.SetStateAction<ACPSessionInfo | null>>
  model: ReturnType<typeof useACPModelSelection>
}

export function useAgentChatModelHandlers({ sessionId, clients, acpInfo, setACPInfo, model }: ModelHandlerInput) {
  const applyACPConfigOptions = (configOptions: ACPConfigOption[]) => {
    setACPInfo((previous) => previous ? { ...previous, configOptions } : previous)
  }
  const common = {
    sessionId, agentAPIRef: clients.agentAPIRef, acpServerEnabled: clients.acpServerEnabled,
    acpServerClientRef: clients.acpServerClientRef, acpInfo, applyACPConfigOptions,
  }
  const handleSetACPModel = async () => handleSetACPModelAction({
    ...common, acpModelConfigIdForUpdate: model.acpModelConfigIdForUpdate,
    acpAgentModelOptions: model.acpAgentModelOptions, acpCurrentModelValue: model.acpCurrentModelValue,
    selectedACPModel: model.selectedACPModel, setIsSettingACPModel: model.setIsSettingACPModel,
    setACPModelMessage: model.setACPModelMessage,
  })
  const handleSetACPEffort = async () => handleSetACPEffortAction({
    ...common, acpEffortConfigId: model.acpEffortConfigId, acpCurrentEffortValue: model.acpCurrentEffortValue,
    selectedACPEffort: model.selectedACPEffort, setIsSettingACPEffort: model.setIsSettingACPEffort,
    setACPEffortMessage: model.setACPEffortMessage,
  })
  return { applyACPConfigOptions, handleSetACPModel, handleSetACPEffort }
}
