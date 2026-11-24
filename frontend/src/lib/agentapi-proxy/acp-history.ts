import { SessionMessage } from '../../types/agentapi';
import { ACPJSONRPCMessage, ACPSessionUpdate } from './types';

export const ACP_KIND_TO_NAME: Record<string, string> = {
  execute: 'Bash',
  read: 'Read',
  write: 'Write',
  edit: 'Edit',
  search: 'Search',
  glob: 'Glob',
  other: 'Tool',
  think: 'Think',
  task: 'Task',
  browse: 'Browser',
};

export function titleToToolName(title: string | undefined): string | undefined {
  if (!title) return undefined;
  const match = title.match(/^[A-Za-z][A-Za-z0-9_-]*/);
  if (!match) return undefined;
  return match[0];
}

export function acpToolDisplayName(kind: string | undefined, title: string | undefined): string {
  if (kind && ACP_KIND_TO_NAME[kind]) return ACP_KIND_TO_NAME[kind];
  return titleToToolName(title) || kind || 'tool';
}

export function acpToolNameFromRawInput(rawInput: unknown): string | undefined {
  if (!rawInput || typeof rawInput !== 'object' || Array.isArray(rawInput)) return undefined;
  const toolName = (rawInput as Record<string, unknown>)._toolName;
  if (typeof toolName !== 'string' || toolName.length === 0) return undefined;
  return ACP_KIND_TO_NAME[toolName] || titleToToolName(toolName) || toolName;
}

export function acpExtractText(content: unknown): string {
  if (!content || typeof content !== 'object' || Array.isArray(content)) return '';
  const obj = content as Record<string, unknown>;
  if (obj.type === 'text' && typeof obj.text === 'string') return obj.text;
  return '';
}

export function acpExtractImage(content: unknown): { mimeType: string; data: string } | null {
  if (!content || typeof content !== 'object' || Array.isArray(content)) return null;
  const obj = content as Record<string, unknown>;
  if (obj.type === 'image' && typeof obj.mimeType === 'string' && typeof obj.data === 'string') {
    return { mimeType: obj.mimeType, data: obj.data };
  }
  return null;
}

export function extractACPToolContent(content: unknown): string {
  if (!content || !Array.isArray(content)) return '';
  const texts: string[] = [];
  for (const item of content) {
    if (!item || typeof item !== 'object') continue;
    const obj = item as Record<string, unknown>;
    if (obj.type === 'content') {
      // { type: "content", content: { type: "text", text: "..." } }
      const inner = obj.content as Record<string, unknown> | undefined;
      if (inner && typeof inner.text === 'string') texts.push(inner.text);
    } else if (obj.type === 'diff') {
      // { type: "diff", path: "...", newText: "..." }
      if (typeof obj.newText === 'string') texts.push(obj.newText);
    }
    // "terminal" type: skip (has no text)
  }
  return texts.join('\n');
}

export function extractACPToolImages(content: unknown): Array<{ mimeType: string; data: string }> {
  if (!Array.isArray(content)) return [];
  return content.flatMap((item) => {
    if (!item || typeof item !== 'object') return [];
    const block = item as Record<string, unknown>;
    if (block.type !== 'content' || !block.content || typeof block.content !== 'object') return [];
    const image = acpExtractImage(block.content);
    return image ? [image] : [];
  });
}

export function extractRawOutputText(rawOutput: unknown): string {
  if (rawOutput == null) return '';
  if (typeof rawOutput === 'string') return rawOutput;
  if (Array.isArray(rawOutput)) {
    return rawOutput
      .map((item) => {
        if (typeof item === 'string') return item;
        if (item && typeof (item as Record<string, unknown>).text === 'string') {
          return (item as Record<string, unknown>).text as string;
        }
        return JSON.stringify(item);
      })
      .join('\n');
  }
  // Handle single ContentBlock object {type: "text", text: "..."} or {text: "..."}
  const obj = rawOutput as Record<string, unknown>;
  if (typeof obj.text === 'string') {
    return obj.text;
  }
  if (typeof obj.content === 'string') {
    return obj.content;
  }
  const outputParts: string[] = [];
  if (typeof obj.stdout === 'string' && obj.stdout.length > 0) {
    outputParts.push(obj.stdout);
  }
  if (typeof obj.stderr === 'string' && obj.stderr.length > 0) {
    outputParts.push(obj.stderr);
  }
  if (outputParts.length > 0) {
    if (typeof obj.exitCode === 'number') {
      outputParts.push(`exit code: ${obj.exitCode}`);
    }
    return outputParts.join('\n');
  }
  return JSON.stringify(rawOutput);
}

