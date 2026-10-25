import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { VitePWA } from 'vite-plugin-pwa';

const backend = process.env.SPLASH_BACKEND ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [
    svelte(),
    VitePWA({
      registerType: 'autoUpdate',
      manifest: {
        name: 'Splash',
        short_name: 'Splash',
        description: 'Endurance race strategy for rFactor 2',
        theme_color: '#0e1217',
        background_color: '#0e1217',
        display: 'standalone',
        icons: [
          { src: 'icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'icon-512.png', sizes: '512x512', type: 'image/png' },
          { src: 'icon-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      workbox: {
        navigateFallback: 'index.html',
        navigateFallbackDenylist: [/^\/api\//],
        runtimeCaching: [
          {
            // The last stint plan stays readable without the backend. The live stream is never cached.
            urlPattern: ({ url }) => url.pathname === '/api/stint-plan',
            handler: 'NetworkFirst',
            options: { cacheName: 'splash-plan', networkTimeoutSeconds: 3 },
          },
        ],
      },
    }),
  ],
  build: {
    outDir: '../backend/internal/webui/dist',
    emptyOutDir: false, // scripts/clean-dist.mjs does it and keeps .gitkeep
  },
  server: { proxy: { '/api': backend } },
  test: { environment: 'node', include: ['src/**/*.test.ts'] },
});
