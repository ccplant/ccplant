"use client";

import { formatNumber } from "./agentChatPresentation";
import type { useAgentChat } from "./useAgentChat";

type AgentChatState = ReturnType<typeof useAgentChat>;

export function AgentChatSessionInfo({ state }: { state: AgentChatState }) {
  if (!state.showSessionInfo) return null;
  return (
    <div className="relative mb-3 grid gap-2 rounded-md border border-gray-200 bg-gray-50 p-3 pr-10 text-xs dark:border-gray-700 dark:bg-gray-800">
      <CloseSessionInfoButton state={state} />
      <SessionSummary state={state} />
      <ACPSessionDetails state={state} />
    </div>
  );
}

function CloseSessionInfoButton({ state }: { state: AgentChatState }) {
  const { setShowSessionInfo } = state;
  return (
    <button
      type="button"
      onClick={() => setShowSessionInfo(false)}
      className="absolute right-2 top-2 rounded-md p-1 text-gray-500 transition-colors hover:bg-gray-200 hover:text-gray-700 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200"
      aria-label="セッション情報を閉じる"
      title="閉じる"
    >
      <svg
        className="h-4 w-4"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
        aria-hidden="true"
      >
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M6 18L18 6M6 6l12 12"
        />
      </svg>
    </button>
  );
}

function SessionSummary({ state }: { state: AgentChatState }) {
  const {
    sessionId,
    agentType,
    agentStatus,
    connectionStatusLabel,
    tokenUsage,
  } = state;
  return (
    <>
      <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
        <span className="text-gray-500 dark:text-gray-400">Session ID</span>
        <span className="break-all font-mono text-gray-900 dark:text-gray-100">
          {sessionId || "-"}
        </span>
      </div>
      <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
        <span className="text-gray-500 dark:text-gray-400">Agent Type</span>
        <span className="break-all text-gray-900 dark:text-gray-100">
          {agentType || "-"}
        </span>
      </div>
      <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
        <span className="text-gray-500 dark:text-gray-400">Status</span>
        <span className="break-all text-gray-900 dark:text-gray-100">
          {agentStatus?.status || "-"}
        </span>
      </div>
      <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
        <span className="text-gray-500 dark:text-gray-400">Connection</span>
        <span className="break-all text-gray-900 dark:text-gray-100">
          {connectionStatusLabel.toLowerCase()}
        </span>
      </div>
      <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
        <span className="text-gray-500 dark:text-gray-400">Tokens so far</span>
        <span className="break-all text-gray-900 dark:text-gray-100">
          {formatNumber(tokenUsage.total)}
          {tokenUsage.source === "estimated" ? " estimated" : ""}
          {tokenUsage.source === "reported" &&
          tokenUsage.input != null &&
          tokenUsage.output != null
            ? ` (${formatNumber(tokenUsage.input)} in / ${formatNumber(tokenUsage.output)} out)`
            : ""}
        </span>
      </div>
    </>
  );
}

function ACPSessionDetails({ state }: { state: AgentChatState }) {
  const { acpInfo, acpModelDisplay } = state;
  if (!acpInfo) return null;
  return (
    <>
      <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
        <span className="text-gray-500 dark:text-gray-400">ACP Session</span>
        <span className="break-all font-mono text-gray-900 dark:text-gray-100">
          {acpInfo.sessionId || "-"}
        </span>
      </div>
      <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
        <span className="text-gray-500 dark:text-gray-400">Model</span>
        <span className="break-all text-gray-900 dark:text-gray-100">
          {acpModelDisplay || "-"}
        </span>
      </div>
      <ACPModelSettings state={state} />
      <ACPEffortSettings state={state} />
      <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
        <span className="text-gray-500 dark:text-gray-400">ACP Status</span>
        <span className="break-all text-gray-900 dark:text-gray-100">
          {acpInfo.status || "-"}
        </span>
      </div>
    </>
  );
}

function ACPModelSettings({ state }: { state: AgentChatState }) {
  if (!state.acpModelConfigIdForUpdate) return null;
  return (
    <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
      <label
        htmlFor="acp-model-select"
        className="pt-2 text-gray-500 dark:text-gray-400"
      >
        Switch Model
      </label>
      <div className="min-w-0">
        <ACPModelControls state={state} />
        <ACPModelGuidance state={state} />
      </div>
    </div>
  );
}

function ACPModelControls({ state }: { state: AgentChatState }) {
  const {
    isSettingACPModel,
    selectedACPModel,
    acpCurrentModelValue,
    handleSetACPModel,
  } = state;
  return (
    <div className="flex gap-2 sm:items-start">
      <ACPModelSelect state={state} />
      <button
        type="button"
        onClick={handleSetACPModel}
        disabled={
          isSettingACPModel ||
          !selectedACPModel ||
          selectedACPModel === acpCurrentModelValue
        }
        className="shrink-0 rounded-md bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
      >
        {isSettingACPModel ? "Applying..." : "Apply"}
      </button>
    </div>
  );
}

