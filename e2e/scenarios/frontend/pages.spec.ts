import { expect, test } from '@playwright/test'

test('設定と連携先の主要ページを表示できる @frontend-pages', async ({ page }) => {
  await test.step('ユーザー設定ページを表示する', async () => {
    await page.goto('/settings/personal/user-profile')
    await expect(page.getByRole('heading', { name: 'ユーザー情報と編集' })).toBeVisible()
    await expect(page.getByText('Principal ID', { exact: true })).toBeVisible()
  })

  await test.step('Integrationsページを表示する', async () => {
    await page.goto('/integrations')
    await expect(page.getByRole('heading', { name: 'Integrations' })).toBeVisible()
    await expect(page.getByRole('heading', { name: '連携先一覧' })).toBeVisible()
  })
})
