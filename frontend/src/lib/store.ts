// Re-export reactive state from store.svelte.ts.
// Svelte 5 $state runes require a .svelte.ts file; this barrel keeps
// existing `import ... from '$lib/store'` imports working unchanged.
export { appState, initStore, refreshDashboard, startUpdate, dismissUpdate } from './store.svelte'
