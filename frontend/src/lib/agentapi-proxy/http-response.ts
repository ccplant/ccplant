import { handleAuthenticationRequired, isAuthenticationRequiredError } from '../auth-error-handler';
import { AgentAPIProxyError } from './types';

// Cookie/public requests and API-key fallback responses may report an expired
// browser session. An initial Bearer rejection alone must not log the user out.
export async function parseProxyResponse<T>(response: Response, checkAuthentication: boolean): Promise<T> {
  if (!response.ok) {
    const errorData = await response.json().catch(() => ({
      message: `HTTP ${response.status}: ${response.statusText}`,
    }));
    if (checkAuthentication && isAuthenticationRequiredError(response.status, errorData)) {
      handleAuthenticationRequired();
    }
    throw new AgentAPIProxyError(
      response.status,
      errorData.error?.code || 'HTTP_ERROR',
      errorData.message || errorData.error?.message || `HTTP ${response.status}`,
      errorData.error?.details
    );
  }
  if (response.status === 204) return undefined as T;
  return response.json();
}

export function proxyRequestError(error: unknown): AgentAPIProxyError {
  if (error instanceof AgentAPIProxyError) return error;
  return new AgentAPIProxyError(0, 'NETWORK_ERROR', error instanceof Error ? error.message : 'Unknown network error');
}
