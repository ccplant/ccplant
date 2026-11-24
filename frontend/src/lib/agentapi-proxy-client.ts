import { CreateSessionRequest, SendSessionMessageRequest, Session, SessionMessage } from '../types/agentapi';
import { SessionPoolSupplier } from '../types/session_pool';
import { addRepositoryToHistory, getDefaultProxySettings, loadFullGlobalSettings } from '../types/settings';
import { aCPOperations } from './agentapi-proxy/acp';
import { acpEventsOperations } from './agentapi-proxy/acp-events';
import { apiTokensOperations } from './agentapi-proxy/api-tokens';
import { credentialsOperations } from './agentapi-proxy/credentials';
import { externalManagersOperations } from './agentapi-proxy/external-managers';
import { filesOperations } from './agentapi-proxy/files';
import { integrationsOperations } from './agentapi-proxy/integrations';
import { legacyEventsOperations } from './agentapi-proxy/legacy-events';
import { poolsOperations } from './agentapi-proxy/pools';
import { sandboxOperations } from './agentapi-proxy/sandbox';
import { schedulesOperations } from './agentapi-proxy/schedules';
import { sessionActionsOperations } from './agentapi-proxy/session-actions';
import { sessionProfilesOperations } from './agentapi-proxy/session-profiles';
import { sessionSecretsOperations } from './agentapi-proxy/session-secrets';
import { sessionsOperations } from './agentapi-proxy/sessions';
import { settingsOperations } from './agentapi-proxy/settings';
import { sharingOperations } from './agentapi-proxy/sharing';
import { slackbotsOperations } from './agentapi-proxy/slackbots';
import { storagesOperations } from './agentapi-proxy/storages';
import { teamsOperations } from './agentapi-proxy/teams';
import { AgentAPIProxyTransport } from './agentapi-proxy/transport';
import { debugAllowed, defaultClientDebugEnabled } from './agentapi-proxy/transport-support';
import {
  ACPInitializationSubscription,
  ACPSessionCallbacks,
  AgentAPIProxyClientConfig,
  AgentAPIProxyError,
  HerdrTerminal,
} from './agentapi-proxy/types';
import { webhooksOperations } from './agentapi-proxy/webhooks';
import { workspacesOperations } from './agentapi-proxy/workspaces';
export * from './agentapi-proxy/types';

const proxyOperations = {
  ...acpEventsOperations,
  ...legacyEventsOperations,
  ...aCPOperations,
  ...apiTokensOperations,
  ...credentialsOperations,
  ...externalManagersOperations,
  ...filesOperations,
  ...integrationsOperations,
  ...poolsOperations,
  ...sandboxOperations,
  ...schedulesOperations,
  ...sessionActionsOperations,
  ...sessionProfilesOperations,
  ...sessionSecretsOperations,
  ...sessionsOperations,
  ...settingsOperations,
  ...sharingOperations,
  ...slackbotsOperations,
  ...storagesOperations,
  ...teamsOperations,
  ...webhooksOperations,
  ...workspacesOperations,
};
type ProxyOperations = typeof proxyOperations;

class ProxyClientCore extends AgentAPIProxyTransport {
  declare subscribeToACPSessionEvents: ProxyOperations['subscribeToACPSessionEvents'];
  declare start: ProxyOperations['start'];
  declare delete: ProxyOperations['delete'];
  declare listSessionPools: ProxyOperations['listSessionPools'];
  constructor(config: AgentAPIProxyClientConfig) {
    super(config);
  }

  async sendSessionMessage(
    sessionId: string,
    data: SendSessionMessageRequest,
    retryCount = 0
  ): Promise<SessionMessage> {
    const maxRetries = 2;

    try {
      if (this.debug) {
        console.log(`[AgentAPIProxy] Sending message to session ${sessionId} (attempt ${retryCount + 1})`);
      }

      const result = await this.makeRequest<SessionMessage>(`/${sessionId}/message`, {
        method: 'POST',
        body: JSON.stringify(data),
      });

      if (this.debug) {
        console.log(`[AgentAPIProxy] Successfully sent message to session ${sessionId}`);
      }

      return result;
    } catch (error) {
      if (this.debug) {
        console.error(
          `[AgentAPIProxy] Failed to send message to session ${sessionId} (attempt ${retryCount + 1}):`,
          error
        );
      }

      // Check if this is a timeout or screen stabilization error
      const isTimeoutError =
        error instanceof AgentAPIProxyError &&
        (error.message.includes('timeout') ||
          error.message.includes('screen to stabilize') ||
          error.message.includes('wait for condition'));

      // Retry for timeout errors
      if (isTimeoutError && retryCount < maxRetries) {
        const delay = (retryCount + 1) * 2000; // 2s, 4s delay
        if (this.debug) {
          console.log(`[AgentAPIProxy] Retrying message send in ${delay}ms...`);
        }

        await new Promise((resolve) => setTimeout(resolve, delay));
        return this.sendSessionMessage(sessionId, data, retryCount + 1);
      }

      // Enhance error message for timeout errors
      if (isTimeoutError) {
        throw new AgentAPIProxyError(
          error instanceof AgentAPIProxyError ? error.status : 500,
          'TIMEOUT_ERROR',
          'メッセージ送信がタイムアウトしました。画面の操作に時間がかかっている可能性があります。しばらく待ってからもう一度お試しください。',
          error instanceof AgentAPIProxyError ? error.details : undefined
        );
      }

      throw error;
    }
  }

