import { PendingAction, ResourceScope, SessionMessage } from '../../types/agentapi';

export interface CredentialsMetadata {
  name: string;
  has_data: boolean;
  created_at: string;
  updated_at: string;
  files?: Array<{ type: string; path: string; has_data: boolean }>;
}

export interface AntigravityAuthAttempt {
  attempt_id: string;
  status: 'starting' | 'waiting_for_user' | 'authorized' | 'denied' | 'failed' | 'cancelled';
  verification_uri?: string;
  transcript?: string;
  expires_at: string;
  poll_after_ms: number;
  error_code?: string;
  error_message?: string;
}

export type TransferableResourceType = 'webhook' | 'slackbot' | 'session_profile' | 'sandbox_policy';

export interface TransferResourceResult {
  resource_type: TransferableResourceType;
  resource_id: string;
  from: { scope: ResourceScope; user_id?: string; team_id?: string };
  to: { scope: ResourceScope; user_id?: string; team_id?: string };
  status: 'transferred' | 'dry_run';
  dry_run: boolean;
  warnings?: string[];
}

export interface CodexDeviceAuthConfig {
  configured: boolean;
  execution_mode?: 'auth_workload';
}

export interface CodexDeviceAuthStart {
  attempt_id: string;
  status: 'starting' | 'waiting_for_user' | 'authorized' | 'denied' | 'failed' | 'cancelled';
  user_code?: string;
  verification_uri?: string;
  expires_at: string;
  poll_after_ms: number;
}

export interface CodexDeviceAuthStatus {
  status: 'pending' | 'authorized' | 'denied';
}

export interface CodexDeviceAuthTarget {
  scope?: 'user' | 'team';
  team_id?: string;
  replace?: boolean;
}

export interface GitHubUser {
  id: number;
  login: string;
  name?: string;
  email?: string;
  avatarUrl?: string;
}

export interface AgentStatus {
  status: 'stable' | 'running' | 'error';
  last_activity?: string;
  current_task?: string;
  agent_type?: string;
  model?: string;
  /** Provisioner error message when status is 'error' */
  message?: string;
  error_code?: string;
}

export interface ProxySessionStatusEvent {
  session_id: string;
  status: string;
  timestamp: string;
}

export interface CreateSessionSecretResponse {
  secret_id: string;
  expires_at: string;
  local_url: string;
}

export interface SessionSecretMetadata {
  secret_id: string;
  status: 'pending' | 'consumed' | 'expired';
  created_at: string;
  expires_at: string;
  consumed_at?: string;
}

export interface SessionSecretListResponse {
  secrets: SessionSecretMetadata[];
}

export type WaitSessionMessagesResponse = { updated: true; session_id: string; timestamp: string } | { updated: false };

export interface AgentAPIProxyClientConfig {
  baseURL: string;
  apiKey?: string;
  timeout?: number;
  maxSessions?: number;
  sessionTimeout?: number;
  debug?: boolean;
}

export class AgentAPIProxyError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public details?: Record<string, unknown>
  ) {
    super(message);
    this.name = 'AgentAPIProxyError';
  }
}

export interface ACPConfigOption {
  id?: string;
  key?: string;
  name?: string;
  description?: string;
  category?: string;
  type?: string;
  value?: unknown;
  currentValue?: unknown;
  default?: unknown;
  options?: unknown;
}

export interface ACPSessionInfo {
  sessionId: string;
  status: string;
  model?: unknown;
  modelId?: unknown;
  currentModel?: unknown;
  selectedModel?: unknown;
  modelInfo?: unknown;
  configOptions?: ACPConfigOption[];
  _meta?: Record<string, unknown>;
}

export interface ACPPromptContentBlock {
  type: 'text' | 'image';
  text?: string;
  mimeType?: string;
  data?: string;
}

