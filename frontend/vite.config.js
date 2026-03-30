import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

// Allow overriding the backend target for development against a remote
// openmanetd instance:
//   VITE_API_TARGET=http://10.41.1.1:8081 npm run dev
const apiTarget = process.env.VITE_API_TARGET || 'http://localhost:8080';

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
      '/ws': { target: apiTarget, ws: true },
      '/api': { target: apiTarget },
      '/whisper': { target: apiTarget },
      '/rpc': {
        target: process.env.VITE_RPC_TARGET || 'http://localhost:8087',
        rewrite: (path) => path.replace(/^\/rpc/, ''),
      },
    },
  },
});
