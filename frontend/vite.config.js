import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'path';

// Allow overriding the backend target for development against a remote
// openmanetd instance:
//   VITE_API_TARGET=http://10.41.1.1:8081 pnpm run dev
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
    emptyOutDir: false,
    // Raised above the size of the (lazy-loaded) TopologyMap chunk, which
    // carries reagraph + three.js. Any synchronous chunk exceeding this is a
    // genuine regression worth investigating.
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        manualChunks: {
          'vendor-react': ['react', 'react-dom', 'react-router-dom'],
          'vendor-connect': [
            '@connectrpc/connect',
            '@connectrpc/connect-web',
            '@bufbuild/protobuf',
          ],
        },
      },
    },
  },
  server: {
    // Proxy every API surface to the Go frontend daemon during development.
    // /rpc and /auth are themselves reverse-proxied by the frontend daemon
    // through to the ConnectRPC API server, so dev mirrors prod's single-
    // origin model and there is no special-cased rewrite here.
    proxy: {
      '/ws': { target: apiTarget, ws: true },
      '/api': { target: apiTarget, ws: true },
      '/auth': { target: apiTarget },
      '/rpc': { target: apiTarget },
      '/whisper': { target: apiTarget },
    },
  },
});
