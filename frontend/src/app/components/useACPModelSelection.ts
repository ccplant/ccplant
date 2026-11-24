'use client';

import { useEffect, useMemo, useState } from 'react';
import { ACPSessionInfo } from '../../lib/agentapi-proxy-client';
import {
  ACP_MODEL_CONFIG_ID_FALLBACK,
  ACP_PROFILE_MODEL_GROUP,
  ACPModelOption,
  flattenACPModelOptions,
  formatACPModelValue,
  getACPConfigOptionCurrentValue,
  getACPConfigOptionId,
  getACPEffortConfigOption,
  getACPModelConfigOption,
  getACPModelDisplay,
} from './agentChatPresentation';

export function useACPModelSelection(acpInfo: ACPSessionInfo | null, sessionModelOptions: string[]) {
  const acpModelConfigOption = useMemo(() => getACPModelConfigOption(acpInfo), [acpInfo]);
  const acpModelConfigId = useMemo(
    () => getACPConfigOptionId(acpModelConfigOption ?? undefined),
    [acpModelConfigOption]
  );
  // Fall back to the conventional config id so sessions whose agent does not
  // advertise a model option can still switch to an advertised/configured model.
  const acpModelConfigIdForUpdate = acpModelConfigId ?? (acpInfo ? ACP_MODEL_CONFIG_ID_FALLBACK : null);
  const acpAgentModelOptions = useMemo(
    () => flattenACPModelOptions(acpModelConfigOption?.options),
    [acpModelConfigOption]
  );
  // Only these values are guaranteed to be accepted by the agent. ACP agents
  // validate the model against their own option list, so values that come from
  // the session profile alone may be rejected with -32602.
  const acpProfileModelOptions = useMemo(() => {
    const known = new Set(acpAgentModelOptions.map((option) => option.value));
    const options: ACPModelOption[] = [];
    for (const model of sessionModelOptions) {
      const value = model.trim();
      if (!value || known.has(value)) continue;
      known.add(value);
      options.push({ value, label: value, group: ACP_PROFILE_MODEL_GROUP });
    }
    return options;
  }, [acpAgentModelOptions, sessionModelOptions]);
  // The session profile can contribute extra candidates. Agent-advertised
  // options come first because they carry labels/descriptions; user-configured
  // values that the agent does not advertise are appended.
  const acpModelOptions = useMemo(
    () => [...acpAgentModelOptions, ...acpProfileModelOptions],
    [acpAgentModelOptions, acpProfileModelOptions]
  );
  const acpCurrentModelValue = useMemo(
    () =>
      getACPConfigOptionCurrentValue(acpModelConfigOption ?? undefined) ?? formatACPModelValue(acpInfo?.model) ?? null,
    [acpModelConfigOption, acpInfo]
  );
  const acpModelDisplay = useMemo(() => getACPModelDisplay(acpInfo), [acpInfo]);
  const [selectedACPModel, setSelectedACPModel] = useState('');
  const acpSelectedModelIsKnown = acpModelOptions.some((option) => option.value === selectedACPModel);
  const acpModelSelectGroups = useMemo(() => {
    const groups: { name: string; options: ACPModelOption[] }[] = [];
    for (const option of acpModelOptions) {
      const name = option.group ?? '';
      const last = groups[groups.length - 1];
      if (last && last.name === name) {
        last.options.push(option);
      } else {
        groups.push({ name, options: [option] });
      }
    }
    return groups;
  }, [acpModelOptions]);
  const [isSettingACPModel, setIsSettingACPModel] = useState(false);
  const [acpModelMessage, setACPModelMessage] = useState<string | null>(null);
  const acpEffortConfigOption = useMemo(() => getACPEffortConfigOption(acpInfo), [acpInfo]);
  const acpEffortConfigId = useMemo(
    () => getACPConfigOptionId(acpEffortConfigOption ?? undefined),
    [acpEffortConfigOption]
  );
  const acpEffortOptions = useMemo(
    () => flattenACPModelOptions(acpEffortConfigOption?.options),
    [acpEffortConfigOption]
  );
  const acpCurrentEffortValue = useMemo(
    () => getACPConfigOptionCurrentValue(acpEffortConfigOption ?? undefined),
    [acpEffortConfigOption]
  );
  const [selectedACPEffort, setSelectedACPEffort] = useState('');
  const [isSettingACPEffort, setIsSettingACPEffort] = useState(false);
  const [acpEffortMessage, setACPEffortMessage] = useState<string | null>(null);
  useEffect(() => {
    setSelectedACPModel(acpCurrentModelValue ?? '');
    setACPModelMessage(null);
  }, [acpCurrentModelValue, acpModelConfigIdForUpdate]);

  useEffect(() => {
    setSelectedACPEffort(acpCurrentEffortValue ?? '');
    setACPEffortMessage(null);
  }, [acpCurrentEffortValue, acpEffortConfigId]);

  return {
    acpModelConfigOption,
    acpModelConfigId,
    acpModelConfigIdForUpdate,
    acpAgentModelOptions,
    acpProfileModelOptions,
    acpModelOptions,
    acpCurrentModelValue,
    acpModelDisplay,
    selectedACPModel,
    setSelectedACPModel,
    acpSelectedModelIsKnown,
    acpModelSelectGroups,
    isSettingACPModel,
    setIsSettingACPModel,
    acpModelMessage,
    setACPModelMessage,
    acpEffortConfigOption,
    acpEffortConfigId,
    acpEffortOptions,
    acpCurrentEffortValue,
    selectedACPEffort,
    setSelectedACPEffort,
    isSettingACPEffort,
    setIsSettingACPEffort,
    acpEffortMessage,
    setACPEffortMessage,
  };
}
