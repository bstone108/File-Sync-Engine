import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: '.',
  timeout: 120000,
  expect: { timeout: 20000 },
  use: { baseURL: 'http://127.0.0.1:8385' },
  reporter: 'line',
});
