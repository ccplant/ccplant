import {defineConfig,devices} from '@playwright/test';
export default defineConfig({testDir:'.',timeout:60_000,retries:0,reporter:'json',use:{baseURL:process.env.E2E_TARGET_FRONTEND_URL||'http://localhost:3000',trace:'retain-on-failure',screenshot:'only-on-failure'},projects:[{name:'chromium',use:{...devices['Desktop Chrome']}}]})
