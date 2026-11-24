import { defineConfig } from 'vite';

const apiTarget = process.env.VITE_API_TARGET ?? `http://127.0.0.1:${process.env.PORT ?? '8080'}`;

export default defineConfig({
  server: {
    proxy: {
      '/api': apiTarget,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
  },
});
