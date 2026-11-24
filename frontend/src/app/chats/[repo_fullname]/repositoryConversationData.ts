import { Chat } from '../../../types/chat'

export type ConversationStatus = Chat['status'] | 'all'
export const statusOptions: Array<{ value: ConversationStatus; label: string }> = [
  { value: 'all', label: 'All' }, { value: 'running', label: 'Running' },
  { value: 'completed', label: 'Completed' }, { value: 'failed', label: 'Failed' },
  { value: 'pending', label: 'Pending' }, { value: 'cancelled', label: 'Cancelled' },
]

export function getMockChats(): Chat[] {
  const ago = (milliseconds: number) => new Date(Date.now() - milliseconds).toISOString()
  return [
    { id: 'chat-001', title: 'Code Review Discussion', summary: 'Discussing best practices for React component architecture and performance optimization strategies.', status: 'completed', created_at: ago(2 * 60 * 60 * 1000), updated_at: ago(30 * 60 * 1000), message_count: 15, duration: 1200, metadata: { repository: 'orgs/example-repo', repository_owner: 'orgs', repository_name: 'example-repo', branch: 'main', commit_hash: 'abc123' } },
    { id: 'chat-002', title: 'API Integration Help', summary: 'Working through REST API integration issues and authentication flow problems.', status: 'running', created_at: ago(45 * 60 * 1000), updated_at: ago(5 * 60 * 1000), message_count: 8, duration: 2400, metadata: { repository: 'users/demo-project', repository_owner: 'users', repository_name: 'demo-project', branch: 'feature/api-integration' } },
    { id: 'chat-003', title: 'Database Schema Design', summary: 'Designing optimal database schema for a multi-tenant application with complex relationships.', status: 'failed', created_at: ago(24 * 60 * 60 * 1000), updated_at: ago(22 * 60 * 60 * 1000), message_count: 23, duration: 3600, metadata: { repository: 'orgs/example-repo', repository_owner: 'orgs', repository_name: 'example-repo', branch: 'develop' } },
    { id: 'chat-004', title: 'Frontend Testing Strategy', summary: 'Exploring different testing approaches for React applications including unit, integration, and e2e tests.', status: 'pending', created_at: ago(3 * 24 * 60 * 60 * 1000), updated_at: ago(2 * 24 * 60 * 60 * 1000), message_count: 5, duration: 600, metadata: { repository: 'users/test-app', repository_owner: 'users', repository_name: 'test-app' } },
  ]
}

export function belongsToRepository(chat: Chat, repository: string) {
  const metadata = chat.metadata
  return metadata?.repository === repository || metadata?.repository_name === repository || (metadata?.repository_owner && metadata?.repository_name && `${metadata.repository_owner}/${metadata.repository_name}` === repository)
}
