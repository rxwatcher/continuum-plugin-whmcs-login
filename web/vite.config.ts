import path from 'node:path';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

// Continuum mounts the plugin under /api/v1/plugins/{installationId}/, where
// {installationId} is assigned at install time. Using a relative base ("./")
// makes asset URLs resolve against the current document URL, so the SPA
// works regardless of installation ID. The Go server further injects a
// <base href> tag at runtime to disambiguate deep nav paths.
export default defineConfig({
  base: './',
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') },
  },
  build: { outDir: 'dist', emptyOutDir: true },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
  },
});
