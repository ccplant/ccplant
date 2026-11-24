import type { SessionEventsOptions } from '../../types/agentapi';
import type { ACPInitializationSubscription } from './types';

export interface EventStreamHandlers {
  onOpen?: () => void;
  onMessage: (event: MessageEvent) => void;
  onTransientError?: (event: Event, state: number) => void;
  onReconnect?: (attempt: number, delayMs: number) => void;
  onClosed?: (event: Event, state: number) => void;
}

export interface EventStreamSettings {
  options?: SessionEventsOptions;
  initialLastEventId?: number;
  initialization?: { deferMessages: boolean; handshakeTimeoutMs: number };
}

export function subscribeToEventStream(
  url: string,
  handlers: EventStreamHandlers,
  settings: EventStreamSettings
): ACPInitializationSubscription {
  return new EventStream(url, handlers, settings).start();
}

class EventStream {
  private source: EventSource | null = null;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private stableTimer: ReturnType<typeof setTimeout> | null = null;
  private handshakeTimer: ReturnType<typeof setTimeout> | null = null;
  private closed = false;
  private reconnectAttempts = 0;
  private lastEventId: number | undefined;
  private deferred: boolean;
  private readonly deferredEvents: MessageEvent[] = [];
  private settleOpened: ((opened: boolean) => void) | null = null;
  private readonly opened: Promise<boolean>;
  private readonly baseDelay: number;

  constructor(
    private readonly url: string,
    private readonly handlers: EventStreamHandlers,
    private readonly settings: EventStreamSettings
  ) {
    this.lastEventId = settings.initialLastEventId;
    this.deferred = settings.initialization?.deferMessages === true;
    this.baseDelay = settings.options?.reconnectInterval ?? 1000;
    this.opened = new Promise((resolve) => {
      this.settleOpened = resolve;
    });
    if (settings.initialization) {
      this.handshakeTimer = setTimeout(() => {
        this.settleOpened?.(false);
        this.settleOpened = null;
      }, settings.initialization.handshakeTimeoutMs);
    }
  }

  start(): ACPInitializationSubscription {
    this.connect();
    return {
      close: () => this.close(),
      opened: this.opened,
      resumeAfterHistory: (cursor) => this.resumeAfterHistory(cursor),
    };
  }

  private settleHandshake(value: boolean): void {
    if (!this.settleOpened) return;
    if (this.handshakeTimer) clearTimeout(this.handshakeTimer);
    this.settleOpened(value);
    this.settleOpened = null;
  }

  private clearReconnectTimer(): void {
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
    this.reconnectTimer = null;
  }

  private clearStableTimer(): void {
    if (this.stableTimer) clearTimeout(this.stableTimer);
    this.stableTimer = null;
  }

  private scheduleReconnect(event: Event, state: number): void {
    if (this.closed || this.settings.options?.reconnect === false) {
      this.handlers.onClosed?.(event, state);
      return;
    }
    this.reconnectAttempts++;
    const maximum = this.settings.options?.maxReconnectAttempts;
    if (maximum && this.reconnectAttempts > maximum) {
      this.handlers.onClosed?.(event, state);
      return;
    }
    const delay = Math.min(this.baseDelay * Math.pow(2, this.reconnectAttempts - 1), 30000);
    this.handlers.onReconnect?.(this.reconnectAttempts, delay);
    this.clearReconnectTimer();
    this.reconnectTimer = setTimeout(() => this.connect(), delay);
  }

  private connect(): void {
    if (this.closed) return;
    const url =
      this.lastEventId === undefined
        ? this.url
        : `${this.url}${this.url.includes('?') ? '&' : '?'}lastEventId=${encodeURIComponent(String(this.lastEventId))}`;
    const source = new EventSource(url);
    this.source = source;
    source.onopen = () => this.onOpen(source);
    source.onmessage = (event) => this.onMessage(source, event);
    source.onerror = (event) => this.onError(source, event);
    source.addEventListener('agentapi_upstream_error', (event) => this.onUpstreamError(source, event));
  }

  private onOpen(source: EventSource): void {
    if (source !== this.source || this.closed) return;
    // Eager BFF responses precede the upstream handshake. Reset backoff only
    // after the connection has remained healthy for the full interval.
    this.clearStableTimer();
    this.stableTimer = setTimeout(() => {
      this.reconnectAttempts = 0;
      this.stableTimer = null;
    }, 30000);
    this.settleHandshake(true);
    this.handlers.onOpen?.();
  }

  private onUpstreamError(source: EventSource, event: Event): void {
    if (source !== this.source || this.closed) return;
    this.clearStableTimer();
    this.settleHandshake(false);
    source.close();
    this.source = null;
    let retryable = true;
    let retryAfter: number | undefined;
    try {
      const payload = JSON.parse((event as MessageEvent).data) as { retryable?: boolean; retryAfterMs?: number };
      retryable = payload.retryable !== false;
      retryAfter = payload.retryAfterMs;
    } catch (error) {
      console.warn('Malformed upstream stream error; using regular retry backoff:', error);
    }
    if (!retryable) {
      this.handlers.onClosed?.(event, EventSource.CLOSED);
      return;
    }
    if (retryAfter !== undefined) {
      this.reconnectAttempts = Math.max(
        this.reconnectAttempts,
        Math.ceil(Math.log2(Math.max(1, retryAfter / this.baseDelay)))
      );
    }
    this.scheduleReconnect(event, EventSource.CLOSED);
  }

  private onMessage(source: EventSource, event: MessageEvent): void {
    if (source !== this.source || this.closed) return;
    if (event.lastEventId !== '') {
      const id = Number(event.lastEventId);
      if (Number.isSafeInteger(id)) {
        if (this.lastEventId !== undefined && id <= this.lastEventId) return;
        this.lastEventId = id;
      }
    }
    if (this.deferred) this.deferredEvents.push(event);
    else this.handlers.onMessage(event);
  }

  private onError(source: EventSource, event: Event): void {
    if (source !== this.source || this.closed) return;
    this.settleHandshake(false);
    const state = source.readyState;
    if (state === EventSource.CLOSED) {
      source.close();
      this.source = null;
      this.scheduleReconnect(event, state);
      return;
    }
    this.handlers.onTransientError?.(event, state);
  }

  private close(): void {
    this.closed = true;
    this.settleHandshake(false);
    this.clearStableTimer();
    this.clearReconnectTimer();
    this.source?.close();
    this.source = null;
  }

  private resumeAfterHistory(cursor?: number): void {
    if (!this.deferred) return;
    this.deferred = false;
    for (const event of this.deferredEvents.splice(0)) {
      const id = Number(event.lastEventId);
      if (cursor !== undefined && Number.isSafeInteger(id) && id <= cursor) continue;
      this.handlers.onMessage(event);
    }
  }
}
