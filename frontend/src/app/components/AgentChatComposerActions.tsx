"use client";

import { getEnterKeyBehavior } from "../../types/settings";
import type { useAgentChat } from "./useAgentChat";

type AgentChatState = ReturnType<typeof useAgentChat>;

export function AgentChatComposerActions({ state }: { state: AgentChatState }) {
  return (
    <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
      <ComposerKeyboardHints />
      <div className="flex w-full items-center justify-end gap-2 sm:w-auto">
        <SecretButton state={state} />
        <ImageButton state={state} />
        <ComposerMenu state={state} />
        <SendControls state={state} />
      </div>
    </div>
  );
}

function ComposerKeyboardHints() {
  return (
    <>
      <div className="text-xs text-gray-500 dark:text-gray-400 hidden sm:block">
        {keyboardHint(false)}
      </div>
      <div className="text-xs text-gray-500 dark:text-gray-400 block sm:hidden">
        {keyboardHint(true)}
      </div>
    </>
  );
}

function keyboardHint(mobile: boolean) {
  const behavior = getEnterKeyBehavior();
  const modifier =
    typeof window !== "undefined" && navigator.platform.includes("Mac")
      ? "⌘"
      : "Ctrl";
  if (mobile)
    return behavior === "send" ? "Enter: send" : `${modifier}+Enter: send`;
  return behavior === "send"
    ? `Press Enter to send, ${modifier}+Enter for new line`
    : `Press ${modifier}+Enter to send, Enter for new line`;
}

function SecretButton({ state }: { state: AgentChatState }) {
  const {
    setError,
    setSecretFeedback,
    setShowSecretModal,
    loadSessionSecrets,
    isConnected,
    isInitialLoadComplete,
    sessionId,
  } = state;
  return (
    <button
      type="button"
      onClick={() => {
        setError(null);
        setSecretFeedback(null);
        setShowSecretModal(true);
        void loadSessionSecrets();
      }}
      disabled={!isConnected || !isInitialLoadComplete || !sessionId}
      className="rounded-md bg-amber-600 px-2 py-2 text-xs text-white transition-colors hover:bg-amber-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
      title="このセッションへ一度だけ取得できる秘匿情報を渡す"
      aria-label="秘匿情報を渡す"
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
          d="M12 11V7a4 4 0 00-8 0v4m2 0h12a2 2 0 012 2v7H4v-7a2 2 0 012-2h2m4 4v2"
        />
      </svg>
    </button>
  );
}

function ImageButton({ state }: { state: AgentChatState }) {
  const {
    isACPSession,
    imageInputRef,
    selectedHerdrTerminal,
    isConnected,
    isLoading,
    agentStatus,
    attachedImages,
  } = state;
  return (
    <>
      {isACPSession && (
        <button
          type="button"
          onClick={() => imageInputRef.current?.click()}
          disabled={
            !!selectedHerdrTerminal ||
            !isConnected ||
            isLoading ||
            agentStatus?.status === "running" ||
            attachedImages.length >= 4
          }
          className="rounded-md bg-sky-600 px-2 py-2 text-xs text-white transition-colors hover:bg-sky-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
          title="画像をアップロード（最大4枚、ペースト対応）"
          aria-label="画像を添付"
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
              d="M12 15V3m0 0L8 7m4-4 4 4"
            />
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M5 13v5a2 2 0 002 2h14a2 2 0 002-2v-5"
            />
          </svg>
        </button>
      )}
    </>
  );
}

function ComposerMenu({ state }: { state: AgentChatState }) {
  const { showComposerMenu, setShowComposerMenu, isConnected } = state;
  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setShowComposerMenu((prev) => !prev)}
        disabled={!isConnected}
        className="flex items-center rounded-md bg-gray-600 px-2 py-2 text-xs text-white transition-colors hover:bg-gray-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
        title="その他の操作"
        aria-label="その他の操作"
        aria-expanded={showComposerMenu}
      >
        <svg
          className="h-4 w-4"
          fill="currentColor"
          viewBox="0 0 24 24"
          aria-hidden="true"
        >
          <circle cx="5" cy="12" r="2" />
          <circle cx="12" cy="12" r="2" />
          <circle cx="19" cy="12" r="2" />
        </svg>
      </button>
      {showComposerMenu && <ComposerMenuPopover state={state} />}
    </div>
  );
}

function ComposerMenuPopover({ state }: { state: AgentChatState }) {
  const {
    isACPSession,
    setShowSessionInfo,
    setShowControlPanel,
    setShowComposerMenu,
    setShowFontSettings,
    setShowTemplateModal,
    isLoading,
  } = state;
  return (
    <div className="absolute bottom-full right-0 z-40 mb-2 w-48 overflow-hidden rounded-md border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-800">
      <button
        type="button"
        onClick={() => {
          if (isACPSession) setShowSessionInfo((prev) => !prev);
          else setShowControlPanel((prev) => !prev);
          setShowComposerMenu(false);
        }}
        className="w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-gray-700"
      >
        セッション情報
      </button>
      <button
        type="button"
        onClick={() => {
          setShowFontSettings(true);
          setShowComposerMenu(false);
        }}
        className="w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-gray-700"
      >
        文字表示設定
      </button>
      <button
        type="button"
        onClick={() => {
          setShowTemplateModal(true);
          setShowComposerMenu(false);
        }}
        disabled={isLoading}
        className="w-full px-3 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 disabled:cursor-not-allowed disabled:text-gray-400 dark:text-gray-200 dark:hover:bg-gray-700"
      >
        履歴・テンプレート
      </button>
    </div>
  );
}

function SendControls({ state }: { state: AgentChatState }) {
  const {
    selectedHerdrTerminal,
    interruptSelectedHerdr,
    isHerdrActionPending,
    isConnected,
    sendComposerMessage,
    inputValue,
    isLoading,
    attachedImages,
    agentStatus,
  } = state;
  return (
    <>
      {selectedHerdrTerminal && (
        <button
          type="button"
          onClick={() => void interruptSelectedHerdr()}
          disabled={isHerdrActionPending || !isConnected}
          className="shrink-0 rounded-md bg-red-600 px-3 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:cursor-not-allowed disabled:bg-gray-300 dark:disabled:bg-gray-600"
        >
          中断
        </button>
      )}
      <button
        onClick={() => void sendComposerMessage()}
        disabled={
          !isConnected ||
          (selectedHerdrTerminal
            ? isHerdrActionPending || !inputValue.trim()
            : isLoading ||
              (!inputValue.trim() && attachedImages.length === 0) ||
              agentStatus?.status === "running")
        }
        aria-label="Send"
        className="shrink-0 px-3 sm:px-4 py-2 bg-green-600 text-white rounded-md hover:bg-green-700 focus:outline-none focus:ring-2 focus:ring-green-500 focus:ring-offset-2 disabled:bg-gray-300 dark:disabled:bg-gray-600 disabled:cursor-not-allowed text-sm font-medium"
      >
        {selectedHerdrTerminal
          ? isHerdrActionPending
            ? "送信中..."
            : "Herdrへ送信"
          : isLoading
            ? "Sending..."
            : "Comment"}
      </button>
    </>
  );
}
