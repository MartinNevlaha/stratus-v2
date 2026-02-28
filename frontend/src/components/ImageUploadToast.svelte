<script lang="ts">
  interface Props {
    message: string
    onDismiss: () => void
  }

  let { message, onDismiss }: Props = $props()

  $effect(() => {
    const timer = setTimeout(onDismiss, 4000)
    return () => clearTimeout(timer)
  })
</script>

<div class="toast">
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
    <rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>
    <circle cx="8.5" cy="8.5" r="1.5"/>
    <polyline points="21 15 16 10 5 21"/>
  </svg>
  <span class="toast-text">{message}</span>
  <button class="toast-close" onclick={onDismiss}>&times;</button>
</div>

<style>
  .toast {
    position: absolute;
    bottom: 16px;
    right: 16px;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    background: #1c2128;
    border: 1px solid #3fb950;
    border-radius: 6px;
    color: #c9d1d9;
    font-size: 12px;
    z-index: 10;
    animation: slide-in 0.2s ease-out;
    max-width: 400px;
  }

  .toast svg {
    width: 16px;
    height: 16px;
    flex-shrink: 0;
    color: #3fb950;
  }

  .toast-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .toast-close {
    background: none;
    border: none;
    color: #8b949e;
    cursor: pointer;
    font-size: 14px;
    padding: 0 4px;
  }

  @keyframes slide-in {
    from { opacity: 0; transform: translateY(8px); }
    to { opacity: 1; transform: translateY(0); }
  }
</style>
