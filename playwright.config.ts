import { defineConfig } from '@playwright/test';

export default defineConfig({
  outputDir: './test-results/prototype',
  testDir: './tests/e2e', testIgnore: '**/live.spec.ts',
  use: { baseURL: 'http://127.0.0.1:5174', trace: 'retain-on-failure' },
  webServer: { command: 'npm run dev:prototype -- --strictPort', url: 'http://127.0.0.1:5174', reuseExistingServer: !process.env.CI },
  projects: [{name:'desktop',use:{browserName:'chromium',viewport:{width:1440,height:1000}}},{name:'mobile',use:{browserName:'chromium',viewport:{width:390,height:844},isMobile:true,hasTouch:true}}],
});
