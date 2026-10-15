import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import preprocess from 'svelte-preprocess';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('.', import.meta.url));
process.env.VITE_FSE_PLATFORM = 'docker';

export default defineConfig({
  plugins: [svelte({ preprocess: preprocess() })],
  build: {
    outDir: 'dist-docker',
    emptyOutDir: true,
    rollupOptions: {
      input: resolve(root, 'index.docker.html'),
    },
  },
});
