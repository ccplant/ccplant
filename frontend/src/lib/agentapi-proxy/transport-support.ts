export const inFlightGetRequests = new Map<string, Promise<unknown>>();

export function defaultClientDebugEnabled(): boolean {
  return (
    process.env.NODE_ENV !== 'production' &&
    (process.env.NEXT_PUBLIC_DEBUG_LOGS === 'true' || process.env.DEBUG_LOGS === 'true')
  );
}

export function debugAllowed(requested: boolean): boolean {
  return process.env.NODE_ENV !== 'production' && requested;
}

export function requestUrlForLog(rawUrl: string): string {
  try {
    const url = new URL(rawUrl);
    return `${url.origin}${url.pathname}`;
  } catch {
    return rawUrl.split('?')[0];
  }
}
