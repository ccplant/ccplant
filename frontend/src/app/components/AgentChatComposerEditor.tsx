"use client";

import type { useAgentChat } from "./useAgentChat";

type AgentChatState = ReturnType<typeof useAgentChat>;

export function AgentChatComposerEditor({ state }: { state: AgentChatState }) {
  return (
    <>
      <ComposerSecretFeedback state={state} />
      <ComposerAttachments state={state} />
      <ComposerInput state={state} />
      <ComposerTemplates state={state} />
    </>
  );
}

function ComposerSecretFeedback({ state }: { state: AgentChatState }) {
  const { secretFeedback, setSecretFeedback } = state;
  return (
    <>
      {secretFeedback && (
        <div className="mb-2 flex items-start justify-between rounded-md border border-emerald-200 bg-emerald-50 px-3 py-2 text-xs text-emerald-800 dark:border-emerald-800 dark:bg-emerald-950/40 dark:text-emerald-200">
          <span>{secretFeedback}</span>
          <button
            type="button"
            onClick={() => setSecretFeedback(null)}
            aria-label="通知を閉じる"
            className="ml-3 font-bold"
          >
            ×
          </button>
        </div>
      )}
    </>
  );
}

function ComposerAttachments({ state }: { state: AgentChatState }) {
  const { selectedHerdrTerminal, attachedImages, setAttachedImages } = state;
  return (
    <>
      {!selectedHerdrTerminal && attachedImages.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-2">
          {attachedImages.map((image, index) => (
            <div key={`${image.name}:${index}`} className="relative">
              {/* eslint-disable-next-line @next/next/no-img-element */}
              <img
                src={`data:${image.mimeType};base64,${image.data}`}
                alt={image.name}
                className="h-20 w-20 rounded-md border border-gray-300 object-cover dark:border-gray-600"
              />
              <button
                type="button"
                onClick={() =>
                  setAttachedImages((prev) =>
                    prev.filter((_, itemIndex) => itemIndex !== index),
                  )
                }
                className="absolute -right-2 -top-2 flex h-6 w-6 items-center justify-center rounded-full bg-gray-800 text-xs text-white shadow"
                aria-label={`${image.name} を削除`}
              >
                ×
              </button>
            </div>
          ))}
        </div>
      )}
    </>
  );
}

function ComposerInput({ state }: { state: AgentChatState }) {
  const {
    isInitialLoadComplete,
    inputValue,
    setInputValue,
    messageInputRef,
    imageInputRef,
    isLoading,
    agentStatus,
    isConnected,
    selectedHerdrTerminal,
    isHerdrActionPending,
    setShowTemplates,
    handleImageSelection,
    handleImagePaste,
    handleKeyDown,
  } = state;
  return (
    <>
      <input
        ref={imageInputRef}
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={(event) => void handleImageSelection(event.target.files)}
        aria-label="画像を添付"
      />
      <textarea
        ref={messageInputRef}
        aria-label="Message"
        value={inputValue}
        onChange={(e) => setInputValue(e.target.value)}
        onKeyDown={handleKeyDown}
        onPaste={selectedHerdrTerminal ? undefined : handleImagePaste}
        onFocus={() => {
          // テンプレートの自動表示を無効化
        }}
        onBlur={() => {
          setTimeout(() => setShowTemplates(false), 150);
        }}
        placeholder={composerPlaceholder(state)}
        className="w-full resize-none border border-gray-300 dark:border-gray-600 rounded-md px-3 py-2 bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 placeholder-gray-500 dark:placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 min-h-[80px]"
        rows={3}
        disabled={
          !isConnected ||
          !isInitialLoadComplete ||
          (selectedHerdrTerminal
            ? isHerdrActionPending
            : isLoading || agentStatus?.status === "running")
        }
      />
    </>
  );
}

function composerPlaceholder(state: AgentChatState) {
  if (!state.isConnected || !state.isInitialLoadComplete)
    return "Connecting...";
  if (state.selectedHerdrTerminal)
    return "選択した Herdr エージェントへ送信...";
  if (state.agentStatus?.status === "running")
    return "Agent is running, please wait...";
  return "Write a comment...";
}

function ComposerTemplates({ state }: { state: AgentChatState }) {
  const { showTemplates, templates, setInputValue, setShowTemplates } = state;
  return (
    <>
      {showTemplates && templates.length > 0 && (
        <div className="absolute z-50 w-full bottom-full mb-1 bg-white dark:bg-gray-800 border border-gray-300 dark:border-gray-600 rounded-md shadow-lg max-h-60 overflow-y-auto">
          <div className="px-3 py-2 text-xs text-gray-500 dark:text-gray-400 border-b border-gray-200 dark:border-gray-700">
            メッセージテンプレート
          </div>
          {templates.map((template) => (
            <button
              key={template.id}
              type="button"
              onClick={() => {
                setInputValue(template.content);
                setShowTemplates(false);
              }}
              className="w-full text-left px-3 py-3 hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-900 dark:text-white border-b border-gray-100 dark:border-gray-700 last:border-b-0"
            >
              <div className="font-medium text-sm">{template.name}</div>
              <div className="line-clamp-2 text-xs text-gray-600 dark:text-gray-400 mt-1">
                {template.content}
              </div>
            </button>
          ))}
        </div>
      )}
    </>
  );
}
