import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { SecretsSection } from '../SecretsSection'

const mocks = vi.hoisted(() => ({
  create: vi.fn().mockResolvedValue({}),
  list: vi.fn().mockResolvedValue([]),
  remove: vi.fn().mockResolvedValue(undefined),
}))

vi.mock('../../SettingsScopeContext', () => ({ useSettingsScope: () => ({ scopeId: 'user-1' }) }))
vi.mock('@/lib/agentapi-proxy-client', () => ({
  createAgentAPIProxyClientFromStorage: () => ({
    listSettingsSecrets: mocks.list,
    createSettingsSecret: mocks.create,
    deleteSettingsSecret: mocks.remove,
  }),
}))

afterEach(() => { cleanup(); vi.clearAllMocks(); mocks.list.mockResolvedValue([]) })

describe('SecretsSection', () => {
  it('asks for the usage first and maps all three usages to the API payload', async () => {
    render(<SecretsSection />)
    fireEvent.click(screen.getByRole('button', { name: '追加' }))
    fireEvent.change(screen.getByPlaceholderText('例: GitHub integration'), { target: { value: 'Runtime secrets' } })

    expect(screen.queryByLabelText('環境変数名 1')).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /環境変数/ }))
    fireEvent.change(screen.getByLabelText('環境変数名 1'), { target: { value: 'API_TOKEN' } })
    fireEvent.change(screen.getByLabelText('環境変数の値 1'), { target: { value: 'env-value' } })

    fireEvent.click(screen.getByRole('button', { name: '項目を追加' }))
    fireEvent.click(screen.getAllByRole('button', { name: /ファイル/ })[1])
    fireEvent.change(screen.getByLabelText('配置パス 2'), { target: { value: '/tmp/credential' } })
    fireEvent.change(screen.getByLabelText('ファイル内容 2'), { target: { value: 'file-value' } })
    fireEvent.change(screen.getByLabelText('パーミッション 2'), { target: { value: '0400' } })

    fireEvent.click(screen.getByRole('button', { name: '項目を追加' }))
    fireEvent.click(screen.getAllByRole('button', { name: /その他/ })[2])
    fireEvent.change(screen.getByLabelText('キー 3'), { target: { value: 'bot-token' } })
    fireEvent.change(screen.getByLabelText('値 3'), { target: { value: 'kv-value' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(mocks.create).toHaveBeenCalledTimes(1))
    expect(mocks.create).toHaveBeenCalledWith('user-1', {
      name: 'Runtime secrets',
      values: { API_TOKEN: 'env-value', '/tmp/credential': 'file-value', 'bot-token': 'kv-value' },
      projections: [
        { key: 'API_TOKEN', type: 'env', env_name: 'API_TOKEN' },
        { key: '/tmp/credential', type: 'file', path: '/tmp/credential', permissions: '0400' },
      ],
    })
  })
})
