<script lang="ts">
  import { tileInfo } from '../lib/info.svelte';
  import { t } from '../lib/settings.svelte';
  import { view } from '../lib/view.svelte';

  const agents = $derived(view.state?.agents ?? []);
  tileInfo(() => t('agents.help'));

  /** Seconds since a time given as ISO text, never negative. */
  const age = (iso: string) => Math.max(Math.round((view.nowMs - Date.parse(iso)) / 1000), 0);
  const STALE = 10;
</script>

{#if agents.length === 0}
  <p class="muted">{t('agents.none')}</p>
{:else}
  <ul>
    {#each agents as a (a.agent_id)}
      {@const sec = age(a.last_data)}
      {@const stale = a.connected && sec > STALE}
      <li class:gone={!a.connected}>
        <span class="dot {!a.connected ? 'bad' : stale ? 'warn' : 'good'}"></span>
        <span class="name"><b>{a.agent_id}</b></span>
        <span class="muted small">
          {#if !a.connected}{t('agents.gone', { s: age(a.since) })}
          {:else if stale}{t('agents.stale', { s: sec })}
          {:else}{t('agents.ok', { s: sec })}{/if}
          {#if a.version} · v{a.version}{/if}{#if a.outdated} · <b class="outdated">{t('agents.outdated')}</b>{/if}
        </span>
      </li>
    {/each}
  </ul>
{/if}

<style>
  ul { list-style: none; margin: 0; padding: 0; display: grid; gap: calc(8 * var(--ru) / 16); }
  li { display: grid; grid-template-columns: auto 1fr; gap: calc(2 * var(--ru) / 16) calc(10 * var(--ru) / 16); align-items: center; }
  li .muted { grid-column: 2; }
  .dot { width: 12px; height: 12px; border-radius: 50%; background: var(--muted); }
  .dot.good { background: var(--good, #3fb950); }
  .dot.warn { background: var(--warn, #e5a50a); }
  .dot.bad { background: var(--bad, #e5534b); }
  .gone { opacity: 0.8; }
  .small { font-size: calc(0.8 * var(--ru)); }
  .outdated { color: var(--warn, #e5a50a); }
</style>
