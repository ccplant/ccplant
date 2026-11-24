import {defineConfig,devices} from '@playwright/test';
const authType = process.env.E2E_AUTH_TYPE;
const authValue = process.env.E2E_AUTH_VALUE || '';
const cloudflareClientId = process.env.E2E_CF_ACCESS_CLIENT_ID || '';
const cloudflareClientSecret = process.env.E2E_CF_ACCESS_CLIENT_SECRET || '';

if (Boolean(cloudflareClientId) !== Boolean(cloudflareClientSecret)) {
  throw new Error('E2E_CF_ACCESS_CLIENT_ID and E2E_CF_ACCESS_CLIENT_SECRET must be set together');
}

const extraHTTPHeaders: Record<string, string> = authType === 'bearer'
  ? { Authorization: `Bearer ${authValue}` }
  : authType === 'api-key'
    ? { 'X-API-Key': authValue }
    : {};

if (cloudflareClientId && cloudflareClientSecret) {
  extraHTTPHeaders['CF-Access-Client-Id'] = cloudflareClientId;
  extraHTTPHeaders['CF-Access-Client-Secret'] = cloudflareClientSecret;
}

const hasSensitiveHeaders = Boolean(authValue || cloudflareClientSecret);

export default defineConfig({
  testDir: '.',
  timeout: 60_000,
  retries: 0,
  reporter: 'json',
  use: {
    baseURL: process.env.E2E_TARGET_FRONTEND_URL || 'http://localhost:3000',
    extraHTTPHeaders,
    trace: hasSensitiveHeaders ? 'off' : 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
});
