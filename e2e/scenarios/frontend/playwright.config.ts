import {defineConfig,devices} from '@playwright/test';
const authType=process.env.E2E_AUTH_TYPE;const authValue=process.env.E2E_AUTH_VALUE||'';const extraHTTPHeaders=authType==='bearer'?{Authorization:`Bearer ${authValue}`}:authType==='api-key'?{'X-API-Key':authValue}:{};
export default defineConfig({testDir:'.',timeout:60_000,retries:0,reporter:'json',use:{baseURL:process.env.E2E_TARGET_FRONTEND_URL||'http://localhost:3000',extraHTTPHeaders,trace:'retain-on-failure',screenshot:'only-on-failure'},projects:[{name:'chromium',use:{...devices['Desktop Chrome']}}]})