  /**
   * Start multiple sessions in parallel
   */
  async startBatch(
    count: number,
    sessionData?: Partial<CreateSessionRequest> | Record<string, unknown>
  ): Promise<Session[]> {
    const promises = Array.from({ length: count }, () => this.start(sessionData));
    return Promise.all(promises);
  }

  /**
   * Delete multiple sessions in parallel
   */
  async deleteBatch(sessionIds: string[]): Promise<void> {
    const promises = sessionIds.map((sessionId) => this.delete(sessionId));
    await Promise.all(promises);
  }

  /**
   * Set debug mode
   */
  setDebug(debug: boolean): void {
    this.debug = debugAllowed(debug);
  }

  async listHerdrTerminals(sessionId: string): Promise<HerdrTerminal[]> {
    const result = await this.herdrRPC<{ terminals: HerdrTerminal[] }>(sessionId, 'terminal.list', { all: false });
    return result.terminals ?? [];
  }

  async readHerdrTerminal(sessionId: string, terminal: string, lines = 200): Promise<string> {
    const result = await this.herdrRPC<{ text: string }>(sessionId, 'terminal.read', { terminal, lines });
    return result.text ?? '';
  }

  async sendHerdrTerminal(sessionId: string, terminal: string, text: string): Promise<void> {
    await this.herdrRPC(sessionId, 'terminal.send', { terminal, text, wait: false });
  }

  async interruptHerdrTerminal(sessionId: string, terminal: string): Promise<void> {
    await this.herdrRPC(sessionId, 'terminal.interrupt', { terminal });
  }

  /**
   * Opens the real ACP event subscription while history is fetched in parallel.
   * Events are buffered until resumeAfterHistory() prevents the history response
   * from overwriting updates received during bootstrap.
   */
  subscribeToACPSessionEventsForInitialization(
    sessionId: string,
    callbacks: ACPSessionCallbacks
  ): ACPInitializationSubscription {
    return this.subscribeToACPSessionEvents(sessionId, '', callbacks, undefined, true) as ACPInitializationSubscription;
  }

  async listSessionPoolSuppliers(managerID: string): Promise<SessionPoolSupplier[]> {
    const pools = await this.listSessionPools();
    const suppliers = await Promise.all(
      pools.map(async (pool) => {
        const result = await this.makeRequest<{ pool_suppliers: SessionPoolSupplier[] }>(
          `/session-pools/${encodeURIComponent(pool.name)}/suppliers`
        );
        return result.pool_suppliers ?? [];
      })
    );
    return suppliers.flat().filter((supplier) => supplier.manager_id === managerID);
  }
}

Object.assign(ProxyClientCore.prototype, proxyOperations);

export type AgentAPIProxyClient = ProxyClientCore & ProxyOperations;
export const AgentAPIProxyClient = ProxyClientCore as {
  new (config: AgentAPIProxyClientConfig): AgentAPIProxyClient;
  readonly prototype: AgentAPIProxyClient;
};

