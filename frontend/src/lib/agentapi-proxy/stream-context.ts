import type { SessionEventsOptions } from '../../types/agentapi';
import type { EventStreamHandlers, EventStreamSettings } from './event-stream';
import type { ProxyOperationContext } from './operation-context';
import type { ACPInitializationSubscription } from './types';

export interface ProxyStreamContext extends ProxyOperationContext {
  readonly baseURL: string;
  subscribeWithEventSource(
    url: string,
    handlers: EventStreamHandlers,
    options?: SessionEventsOptions,
    initialLastEventId?: number,
    initialization?: EventStreamSettings['initialization']
  ): ACPInitializationSubscription;
}
