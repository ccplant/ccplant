import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { BindingSettingsEditor, PoolSettingsEditor, SupplierSettingsEditor } from '../PoolResourceEditors'

afterEach(cleanup)

describe('pool resource editors', () => {
  it('updates pool labels', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<PoolSettingsEditor pool={{ name: 'builders', enabled: true, labels: { arch: 'amd64' } }} onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: '設定を編集' }))
    fireEvent.change(screen.getByLabelText('buildersのLabels'), { target: { value: 'arch=arm64\nregion=tokyo' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ labels: { arch: 'arm64', region: 'tokyo' } }))
  })

  it('updates supplier capacity without recreating it', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<SupplierSettingsEditor supplier={{ pool: 'builders', manager_id: 'manager-1', enabled: true, min_idle: 1, max_runners: 10 }} onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: '編集' }))
    fireEvent.change(screen.getByLabelText('Min idle'), { target: { value: '3' } })
    fireEvent.change(screen.getByLabelText('Max runners'), { target: { value: '20' } })
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ min_idle: 3, max_runners: 20 }))
  })

  it('updates binding roles, priority, quota, and enabled state', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined)
    render(<BindingSettingsEditor binding={{ id: 'binding-1', pool: 'builders', subject_type: 'team', subject_id: 'acme/dev', role: 'use', enabled: true, priority: 1, max_concurrent: 2 }} onSave={onSave} />)
    fireEvent.click(screen.getByRole('button', { name: '編集' }))
    fireEvent.click(screen.getByRole('checkbox', { name: 'manage' }))
    fireEvent.change(screen.getByLabelText('Priority'), { target: { value: '5' } })
    fireEvent.change(screen.getByLabelText('Max concurrent'), { target: { value: '8' } })
    fireEvent.click(screen.getByRole('checkbox', { name: '有効' }))
    fireEvent.click(screen.getByRole('button', { name: '保存' }))
    await waitFor(() => expect(onSave).toHaveBeenCalledWith({ roles: ['use', 'manage'], priority: 5, max_concurrent: 8, enabled: false }))
  })
})
