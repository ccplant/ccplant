import type React from 'react';
import type { SessionMessage } from '../../types/agentapi';
export interface MessageItemProps {
  message: SessionMessage;
  toolResult?: SessionMessage; // 対応するツール結果（オプショナル）
  formatTimestamp: (timestamp: string) => string;
  fontSettings: {
    fontSize: number;
    fontFamily: string;
  };
  isClaudeAgent?: boolean;
}
export interface MessageExpansionProps {
  isExpanded: boolean;
  setIsExpanded: React.Dispatch<React.SetStateAction<boolean>>;
}
export interface MessageThoughtProps {
  isThoughtExpanded: boolean;
  setIsThoughtExpanded: React.Dispatch<React.SetStateAction<boolean>>;
}
