import { appState } from './store.svelte'

export function reportError(message: string, error?: unknown) {
  console.error(message, error)
  appState.error = error instanceof Error ? error.message : message
}
