import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests/ui',
  fullyParallel: true,
  workers: 3,
  retries: 0,
  reporter: 'list',
  use: {
    baseURL: 'http://127.0.0.1:5183',
    browserName: 'chromium',
    channel: 'msedge',
    viewport: { width: 960, height: 680 },
    colorScheme: 'light',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'npm run dev',
    url: 'http://127.0.0.1:5183',
    reuseExistingServer: false,
  },
});
