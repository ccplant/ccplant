import { ACPConfigOption, ACPSessionInfo, ACPUserPromptInfo } from '../../lib/agentapi-proxy-client';
import { Session, SessionAnnotations, SessionMessage, SessionMessageListResponse } from '../../types/agentapi';

export const SIDEBAR_VISIBLE_KEY = 'session_list_sidebar_visible';

export interface AgentStatus {
  status: 'stable' | 'running' | 'error';
  last_activity?: string;
  current_task?: string;
  /** Provisioner error message when status is 'error' */
  message?: string;
  error_code?: string;
}

export type MessageSSEConnectionStatus = 'connecting' | 'connected';

export function normalizeAgentStatus(raw: string): 'stable' | 'running' | 'error' {
  if (raw === 'running') return 'running';
  if (raw === 'error') return 'error';
  return 'stable';
}

export function isACPAgentType(agentType?: string | null): boolean {
  return !!agentType && (agentType.includes('acp') || agentType === 'pi' || agentType === 'cursor');
}

export function isValidSessionMessageResponse(response: unknown): response is SessionMessageListResponse {
  return (
    response !== null &&
    typeof response === 'object' &&
    'messages' in response &&
    Array.isArray((response as SessionMessageListResponse).messages)
  );
}

export type RelatedLink = {
  type: 'pr' | 'issue';
  url: string;
};

export function getSessionFromList(sessions: Session[], sessionId: string): Session | undefined {
  return sessions.find((session) => session.session_id === sessionId);
}

export function getRelatedLinks(annotations?: SessionAnnotations): RelatedLink[] {
  return [
    annotations?.pr_url ? { type: 'pr' as const, url: annotations.pr_url } : null,
    annotations?.issue_url ? { type: 'issue' as const, url: annotations.issue_url } : null,
  ].filter((link): link is RelatedLink => Boolean(link));
}

export function getGitHubLinkParts(url: string, type: RelatedLink['type']) {
  const urlParts = url.split('/');
  const marker = type === 'pr' ? 'pull' : 'issues';
  const markerIndex = urlParts.findIndex((part) => part === marker);
  const domain = urlParts[2] || '';
  const number = markerIndex >= 0 ? urlParts[markerIndex + 1] || '' : '';
  const owner = markerIndex >= 2 ? urlParts[markerIndex - 2] || '' : '';
  const repo = markerIndex >= 1 ? urlParts[markerIndex - 1] || '' : '';
  const repoName = owner && repo ? `${owner}/${repo}` : '';
  return { domain, number, repoName, isGitHubCom: domain === 'github.com' };
}

export function formatACPModelValue(value: unknown): string | null {
  if (typeof value === 'string') {
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : null;
  }

  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }

  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null;
  }

  const record = value as Record<string, unknown>;
  const provider = formatACPModelValue(record.provider);
  const direct =
    formatACPModelValue(record.id) ||
    formatACPModelValue(record.model) ||
    formatACPModelValue(record.modelId) ||
    formatACPModelValue(record.name) ||
    formatACPModelValue(record.displayName) ||
    formatACPModelValue(record.value) ||
    formatACPModelValue(record.currentValue) ||
    formatACPModelValue(record.default);

  if (provider && direct && !direct.includes(provider)) {
    return `${provider}/${direct}`;
  }
  return direct;
}

export interface ACPModelOption {
  value: string;
  label: string;
  description?: string;
  group?: string;
}

export function getACPConfigOptionId(option: ACPConfigOption | undefined): string | null {
  if (!option) return null;
  return formatACPModelValue(option.id) || formatACPModelValue(option.key);
}

export function getACPConfigOptionCurrentValue(option: ACPConfigOption | undefined): string | null {
  if (!option) return null;
  return (
    formatACPModelValue(option.currentValue) || formatACPModelValue(option.value) || formatACPModelValue(option.default)
  );
}

export const ACP_MODEL_CONFIG_ID_FALLBACK = 'model';

export const ACP_PROFILE_MODEL_GROUP = 'プロファイルのモデル候補（エージェント未対応の場合は拒否されます）';

export function formatACPModelUpdateError(
  message: string,
  agentOptions: ACPModelOption[],
  rejectedValue?: string,
): string {
  if (!message.includes('-32602')) return message;

  const target = rejectedValue ? `「${rejectedValue}」` : 'このモデル文字列';
  // Only the agent's own options are guaranteed to be accepted. Listing the
  // merged dropdown here used to advertise profile-only values as available.
  const available = agentOptions.map((option) => option.value).filter(Boolean);
  const known = rejectedValue ? agentOptions.some((option) => option.value === rejectedValue) : true;

  const base =
    available.length === 0
      ? `エージェントが${target}を受け付けませんでした（-32602 Invalid params）`
      : `エージェントが${target}を受け付けませんでした。エージェントが切り替え可能なモデル: ${available.join(', ')}`;

  if (known) return base;
  return `${base}。セッションプロファイルのモデル候補はエージェントが提示していない場合、切り替えできません`;
}

export function getACPModelConfigOption(info: ACPSessionInfo | null): ACPConfigOption | null {
  if (!info?.configOptions?.length) return null;

  const byCategory = info.configOptions.find((option) => option.category === 'model');
  if (byCategory) return byCategory;

  return (
    info.configOptions.find((option) => {
      const haystack = [option.id, option.key, option.name, option.description]
        .filter((value): value is string => typeof value === 'string')
        .join(' ')
        .toLowerCase();
      return /\bmodel\b/.test(haystack) || haystack.includes('modelid');
    }) ?? null
  );
}