function ACPModelSelect({ state }: { state: AgentChatState }) {
  const {
    selectedACPModel,
    setSelectedACPModel,
    setACPModelMessage,
    isSettingACPModel,
    acpModelOptions,
    acpSelectedModelIsKnown,
    acpModelSelectGroups,
  } = state;
  return (
    <select
      id="acp-model-select"
      value={selectedACPModel}
      onChange={(event) => {
        setSelectedACPModel(event.target.value);
        setACPModelMessage(null);
      }}
      disabled={isSettingACPModel || acpModelOptions.length === 0}
      className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-2 py-1.5 text-xs text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:cursor-not-allowed disabled:bg-gray-100 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100 dark:disabled:bg-gray-700"
    >
      {!selectedACPModel && <option value="">モデルを選択</option>}
      {selectedACPModel && !acpSelectedModelIsKnown && (
        <option value={selectedACPModel}>{selectedACPModel}</option>
      )}
      {acpModelSelectGroups.map((group, groupIndex) =>
        group.name ? (
          <optgroup
            key={`group:${group.name}:${groupIndex}`}
            label={group.name}
          >
            {group.options.map((option) => (
              <option key={`model:${option.value}`} value={option.value}>
                {option.label}
              </option>
            ))}
          </optgroup>
        ) : (
          group.options.map((option) => (
            <option key={`model:${option.value}`} value={option.value}>
              {option.label}
            </option>
          ))
        ),
      )}
    </select>
  );
}

function ACPModelGuidance({ state }: { state: AgentChatState }) {
  const {
    acpModelOptions,
    acpProfileModelOptions,
    selectedACPModel,
    acpModelMessage,
  } = state;
  const description = acpModelOptions.find(
    (option) => option.value === selectedACPModel,
  )?.description;
  return (
    <>
      {acpModelOptions.length === 0 && (
        <p className="mt-1 text-[11px] text-gray-500 dark:text-gray-400">
          エージェントがモデル候補を提示していません。セッションプロファイルの「モデル候補」で切り替え候補を設定できます。
        </p>
      )}
      {acpProfileModelOptions.length > 0 && (
        <p className="mt-1 text-[11px] text-amber-600 dark:text-amber-400">
          プロファイルのモデル候補 {acpProfileModelOptions.length}{" "}
          件はエージェントが提示していないため、切り替えできない場合があります。
        </p>
      )}
      {description && (
        <p className="mt-1 text-[11px] text-gray-500 dark:text-gray-400">
          {description}
        </p>
      )}
      {acpModelMessage && (
        <p
          className={`mt-1 text-[11px] ${acpModelMessage === "Model updated" ? "text-green-600 dark:text-green-400" : "text-red-600 dark:text-red-400"}`}
        >
          {acpModelMessage}
        </p>
      )}
    </>
  );
}

function ACPEffortSettings({ state }: { state: AgentChatState }) {
  const {
    acpEffortConfigId,
    acpEffortOptions,
    selectedACPEffort,
    setSelectedACPEffort,
    isSettingACPEffort,
    handleSetACPEffort,
    acpCurrentEffortValue,
    acpEffortMessage,
    agentStatus,
  } = state;
  return (
    <>
      {acpEffortConfigId && acpEffortOptions.length > 0 && (
        <div className="grid grid-cols-[7.5rem_minmax(0,1fr)] gap-2">
          <label
            htmlFor="acp-effort-select"
            className="pt-2 text-gray-500 dark:text-gray-400"
          >
            Effort
          </label>
          <div className="min-w-0">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
              <select
                id="acp-effort-select"
                value={selectedACPEffort}
                onChange={(event) => setSelectedACPEffort(event.target.value)}
                disabled={
                  isSettingACPEffort || agentStatus?.status === "running"
                }
                className="min-w-0 flex-1 rounded-md border border-gray-300 bg-white px-2 py-1.5 text-xs text-gray-900 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500 disabled:cursor-not-allowed disabled:bg-gray-100 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-100 dark:disabled:bg-gray-700"
              >
                {acpEffortOptions.map((option) => (
                  <option key={`effort:${option.value}`} value={option.value}>
                    {option.label}
                  </option>
                ))}
              </select>
              <button
                type="button"
                onClick={handleSetACPEffort}
                disabled={
                  isSettingACPEffort ||
                  agentStatus?.status === "running" ||
                  !selectedACPEffort ||
                  selectedACPEffort === acpCurrentEffortValue
                }
                className="shrink-0 rounded-md bg-blue-600 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
              >
                {isSettingACPEffort ? "Applying..." : "Apply"}
              </button>
            </div>
            <EffortMessage message={acpEffortMessage} />
          </div>
        </div>
      )}
    </>
  );
}

function EffortMessage({ message }: { message: string | null }) {
  if (!message) return null;
  const color =
    message === "Effort updated"
      ? "text-green-600 dark:text-green-400"
      : "text-red-600 dark:text-red-400";
  return <p className={`mt-1 text-[11px] ${color}`}>{message}</p>;
}
