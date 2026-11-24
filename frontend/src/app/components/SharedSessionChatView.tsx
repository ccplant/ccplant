"use client";

import type { Dispatch, ReactElement, RefObject, SetStateAction } from "react";
import Link from "next/link";

export interface SharedSessionMessage {
  id: number;
  role: "user" | "agent";
  content: string;
  timestamp: string;
}

export interface SharedAgentStatus {
  status: "stable" | "running" | "error";
  last_activity?: string;
}

interface SharedSessionChatViewProps {
  messages: SharedSessionMessage[];
  agentStatus: SharedAgentStatus | null;
  error: string | null;
  isConnected: boolean;
  messagesContainerRef: RefObject<HTMLDivElement | null>;
  messagesEndRef: RefObject<HTMLDivElement | null>;
  handleScroll: () => void;
  hasNewMessages: boolean;
  shouldAutoScroll: boolean;
  setShouldAutoScroll: Dispatch<SetStateAction<boolean>>;
  setHasNewMessages: Dispatch<SetStateAction<boolean>>;
  scrollToBottom: () => void;
}

function formatTextWithLinks(text: string): ReactElement {
  if (!text || typeof text !== "string") return <>{text || ""}</>;

  const parts = text.split(/(https?:\/\/[^\s<>"'()[\]{}]+)/g);
  return (
    <>
      {parts.map((part, index) => {
        if (!/^https?:\/\//.test(part)) return part;
        const cleanUrl = part.replace(/[.,;!?]+$/, "");
        return (
          <a
            key={index}
            href={cleanUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline whitespace-nowrap"
          >
            {cleanUrl}
          </a>
        );
      })}
    </>
  );
}

function statusTextColor(status: string): string {
  if (status === "stable") return "text-green-600 dark:text-green-400";
  if (status === "running") return "text-yellow-600 dark:text-yellow-400";
  return "text-gray-600 dark:text-gray-400";
}

function SharedSessionHeader({
  agentStatus,
  error,
  isConnected,
}: Pick<SharedSessionChatViewProps, "agentStatus" | "error" | "isConnected">) {
  return (
    <div className="bg-white dark:bg-gray-900 border-b border-gray-200 dark:border-gray-700 px-3 sm:px-6 py-2 sm:py-3 flex-shrink-0">
      <div className="flex items-center justify-between">
        <div className="flex items-center space-x-2 sm:space-x-4">
          <Link
            href="/chats"
            className="flex items-center space-x-1 sm:space-x-2 px-2 sm:px-3 py-1 sm:py-1.5 text-xs sm:text-sm text-gray-600 dark:text-gray-400 hover:text-gray-900 dark:hover:text-white hover:bg-gray-100 dark:hover:bg-gray-800 rounded-md transition-colors"
            title="Go to Conversations"
          >
            <svg
              className="w-3 h-3 sm:w-4 sm:h-4"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                strokeWidth={2}
                d="M15 19l-7-7 7-7"
              />
            </svg>
            <span className="hidden sm:inline">Conversations</span>
          </Link>
          <div className="flex items-center space-x-2">
            <h2 className="text-base sm:text-lg font-semibold text-gray-900 dark:text-white">
              Shared Session
            </h2>
            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200">
              <svg
                className="w-3 h-3 mr-1"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
                />
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth={2}
                  d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"
                />
              </svg>
              Read-only
            </span>
          </div>
        </div>
        <SessionStatus agentStatus={agentStatus} isConnected={isConnected} />
      </div>
      {error && (
        <div className="mt-4 p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md text-red-700 dark:text-red-400 text-sm">
          {error}
        </div>
      )}
    </div>
  );
}

function SessionStatus({
  agentStatus,
  isConnected,
}: Pick<SharedSessionChatViewProps, "agentStatus" | "isConnected">) {
  return (
    <div className="flex items-center space-x-2 sm:space-x-4">
      {agentStatus && (
        <div className="flex items-center space-x-1 sm:space-x-2">
          <div
            className={`w-2 h-2 rounded-full ${agentStatus.status === "stable" ? "bg-green-500" : "bg-yellow-500"}`}
          />
          <span
            className={`text-xs ${statusTextColor(agentStatus.status)} hidden sm:inline`}
          >
            {agentStatus.status === "stable"
              ? "Agent Available"
              : agentStatus.status === "running"
                ? "Agent Running"
                : agentStatus.status}
          </span>
        </div>
      )}
      <div className="flex items-center space-x-1 sm:space-x-2">
        <div
          className={`w-2 h-2 rounded-full ${isConnected ? "bg-green-500" : "bg-red-500"}`}
        />
        <span
          className={`text-xs sm:text-sm ${isConnected ? "text-green-600 dark:text-green-400" : "text-red-600 dark:text-red-400"} hidden sm:inline`}
        >
          {isConnected ? "Connected" : "Disconnected"}
        </span>
      </div>
    </div>
  );
}

function MessageRow({ message }: { message: SharedSessionMessage }) {
  return (
    <div className="px-4 sm:px-6 py-4">
      <div className="flex items-start space-x-2 sm:space-x-3">
        <div className="flex-shrink-0">
          <div
            className={`w-7 h-7 sm:w-8 sm:h-8 rounded-full flex items-center justify-center ${message.role === "user" ? "bg-blue-500 text-white" : "bg-purple-500 text-white"}`}
          >
            {message.role === "user" ? (
              <svg
                className="w-3 h-3 sm:w-4 sm:h-4"
                fill="currentColor"
                viewBox="0 0 24 24"
              >
                <path d="M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z" />
              </svg>
            ) : (
              <svg
                className="w-3 h-3 sm:w-4 sm:h-4"
                fill="currentColor"
                viewBox="0 0 24 24"
              >
                <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z" />
              </svg>
            )}
          </div>
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center space-x-1 sm:space-x-2 mb-2">
            <span className="text-sm font-medium text-gray-900 dark:text-white">
              {message.role === "user" ? "User" : "AgentAPI"}
            </span>
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {new Date(message.timestamp).toLocaleTimeString()}
            </span>
          </div>
          <div className="prose prose-sm max-w-none text-gray-700 dark:text-gray-300">
            <div className="whitespace-pre-wrap break-words overflow-wrap-anywhere max-w-full">
              {formatTextWithLinks(message.content)}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function EmptySession() {
  return (
    <div className="text-center text-gray-500 dark:text-gray-400 py-12">
      <div className="mb-3">
        <svg
          className="w-12 h-12 mx-auto text-gray-300 dark:text-gray-600"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={1}
            d="M8 12h.01M12 12h.01M16 12h.01M21 12c0 4.418-4.03 8-9 8a9.863 9.863 0 01-4.255-.949L3 20l1.395-3.72C3.512 15.042 3 13.574 3 12c0-4.418 4.03-8 9-8s9 3.582 9 8z"
          />
        </svg>
      </div>
      <p className="text-lg font-medium">No messages yet</p>
      <p className="text-sm mt-1">This shared session has no messages</p>
    </div>
  );
}

function ScrollControls(
  props: Pick<
    SharedSessionChatViewProps,
    | "hasNewMessages"
    | "shouldAutoScroll"
    | "setShouldAutoScroll"
    | "setHasNewMessages"
    | "scrollToBottom"
  >,
) {
  const scroll = (clearNotification: boolean) => {
    props.setShouldAutoScroll(true);
    if (clearNotification) props.setHasNewMessages(false);
    props.scrollToBottom();
  };
  if (props.hasNewMessages) {
    return (
      <div className="absolute bottom-4 left-1/2 transform -translate-x-1/2">
        <button
          onClick={() => scroll(true)}
          className="bg-blue-600 hover:bg-blue-700 text-white px-4 py-2 rounded-full shadow-lg flex items-center space-x-2 transition-colors"
        >
          <span className="text-sm">新しいメッセージ</span>
          <DownArrow className="w-4 h-4" />
        </button>
      </div>
    );
  }
  if (props.shouldAutoScroll) return null;
  return (
    <div className="absolute bottom-4 right-4">
      <button
        onClick={() => scroll(false)}
        className="bg-gray-600 hover:bg-gray-700 text-white p-2 rounded-full shadow-lg transition-colors"
        title="最新メッセージにスクロール"
      >
        <DownArrow className="w-5 h-5" />
      </button>
    </div>
  );
}

function DownArrow({ className }: { className: string }) {
  return (
    <svg
      className={className}
      fill="none"
      stroke="currentColor"
      viewBox="0 0 24 24"
    >
      <path
        strokeLinecap="round"
        strokeLinejoin="round"
        strokeWidth={2}
        d="M19 14l-7 7m0 0l-7-7m7 7V3"
      />
    </svg>
  );
}

function ReadOnlyFooter() {
  return (
    <div className="bg-gray-100 dark:bg-gray-800 border-t border-gray-200 dark:border-gray-700 px-4 sm:px-6 py-3 flex-shrink-0">
      <div className="flex items-center justify-center space-x-2 text-gray-500 dark:text-gray-400">
        <svg
          className="w-5 h-5"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"
          />
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z"
          />
        </svg>
        <span className="text-sm">This is a read-only shared session view</span>
      </div>
    </div>
  );
}

export default function SharedSessionChatView(
  props: SharedSessionChatViewProps,
) {
  return (
    <div
      className="flex flex-col h-full bg-white dark:bg-gray-900"
      style={{ position: "relative", minHeight: 0 }}
    >
      <SharedSessionHeader
        agentStatus={props.agentStatus}
        error={props.error}
        isConnected={props.isConnected}
      />
      <div
        ref={props.messagesContainerRef}
        onScroll={props.handleScroll}
        className="flex-1 overflow-y-auto bg-white dark:bg-gray-900 mobile-scroll min-h-0 relative"
        style={{
          overscrollBehavior: "contain",
          WebkitOverflowScrolling: "touch",
          transform: "translateZ(0)",
        }}
      >
        {props.messages.length === 0 && props.isConnected && <EmptySession />}
        <div className="divide-y divide-gray-200 dark:divide-gray-700">
          {props.messages.map((message) => (
            <MessageRow key={message.id} message={message} />
          ))}
        </div>
        <div ref={props.messagesEndRef} />
        <ScrollControls {...props} />
      </div>
      <ReadOnlyFooter />
    </div>
  );
}
