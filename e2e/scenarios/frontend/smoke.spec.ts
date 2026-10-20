import {test,expect} from '@playwright/test';
test('トップ画面を表示できる @smoke-home',async({page})=>{await test.step('トップ画面を開く',async()=>{await page.goto('/');await expect(page.locator('body')).toBeVisible()});await test.step('ページが応答している',async()=>{await expect(page).toHaveTitle(/.+/)})})
