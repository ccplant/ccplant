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
    expect(screen.getByText(/セッションプロファイルの指定がある場合/)).toBeTruthy()
  })
  it('uses the instance Base URL and API key authentication for Claude', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('API key is required'))
    render(<ModelConnectionSettings agent="claude" defaultBaseURL="https://system-anthropic.example" onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('claude 接続方式'), { target: { value: 'anthropic_compatible' } })
    expect(screen.getByLabelText('claude Base URL')).toHaveValue('https://system-anthropic.example')
    expect(screen.queryByRole('option', { name: 'Bearer トークン' })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '保存して使用' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('API key is required')
    expect(onSave.mock.calls[0][0]).toMatchObject({ mode: 'anthropic_compatible', authentication: 'api_key', base_url: 'https://system-anthropic.example' })
  })
  it('configures an agent-specific default model with built-in authentication', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<ModelConnectionSettings agent="codex" connection={{ mode: 'auth_json' }} onSave={onSave} />)
    fireEvent.change(screen.getByLabelText('codex デフォルトモデル ID'), { target: { value: 'gpt-default' } })
    fireEvent.click(screen.getByRole('button', { name: '保存して使用' }))
    await waitFor(() => expect(onSave).toHaveBeenCalled())
    expect(onSave.mock.calls[0][0]).toMatchObject({ mode: 'auth_json', model: 'gpt-default' })
  })
})
