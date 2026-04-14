import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { resolve } from 'path'

export default defineConfig({
  plugins: [svelte({
    compilerWarnings: {
      'a11y_click_events_have_key_events': 'ignore',
      'a11y_no_static_element_interactions': 'ignore',
      'a11y_no_noninteractive_element_interactions': 'ignore',
      'a11y_label_has_associated_control': 'ignore',
      'non_reactive_update': 'ignore',
    },
  })],
  resolve: {
    alias: {
      $lib: resolve('./src/lib'),
    },
  },
  build: {
    outDir: '../cmd/stratus/static',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': {
        target: `http://localhost:${process.env.STRATUS_PORT || '41777'}`,
        changeOrigin: true,
        ws: true,
      },
    },
  },
})