export interface ACPSessionUpdate {
  sessionUpdate: string;
  // agent_message_chunk / user_message_chunk / agent_thought_chunk:
  //   content = { type: "text", text: "..." }
  // tool_call / tool_call_update:
  //   content = [{ type: "content"|"diff"|"terminal", content?: {type,text}, ... }]
  //   (ACP ToolCallContent[] — use extractACPToolContent() to get text)
  content?: unknown;
  messageId?: string;
  // tool_call / tool_call_update
  toolCallId?: string;
  kind?: string;
  /** Human-readable tool title (e.g. "Terminal", "Task", "Find …"). Takes precedence over kind. */
  title?: string;
  status?: string;
  /** File/location hints emitted with tool_call (matches ACP ToolCallInfo.locations). */
  locations?: Array<{ path: string; line?: number }>;
  /** Raw tool input (Anthropic SDK format). May be empty {} on streaming start; full input arrives in subsequent tool_call_update. */
  rawInput?: unknown;
  /** Raw tool output (Anthropic SDK format). Prefer content[] for display; rawOutput may contain non-text blocks. */
  rawOutput?: unknown;
  // plan
  entries?: Array<{ content: string; status: string; priority: string }>;
  // current_mode_update
  modeId?: string;
  // available_commands_update
  availableCommands?: Array<{ name: string; description: string; input?: { hint?: string } }>;
  // config_option_update
  configOptions?: ACPConfigOption[];
}

export interface ACPPermissionOption {
  optionId: string;
  name: string;
  kind?: string;
}

export interface ACPPermissionParams {
  sessionId: string;
  toolCall: { toolCallId: string; kind?: string };
  options: ACPPermissionOption[];
}

export interface ACPJSONRPCMessage {
  jsonrpc: '2.0';
  id?: number | string | null;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: { code: number; message: string };
}

export interface ACPUserPromptInfo {
  index: number;
  preview?: string;
}

export interface ACPMessageHistoryResult {
  messages: SessionMessage[];
  lastEventId?: number;
  userPromptCount: number;
  userPromptIndex?: number;
  userPrompts: ACPUserPromptInfo[];
}

export interface EventSubscription {
  close: () => void;
}

export interface ACPInitializationSubscription extends EventSubscription {
  /** Resolves after the initial SSE handshake succeeds, or false on error/timeout. */
  opened: Promise<boolean>;
  /** Delivers events buffered while history was loading, skipping history-covered IDs. */
  resumeAfterHistory: (lastEventId?: number) => void;
}

export interface ACPSessionCallbacks {
  /** Called for each complete or streaming agent message. */
  onMessage: (message: SessionMessage) => void;
  /** Called with additional text to append to an existing streaming message. */
  onChunk: (messageId: number, text: string) => void;
  /** Called when an image is appended to an existing streaming message. */
  onImageChunk?: (messageId: number, image: { mimeType: string; data: string }) => void;
  /**
   * Called with additional thought text to append to an existing streaming message.
   * The thought is displayed in a collapsible "Thinking…" section.
   */
  onThoughtChunk: (messageId: number, thought: string) => void;
  /**
   * Called when a tool call's status changes to in_progress.
   * The message with matching toolUseId should update its visual state.
   */
  onToolUpdate?: (toolCallId: string, status: string) => void;
  /**
   * Called when a tool call's input is updated (step 2 of the 3-step ACP sequence).
   * The message with matching toolUseId should patch its input/title/locations.
   */
  onToolInputUpdate?: (
    toolCallId: string,
    input: unknown,
    title?: string,
    locations?: Array<{ path: string; line?: number }>
  ) => void;
  /**
   * Called when the agent switches to a different mode (current_mode_update).
   */
  onModeUpdate?: (modeId: string) => void;
  /**
   * Called when the agent advertises its slash-command list (available_commands_update).
   */
  onCommandsUpdate?: (commands: Array<{ name: string; description: string; hint?: string }>) => void;
  /**
   * Called when the agent reports updated session configuration options.
   */
  onConfigOptionsUpdate?: (configOptions: ACPConfigOption[]) => void;
  /** Called when a permission request arrives from the agent. */
  onPermission: (action: PendingAction, rpcId: number) => void;
  /** Called when the message SSE stream opens. */
  onConnectionOpen?: () => void;
  /** Called when the message SSE stream is connecting or retrying. */
  onConnectionConnecting?: (attempt?: number, delayMs?: number) => void;
  /** Called when the message SSE stream closes permanently. */
  onConnectionClosed?: () => void;
  /** Called on transport errors. */
  onError: (error: Error) => void;
}

export interface HerdrTerminalStatus {
  state: string;
  title?: string;
  msg?: string;
}

export interface HerdrTerminal {
  address: string;
  name?: string | null;
  title?: string;
  state: string;
  cwd?: string;
  argv?: string[];
  status?: HerdrTerminalStatus | null;
}

export interface HerdrRPCResponse<T> {
  jsonrpc: '2.0';
  id: number;
  result?: T;
  error?: { code: number; message: string };
}
