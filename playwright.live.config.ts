import { defineConfig } from '@playwright/test';
export default defineConfig({
 testDir:'./tests/e2e',testMatch:'**/live.spec.ts',workers:2,
 use:{baseURL:'http://127.0.0.1:5175',trace:'retain-on-failure'},
 webServer:{command:'npm run dev -- --port 5175 --strictPort',url:'http://127.0.0.1:5175',reuseExistingServer:false},
 projects:[{name:'desktop',use:{browserName:'chromium',viewport:{width:1440,height:1000}}},{name:'mobile',use:{browserName:'chromium',viewport:{width:390,height:844},isMobile:true,hasTouch:true}}],
});
