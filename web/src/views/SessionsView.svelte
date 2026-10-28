<script lang="ts">
  import { onMount } from 'svelte';
  import { exportUrl, getSessions } from '../lib/api';
  import { duration } from '../lib/format';
  import { driverKind } from '../lib/sessions';
  import { t } from '../lib/settings.svelte';
  import type { SessionSummary } from '../lib/types';

  let list = $state<SessionSummary[] | null>(null);
  let error = $state('');

  async function load(): Promise<void> {
    error = '';
    try {
      list = await getSessions();
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    }
  }

  onMount(() => void load());

  const when = (iso: string) => (iso ? new Date(iso).toLocaleString() : '–');
</script>

<h1>{t('sessions.title')}</h1>
<p class="muted">{t('sessions.help')}</p>
<button onclick={load}>{t('sessions.reload')}</button>
{#if error}<p class="err">{error}</p>{/if}

{#if list && list.length === 0}
  <p class="muted">{t('sessions.none')}</p>
{:else if list}
  <div class="scroll">
    <table>
      <thead>
        <tr>
          <th>{t('sessions.started')}</th><th>{t('sessions.track')}</th><th>{t('sessions.type')}</th><th>{t('sessions.car')}</th>
          <th>{t('sessions.length')}</th><th>{t('sessions.laps')}</th><th>{t('sessions.stops')}</th><th>{t('sessions.driver')}</th>
          <th>{t('sessions.export')}</th>
        </tr>
      </thead>
      <tbody>
        {#each list as s (s.session_id)}
          <tr>
            <td>{when(s.started_at)}</td>
            <td>{s.track || '–'}</td>
            <td>{s.session_type || '–'}</td>
            <td>{s.car || '–'}</td>
            <td class="num">{duration(s.last_et)}</td>
            <td class="num">{s.laps}</td>
            <td class="num">{s.stops}</td>
            <td><span class="badge {driverKind(s)}">{t(`sessions.kind.${driverKind(s)}` as 'sessions.kind.ai')}</span></td>
            <td class="links">
              <a href={exportUrl(s.session_id, 'laps')} download>{t('sessions.laps')}</a>
              <a href={exportUrl(s.session_id, 'stops')} download>{t('sessions.stops')}</a>
              <a href={exportUrl(s.session_id, 'conditions')} download>{t('sessions.conditions')}</a>
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<style>
  .scroll { overflow-x: auto; margin-top: 12px; }
  table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
  th { text-align: left; color: var(--muted); font-weight: 500; font-size: 0.75rem; padding: 4px 10px 4px 0; white-space: nowrap; }
  td { padding: 6px 10px 6px 0; border-top: 1px solid var(--border); white-space: nowrap; }
  .links { display: flex; gap: 12px; }
  .badge { font-size: 0.75rem; padding: 2px 8px; border-radius: 999px; background: var(--surface-2); }
  .badge.ai { color: var(--warn, #e5a50a); }
  .badge.player { color: var(--good, #3fb950); }
  .err { color: var(--bad, #e5534b); }
</style>
