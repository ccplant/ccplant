// Keep this as a dedicated route instead of relying on the /api/v1 catch-all:
// connection-based login must turn the backend OAuth session into the UI's
// encrypted authentication cookie. Account-link callbacks use the same handler
// and are redirected by the backend without requiring an existing UI session.
export { GET } from '@/app/api/proxy/auth/github-connections/callback/route'
