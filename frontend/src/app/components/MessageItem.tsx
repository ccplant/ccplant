'use client';
import { memo, useState } from 'react';
import { parseToolUseContent } from './messageContent';
import type { MessageItemProps } from './messageItemTypes';
import { NormalMessage } from './NormalMessage';
import { QuestionMessage } from './QuestionMessage';
import { ToolResultMessage } from './ToolResultMessage';
import { ToolUseMessage } from './ToolUseMessage';
function MessageItem(props: MessageItemProps) {
  const { message } = props;
  const [isExpanded, setIsExpanded] = useState(false);
  const [isThoughtExpanded, setIsThoughtExpanded] = useState(false);
  if (message.role === 'agent' && message.toolUseId) {
    const toolUse = parseToolUseContent(message.content);
    if (toolUse)
      return <ToolUseMessage {...props} toolUse={toolUse} isExpanded={isExpanded} setIsExpanded={setIsExpanded} />;
  }
  if (message.role === 'tool_result')
    return <ToolResultMessage {...props} isExpanded={isExpanded} setIsExpanded={setIsExpanded} />;
  if (message.type === 'question') return <QuestionMessage {...props} />;
  return <NormalMessage {...props} isThoughtExpanded={isThoughtExpanded} setIsThoughtExpanded={setIsThoughtExpanded} />;
}
function areEqual(prevProps: MessageItemProps, nextProps: MessageItemProps): boolean {
  // Compare message object - only the fields that affect rendering
  const messageEqual =
    prevProps.message.id === nextProps.message.id &&
    prevProps.message.role === nextProps.message.role &&
    prevProps.message.content === nextProps.message.content &&
    prevProps.message.thought === nextProps.message.thought &&
    JSON.stringify(prevProps.message.images) === JSON.stringify(nextProps.message.images) &&
    prevProps.message.type === nextProps.message.type &&
    prevProps.message.toolUseId === nextProps.message.toolUseId &&
    prevProps.message.parentToolUseId === nextProps.message.parentToolUseId &&
    prevProps.message.timestamp === nextProps.message.timestamp &&
    prevProps.message.time === nextProps.message.time &&
    prevProps.message.status === nextProps.message.status;

  // Compare toolResult if present
  const toolResultEqual =
    prevProps.toolResult?.id === nextProps.toolResult?.id &&
    prevProps.toolResult?.content === nextProps.toolResult?.content &&
    prevProps.toolResult?.status === nextProps.toolResult?.status &&
    JSON.stringify(prevProps.toolResult?.images) === JSON.stringify(nextProps.toolResult?.images);

  // Compare fontSettings
  const fontSettingsEqual =
    prevProps.fontSettings.fontSize === nextProps.fontSettings.fontSize &&
    prevProps.fontSettings.fontFamily === nextProps.fontSettings.fontFamily;

  // Compare other props
  const otherPropsEqual = prevProps.isClaudeAgent === nextProps.isClaudeAgent;

  // formatTimestamp is a function - we assume it's stable
  // (they should be wrapped with useCallback in the parent component)

  return messageEqual && toolResultEqual && fontSettingsEqual && otherPropsEqual;
}
export default memo(MessageItem, areEqual);