export function getACPEffortConfigOption(info: ACPSessionInfo | null): ACPConfigOption | null {
  if (!info?.configOptions?.length) return null;

  const byCategory = info.configOptions.find((option) => option.category?.toLowerCase() === 'effort');
  if (byCategory) return byCategory;

  return (
    info.configOptions.find((option) => {
      const haystack = [option.id, option.key, option.name, option.description]
        .filter((value): value is string => typeof value === 'string')
        .join(' ')
        .toLowerCase();
      return /\b(effort|reasoning effort)\b/.test(haystack);
    }) ?? null
  );
}

export function flattenACPModelOptions(options: unknown): ACPModelOption[] {
  if (!Array.isArray(options)) return [];

  const flattened: ACPModelOption[] = [];
  for (const item of options) {
    if (typeof item === 'string') {
      flattened.push({ value: item, label: item });
      continue;
    }

    if (!item || typeof item !== 'object') continue;
    const record = item as Record<string, unknown>;

    if (Array.isArray(record.options)) {
      const groupName = formatACPModelValue(record.name) || formatACPModelValue(record.group) || undefined;
      flattened.push(
        ...flattenACPModelOptions(record.options).map((option) => ({
          ...option,
          group: option.group ?? groupName,
        })),
      );
      continue;
    }

    const value = formatACPModelValue(record.value);
    if (!value) continue;
    flattened.push({
      value,
      label: formatACPModelValue(record.name) || value,
      description: formatACPModelValue(record.description) || undefined,
    });
  }

  return flattened;
}

export function getACPModelDisplay(info: ACPSessionInfo | null): string | null {
  if (!info) return null;

  const direct =
    formatACPModelValue(info.model) ||
    formatACPModelValue(info.modelId) ||
    formatACPModelValue(info.currentModel) ||
    formatACPModelValue(info.selectedModel) ||
    formatACPModelValue(info.modelInfo);

  if (direct) return direct;

  const meta = info._meta;
  const metaModel =
    meta &&
    (formatACPModelValue(meta.model) ||
      formatACPModelValue(meta.modelId) ||
      formatACPModelValue(meta.currentModel) ||
      formatACPModelValue(meta.modelInfo));
  if (metaModel) return metaModel;

  const modelOption = getACPModelConfigOption(info);

  if (!modelOption) return null;
  const currentValue = getACPConfigOptionCurrentValue(modelOption);
  const selected = flattenACPModelOptions(modelOption.options).find((option) => option.value === currentValue);
  return selected ? `${selected.label} (${selected.value})` : currentValue;
}

export interface SessionTokenUsage {
  total: number;
  input?: number;
  output?: number;
  source: 'reported' | 'estimated';
}

export function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value);
}

export function getNumberField(record: Record<string, unknown>, keys: string[]): number | undefined {
  for (const key of keys) {
    const value = record[key];
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string' && value.trim() !== '') {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) return parsed;
    }
  }
  return undefined;
}

export function extractReportedTokenUsage(value: unknown): SessionTokenUsage | null {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return null;

  const record = value as Record<string, unknown>;
  const candidates = [record.usage, record.tokenUsage, record.tokens, record.modelUsage, record._meta, value];

  for (const candidate of candidates) {
    if (!candidate || typeof candidate !== 'object' || Array.isArray(candidate)) continue;
    const usage = candidate as Record<string, unknown>;
    const input = getNumberField(usage, ['inputTokens', 'input_tokens', 'promptTokens', 'prompt_tokens']);
    const output = getNumberField(usage, ['outputTokens', 'output_tokens', 'completionTokens', 'completion_tokens']);
    const total = getNumberField(usage, ['totalTokens', 'total_tokens', 'tokens', 'total']);
    const computedTotal = total ?? (input ?? 0) + (output ?? 0);

    if (computedTotal > 0) {
      return {
        total: computedTotal,
        input,
        output,
        source: 'reported',
      };
    }
  }

  return null;
}

export function estimateMessageTokens(messages: SessionMessage[]): SessionTokenUsage {
  const totalChars = messages.reduce((sum, message) => {
    return sum + (message.content?.length ?? 0) + (message.thought?.length ?? 0);
  }, 0);

  return {
    total: Math.ceil(totalChars / 4),
    source: 'estimated',
  };
}

export function aggregateReportedMessageUsage(messages: SessionMessage[]): SessionTokenUsage | null {
  const total = messages.reduce(
    (sum, message) => {
      const usage = extractReportedTokenUsage(message.metadata);
      if (!usage) return sum;
      return {
        total: sum.total + usage.total,
        input: sum.input + (usage.input ?? 0),
        output: sum.output + (usage.output ?? 0),
        count: sum.count + 1,
      };
    },
    { total: 0, input: 0, output: 0, count: 0 },
  );

  if (total.count === 0 || total.total <= 0) return null;
  return {
    total: total.total,
    input: total.input > 0 ? total.input : undefined,
    output: total.output > 0 ? total.output : undefined,
    source: 'reported',
  };
}

export function getSessionTokenUsage(info: ACPSessionInfo | null, messages: SessionMessage[]): SessionTokenUsage {
  return extractReportedTokenUsage(info) ?? aggregateReportedMessageUsage(messages) ?? estimateMessageTokens(messages);
}

export function getLatestACPUserPromptIndex(prompts: ACPUserPromptInfo[]): number | null {
  return prompts.length > 0 ? prompts[prompts.length - 1].index : null;
}

export function withACPHistoryMessageIds(messages: SessionMessage[], userPromptIndex: number): SessionMessage[] {
  return messages.map((message, index) => ({
    ...message,
    id: -((userPromptIndex + 1) * 100000 + index + 1),
  }));
}

export interface AgentAPIChatProps {
  sessionId?: string;
}
