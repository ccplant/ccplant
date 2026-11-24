// Context shared by resource operations installed on the proxy client.
export interface ProxyOperationContext {
  makeRequest<T>(endpoint: string, options?: RequestInit, attempt?: number): Promise<T>;
  readonly debug: boolean;
}