// Utility functions to get proxy settings from browser storage
export function getAgentAPIProxyConfigFromStorage(repoFullname?: string): AgentAPIProxyClientConfig {
  // Check if we're in a browser environment
  if (typeof window === 'undefined') {
    // Server-side rendering or Node.js environment - use environment variables
    return {
      baseURL: process.env.AGENTAPI_PROXY_URL || 'http://localhost:8080',
      apiKey: process.env.AGENTAPI_API_KEY,
      timeout: parseInt(process.env.AGENTAPI_TIMEOUT || '10000'),
      maxSessions: parseInt(process.env.AGENTAPI_PROXY_MAX_SESSIONS || '10'),
      sessionTimeout: parseInt(process.env.AGENTAPI_PROXY_SESSION_TIMEOUT || '300000'),
      debug: defaultClientDebugEnabled(),
    };
  }

  try {
    // Get settings from global settings
    const globalSettings = loadFullGlobalSettings();
    const proxySettings = globalSettings.agentApiProxy || getDefaultProxySettings();

    // Add repository to history if repoFullname is provided
    if (repoFullname) {
      addRepositoryToHistory(repoFullname);
    }

    // Use proxy configuration
    const baseURL = proxySettings.enabled
      ? proxySettings.endpoint
      : `${window.location.protocol}//${window.location.host}/api/v1`;

    const timeout = proxySettings.enabled ? proxySettings.timeout : parseInt(process.env.AGENTAPI_TIMEOUT || '10000');

    return {
      baseURL,
      apiKey: proxySettings.apiKey || process.env.AGENTAPI_API_KEY,
      timeout,
      maxSessions: 10,
      sessionTimeout: 300000, // 5 minutes
      debug: defaultClientDebugEnabled(),
    };
  } catch {
    if (defaultClientDebugEnabled()) {
      console.warn('Failed to load proxy settings from storage, using fallback');
    }
    // Fallback if storage access fails
    return {
      baseURL: `${window.location.protocol}//${window.location.host}/api/v1`,
      apiKey: process.env.AGENTAPI_API_KEY,
      timeout: parseInt(process.env.AGENTAPI_TIMEOUT || '10000'),
      maxSessions: parseInt(process.env.AGENTAPI_PROXY_MAX_SESSIONS || '10'),
      sessionTimeout: parseInt(process.env.AGENTAPI_PROXY_SESSION_TIMEOUT || '300000'),
      debug: defaultClientDebugEnabled(),
    };
  }
}

// Factory function for easier client creation
export function createAgentAPIProxyClient(config: AgentAPIProxyClientConfig): AgentAPIProxyClient {
  return new AgentAPIProxyClient(config);
}

// Factory function to create client using stored settings
export function createAgentAPIProxyClientFromStorage(repoFullname?: string): AgentAPIProxyClient {
  const config = getAgentAPIProxyConfigFromStorage(repoFullname);
  return new AgentAPIProxyClient(config);
}

// Cluster administration always targets the proxy paired with this UI
// deployment. User-configured endpoints may point at a session-specific or
// external proxy and must not receive cluster-wide admin operations.
export function createCurrentDeploymentAgentAPIProxyClient(): AgentAPIProxyClient {
  const browser = typeof window !== 'undefined';
  return new AgentAPIProxyClient({
    baseURL: browser
      ? `${window.location.protocol}//${window.location.host}/api/v1`
      : process.env.AGENTAPI_PROXY_URL || 'http://localhost:8080',
    apiKey: browser ? undefined : process.env.AGENTAPI_API_KEY,
    timeout: parseInt(process.env.AGENTAPI_TIMEOUT || '10000'),
    maxSessions: 10,
    sessionTimeout: 300000,
    debug: defaultClientDebugEnabled(),
  });
}

// Default proxy client instance for convenience (uses global settings from storage)
// Synchronous version for backward compatibility
export function createDefaultAgentAPIProxyClient(): AgentAPIProxyClient {
  if (typeof window === 'undefined') {
    // Server-side - use environment variables directly
    const config = {
      baseURL: process.env.AGENTAPI_PROXY_URL || 'http://localhost:8080',
      apiKey: process.env.AGENTAPI_API_KEY,
      timeout: parseInt(process.env.AGENTAPI_TIMEOUT || '10000'),
      maxSessions: parseInt(process.env.AGENTAPI_PROXY_MAX_SESSIONS || '10'),
      sessionTimeout: parseInt(process.env.AGENTAPI_PROXY_SESSION_TIMEOUT || '300000'),
      debug: defaultClientDebugEnabled(),
    };
    return new AgentAPIProxyClient(config);
  }

  // Client-side - use stored settings
  const defaultProxySettings = getDefaultProxySettings();
  const config = {
    baseURL: defaultProxySettings.endpoint,
    apiKey: defaultProxySettings.apiKey,
    timeout: defaultProxySettings.timeout,
    maxSessions: parseInt(process.env.AGENTAPI_PROXY_MAX_SESSIONS || '10'),
    sessionTimeout: parseInt(process.env.AGENTAPI_PROXY_SESSION_TIMEOUT || '300000'),
    debug: defaultClientDebugEnabled(),
  };
  return new AgentAPIProxyClient(config);
}

// Default proxy client instance for convenience
export const agentAPIProxy = createDefaultAgentAPIProxyClient();
