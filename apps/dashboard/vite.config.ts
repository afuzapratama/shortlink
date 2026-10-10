import { fileURLToPath } from 'node:url';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, fileURLToPath(new URL('.', import.meta.url)), 'CONTROL_API_TARGET');
  const target = process.env.CONTROL_API_TARGET ?? env.CONTROL_API_TARGET ?? 'http://127.0.0.1:8080';
  return {
    server: {
      proxy: {
        '/v1': { target, changeOrigin: false },
        '/health': { target, changeOrigin: false },
      },
    },
  };
});