export function acpPlanToMarkdown(entries: NonNullable<ACPSessionUpdate['entries']>): string {
  let md = '## Plan\n\n';
  for (const e of entries) {
    const marker =
      e.status === 'completed'
        ? '- [x]'
        : e.status === 'in_progress'
          ? '- [~]'
          : e.status === 'cancelled'
            ? '- [!]'
            : '- [ ]';
    const priority = e.priority === 'high' ? ' 🔴' : e.priority === 'low' ? ' 🔵' : '';
    md += `${marker} ${e.content}${priority}\n`;
  }
  return md;
}

export function parseACPJSONRPCMessages(rawMsgs: ACPJSONRPCMessage[]): SessionMessage[] {
  return new ACPHistoryParser().parse(rawMsgs);
}

class ACPHistoryParser {
  private readonly messages: SessionMessage[] = [];
  private nextId = Date.now();
  private streamingId: number | null = null;

  parse(rawMsgs: ACPJSONRPCMessage[]): SessionMessage[] {
    for (const message of rawMsgs) this.consume(message);
    return this.messages;
  }

  private consume(message: ACPJSONRPCMessage): void {
    if (message.method === 'session/update') {
      const params = message.params as { sessionId: string; update: ACPSessionUpdate; time?: string };
      if (params?.update) this.applyUpdate(params.update, params.time || new Date().toISOString());
    } else if (message.result != null || message.error) {
      this.streamingId = null;
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
      default:
        this.streamingId = null;
    }
  }

  private appendAgentChunk(update: ACPSessionUpdate, time: string): void {
    const content = acpExtractText(update.content);
    const image = acpExtractImage(update.content);
    if (!content && !image) return;
    if (this.streamingId === null) {
      this.streamingId = this.nextId++;
      this.messages.push({
        id: this.streamingId,
        role: 'agent',
        content,
        images: image ? [image] : undefined,
        time,
        type: 'normal',
      });
      return;
    }
    const index = this.messages.findIndex((message) => message.id === this.streamingId);
    if (index < 0) return;
    const message = this.messages[index];
    this.messages[index] = {
      ...message,
      content: message.content + content,
      images: image ? [...(message.images ?? []), image] : message.images,
    };
  }

  private appendThought(update: ACPSessionUpdate, time: string): void {
    const thought = acpExtractText(update.content);
    if (!thought) return;
    if (this.streamingId === null) {
      this.streamingId = this.nextId++;
      this.messages.push({ id: this.streamingId, role: 'agent', content: '', thought, time, type: 'normal' });
      return;
    }
    const index = this.messages.findIndex((message) => message.id === this.streamingId);
    if (index >= 0)
      this.messages[index] = { ...this.messages[index], thought: (this.messages[index].thought ?? '') + thought };
  }

  private appendToolCall(update: ACPSessionUpdate, time: string): void {
    this.streamingId = null;
    const tool = {
      type: 'tool_use',
      name: acpToolNameFromRawInput(update.rawInput) || acpToolDisplayName(update.kind, update.title),
      id: update.toolCallId,
      input: update.rawInput ?? {},
      title: update.title,
      locations: update.locations,
    };
    this.messages.push({
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
    if (update.status === 'running' || update.status === 'in_progress') return;
    if (!success && !error) {
      this.updateToolInput(update);
      return;
    }
    const images = extractACPToolImages(update.content);
    this.messages.push({
      id: this.nextId++,
      role: 'tool_result',
      content: extractACPToolContent(update.content) || extractRawOutputText(update.rawOutput),
      images: images.length ? images : undefined,
      time,
      type: 'normal',
      parentToolUseId: update.toolCallId,
      status: error ? 'error' : 'success',
    });
  }

  private updateToolInput(update: ACPSessionUpdate): void {
    if (update.rawInput == null || !update.toolCallId) return;
    let index = this.messages.length - 1;
    while (index >= 0 && this.messages[index].toolUseId !== update.toolCallId) index--;
    if (index < 0) return;
    try {
      const tool = JSON.parse(this.messages[index].content);
      tool.input = update.rawInput;
      if (update.title) tool.title = update.title;
      if (update.locations) tool.locations = update.locations;
      this.messages[index] = { ...this.messages[index], content: JSON.stringify(tool) };
    } catch (error) {
      console.warn('Leaving malformed ACP tool history entry unchanged:', error);
    }
  }

  private appendPlan(update: ACPSessionUpdate, time: string): void {
    this.streamingId = null;
    if (!update.entries?.length) return;
    this.messages.push({
      id: this.nextId++,
      role: 'agent',
      content: acpPlanToMarkdown(update.entries),
      time,
      type: 'plan',
    });
  }

  private appendUserChunk(update: ACPSessionUpdate, time: string): void {
    const content = acpExtractText(update.content);
    const image = acpExtractImage(update.content);
    if (!content && !image) return;
    const previous = this.messages[this.messages.length - 1];
    if (image && previous?.role === 'user') {
      previous.images = [...(previous.images ?? []), image];
    } else {
      this.messages.push({
        id: this.nextId++,
        role: 'user',
        content,
        images: image ? [image] : undefined,
        time,
        type: 'normal',
      });
    }
  }
}
