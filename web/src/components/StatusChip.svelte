<script lang="ts">
  import { live } from '../lib/store';
  import { t } from '../lib/settings.svelte';

  // After a lost stream the last state stays on screen; the chip says so instead of just "offline".
  const label = $derived(
    live.status === 'offline' && live.fromCache
      ? t('status.saved')
      : live.reconnecting
        ? t('status.reconnecting')
        : t(`status.${live.status}` as 'status.live'),
  );
</script>

<span class="chip {live.status}" class:reconnecting={live.reconnecting} role="status" aria-label={label} title={label}>
  <span class="dot"></span><span class="label">{label}</span>
</span>

<style>
  .chip { display: inline-flex; align-items: center; gap: 6px; font-size: 0.85rem; color: var(--muted); white-space: nowrap; }
  .dot { width: 10px; height: 10px; border-radius: 50%; background: var(--muted); }
  @media (max-width: 520px) { .label { display: none; } .dot { width: 14px; height: 14px; } }
  .live .dot { background: var(--good); }
  .stale .dot { background: var(--warn); }
  .offline .dot { background: var(--bad); }
  .reconnecting .dot { background: var(--warn); animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 50% { opacity: 0.35; } }
  @media (prefers-reduced-motion: reduce) { .reconnecting .dot { animation: none; } }
</style>
