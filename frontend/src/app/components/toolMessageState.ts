import {
  getToolBriefInfo,
  hasObjectEntries,
  parseToolResultContent,
  visibleToolInput,
  type ToolUseContent,
} from './messageContent';
import type { MessageExpansionProps, MessageItemProps } from './messageItemTypes';
export interface ToolUseMessageProps extends MessageItemProps, MessageExpansionProps {
  toolUse: ToolUseContent;
}
export function buildToolMessageState(props: ToolUseMessageProps) {
  const { toolUse, toolResult } = props;
  const result = toolResult ? parseToolResultContent(toolResult.content) : null;
  const isSuccess = toolResult?.status === 'success';
  const hasResult = !!toolResult;
  const briefInfo = getToolBriefInfo(toolUse, result, toolResult?.content);
  const inputForDisplay = visibleToolInput(toolUse.input);
  const hasInput = hasObjectEntries(inputForDisplay);
  return { ...props, result, isSuccess, hasResult, briefInfo, inputForDisplay, hasInput };
}
export type ToolMessageState = ReturnType<typeof buildToolMessageState>;
