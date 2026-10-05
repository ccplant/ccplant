import { createProxyRouteHandler } from '@/lib/server-proxy-route'

const options = {
  publicPrefix: '/mcp',
  upstreamPath: 'mcp',
  passThroughAuthorization: true,
  // Streamable HTTP clients advertise both JSON and SSE. Wait for the
  // backend response headers so its selected representation stays intact.
  eagerSSE: false,
}

export const GET = createProxyRouteHandler('GET', options)
export const POST = createProxyRouteHandler('POST', options)
export const DELETE = createProxyRouteHandler('DELETE', options)
