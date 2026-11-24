import type { PendingAction } from '../../types/agentapi';
import {
  acpExtractImage,
  acpExtractText,
  acpPlanToMarkdown,
  acpToolDisplayName,
  acpToolNameFromRawInput,
  extractACPToolContent,
  extractACPToolImages,
  extractRawOutputText,
} from './acp-history';
import type { ACPJSONRPCMessage, ACPPermissionParams, ACPSessionCallbacks, ACPSessionUpdate } from './types';

export class ACPEventsDecoder {
  private nextId = 1;
  private agentMessageId: number | null = null;
  private userMessageId: number | null = null;

  constructor(
    private readonly callbacks: ACPSessionCallbacks,
    private readonly debugEnabled: () => boolean
  ) {}

  consume(event: MessageEvent): void {
    try {
      const message: ACPJSONRPCMessage = JSON.parse(event.data);
      if (this.debugEnabled()) console.log('[AgentAPIProxy] ACP SSE message:', message);
      this.consumeMessage(message);
    } catch (error) {
      if (this.debugEnabled()) console.error('[AgentAPIProxy] ACP SSE parse error:', error);
      this.callbacks.onError(error instanceof Error ? error : new Error('Failed to parse ACP SSE message'));
    }
  }

  private consumeMessage(message: ACPJSONRPCMessage): void {
    if (message.method === 'session/update') {
      const params = message.params as { sessionId: string; update: ACPSessionUpdate; time?: string };
      if (params?.update) this.applyUpdate(params.update, params.time || new Date().toISOString());
      return;
    }
    if (message.method === 'session/request_permission' && message.id != null) {
      this.requestPermission(message);
      return;
    }
    if (message.result != null && message.id != null) {
      const reason = (message.result as { stopReason?: unknown })?.stopReason;
      // Permission replies are not turn boundaries. Only prompt results carry a stop reason.
      if (typeof reason === 'string' && reason.length > 0) this.finishTurn();
      return;
    }
    if (message.error && message.id != null) {
      this.finishTurn();
      this.callbacks.onError(new Error(message.error.message));
    }
  }

  private applyUpdate(update: ACPSessionUpdate, time: string): void {
    switch (update.sessionUpdate) {
      case 'agent_message_chunk':
        this.appendAgentChunk(update, time);
        break;
      case 'agent_thought_chunk':
        this.appendThought(update, time);
        break;
      case 'tool_call':
        this.appendToolCall(update, time);
        break;
      case 'tool_call_update':
        this.applyToolUpdate(update, time);
        break;
      case 'plan':
        this.appendPlan(update, time);
        break;
      case 'user_message_chunk':
        this.appendUserChunk(update, time);
        break;
      case 'current_mode_update':
        if (update.modeId) this.callbacks.onModeUpdate?.(update.modeId);
        break;
      case 'available_commands_update':
        this.updateCommands(update);
        break;
      case 'config_option_update':
        if (Array.isArray(update.configOptions)) this.callbacks.onConfigOptionsUpdate?.(update.configOptions);
        break;
      case 'agent_turn_end':
        this.finishTurn();
        break;
    }
  }

  private appendAgentChunk(update: ACPSessionUpdate, time: string): void {
    const content = acpExtractText(update.content),
      image = acpExtractImage(update.content);
    if (!content && !image) return;
    if (this.agentMessageId !== null) {
      if (content) this.callbacks.onChunk(this.agentMessageId, content);
      if (image) this.callbacks.onImageChunk?.(this.agentMessageId, image);
      return;
    }
    this.agentMessageId = this.nextId++;
    this.callbacks.onMessage({
      id: this.agentMessageId,
      role: 'agent',
      content,
      images: image ? [image] : undefined,
      time,
      type: 'normal',
    });
  }

  private appendThought(update: ACPSessionUpdate, time: string): void {
    const thought = acpExtractText(update.content);
    if (!thought) return;
    if (this.agentMessageId === null) {
      this.agentMessageId = this.nextId++;
      this.callbacks.onMessage({ id: this.agentMessageId, role: 'agent', content: '', thought, time, type: 'normal' });
    } else this.callbacks.onThoughtChunk(this.agentMessageId, thought);
  }

  private appendToolCall(update: ACPSessionUpdate, time: string): void {
    this.agentMessageId = null;
    const tool = {
      type: 'tool_use',
      name: acpToolNameFromRawInput(update.rawInput) || acpToolDisplayName(update.kind, update.title),
      id: update.toolCallId,
      input: update.rawInput ?? {},
      title: update.title,
      locations: update.locations,
    };
    this.callbacks.onMessage({
      id: this.nextId++,
      role: 'agent',
      content: JSON.stringify(tool),
      time,
      type: 'normal',
      toolUseId: update.toolCallId,
    });
  }

  private applyToolUpdate(update: ACPSessionUpdate, time: string): void {
    const success = update.status === 'completed' || update.status === 'success';
    const error = update.status === 'failed' || update.status === 'error';
    if (update.status === 'running' || update.status === 'in_progress') {
      this.callbacks.onToolUpdate?.(update.toolCallId ?? '', 'in_progress');
      return;
    }
    if (!success && !error) {
      if (update.rawInput != null && update.toolCallId)
        this.callbacks.onToolInputUpdate?.(update.toolCallId, update.rawInput, update.title, update.locations);
      return;
    }
    const images = extractACPToolImages(update.content);
    this.callbacks.onMessage({
      id: this.nextId++,
      role: 'tool_result',
      content: extractACPToolContent(update.content) || extractRawOutputText(update.rawOutput),
      images: images.length ? images : undefined,
      time,
      type: 'normal',
      parentToolUseId: update.toolCallId,
      status: success ? 'success' : 'error',
    });
  }

  private appendPlan(update: ACPSessionUpdate, time: string): void {
    this.agentMessageId = null;
    if (!update.entries?.length) return;
    this.callbacks.onMessage({
      id: this.nextId++,
      role: 'agent',
      content: acpPlanToMarkdown(update.entries),
      time,
      type: 'plan',
    });
  }

  private appendUserChunk(update: ACPSessionUpdate, time: string): void {
    const content = acpExtractText(update.content),
      image = acpExtractImage(update.content);
    if (!content && !image) return;
    if (image && this.userMessageId !== null) this.callbacks.onImageChunk?.(this.userMessageId, image);
    else {
      this.userMessageId = this.nextId++;
      this.callbacks.onMessage({
        id: this.userMessageId,
        role: 'user',
        content,
        images: image ? [image] : undefined,
        time,
        type: 'normal',
      });
    }
  }

  private updateCommands(update: ACPSessionUpdate): void {
    if (!Array.isArray(update.availableCommands)) return;
    this.callbacks.onCommandsUpdate?.(
      update.availableCommands.map((command) => ({
        name: command.name,
        description: command.description,
        hint: command.input?.hint,
      }))
    );
  }

  private requestPermission(message: ACPJSONRPCMessage): void {
    this.agentMessageId = null;
    const params = message.params as ACPPermissionParams;
    const action: PendingAction = {
      type: 'answer_question',
      tool_use_id: params.toolCall?.toolCallId ?? '',
      content: {
        questions: [
          {
            question: 'Permission required',
            header: 'Permission Required',
            options: (params.options ?? []).map((option) => ({
              label: option.name || option.optionId,
              description: option.kind ?? '',
            })),
            multiSelect: false,
          },
        ],
      },
    };
    this.callbacks.onPermission(action, message.id as number);
  }

  private finishTurn(): void {
    this.agentMessageId = null;
    this.userMessageId = null;
  }
}
