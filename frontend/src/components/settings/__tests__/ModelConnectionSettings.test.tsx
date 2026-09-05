import { render, screen, fireEvent, waitFor, cleanup } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ModelConnectionSettings } from '../ModelConnectionSettings'

afterEach(cleanup)
describe('ModelConnectionSettings', () => {
  it('saves a default model without re-sending a stored secret', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<ModelConnectionSettings agent="codex" connection={{ mode: 'openai_compatible', base_url: 'https://gateway.example/v1', model: 'default', authentication: 'api_key', has_api_key: true }} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('codex デフォルトモデル ID'), { target: { value: 'new-default' } })
    fireEvent.click(screen.getByRole('button', { name: '保存して使用' }))
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(onSave.mock.calls[0][0]).toMatchObject({ model: 'new-default', base_url: 'https://gateway.example/v1' })
    expect(onSave.mock.calls[0][0]).not.toHaveProperty('api_key')
    expect(screen.getByText(/セッションプロファイルでモデルを指定/)).toBeTruthy()
  })
  it('supports Claude bearer credentials and reports save failures', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('API key is required'))
    render(<ModelConnectionSettings agent="claude" onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('claude 接続方式'), { target: { value: 'anthropic_compatible' } })
    fireEvent.change(screen.getByLabelText('claude 認証'), { target: { value: 'bearer_token' } })
    fireEvent.click(screen.getByRole('button', { name: '保存して使用' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('API key is required')
    expect(onSave.mock.calls[0][0]).toMatchObject({ mode: 'anthropic_compatible', authentication: 'bearer_token' })
  })
})
