import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    setupFiles: './src/test-setup.js',
  },
  // Build output goes into ../static/ so the Go binary can embed it
  build: {
    outDir: path.resolve(__dirname, '../static'),
    emptyOutDir: true,
  },
  server: {
    // Proxy API and WebSocket to the Go backend during development
    proxy: {
      '/ws': { target: 'http://localhost:8080', ws: true },
      '/api': { target: 'http://localhost:8080' },
      '/whisper': { target: 'http://localhost:8080' },
    },
  },
});
