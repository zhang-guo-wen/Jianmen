import path from 'node:path';
import { fileURLToPath } from 'node:url';

import vue from '@vitejs/plugin-vue';
import { defineConfig, loadEnv } from 'vite';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');

  return {
    plugins: [vue({
      template: {
        compilerOptions: {
          isCustomElement: (tag) => tag === 'altcha-widget'
        }
      }
    })],
    resolve: {
      alias: {
        '@': path.resolve(__dirname, 'src')
      }
    },
    build: {
      rolldownOptions: {
        output: {
          codeSplitting: {
            minSize: 50_000,
            groups: [
              {
                name: 'vendor-vue',
                test: /node_modules[\\/](vue|vue-router|pinia)[\\/]/,
                priority: 30
              },
              {
                name: 'vendor-xterm',
                test: /node_modules[\\/]@xterm[\\/]/,
                priority: 10
              }
            ]
          }
        }
      }
    },
    server: {
      port: 47101,
      proxy: {
        '/api': {
          target: env.VITE_API_BASE_URL || 'http://localhost:47100',
          ws: true
        }
      }
    }
  };
});
