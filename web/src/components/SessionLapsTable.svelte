<script lang="ts">
  import { onMount } from 'svelte';
  import { getSessionLaps, setLapExcluded, setSessionExcluded } from '../lib/api';
  import { lapTime } from '../lib/format';
  import { nextFlag } from '../lib/laps';
  import { t } from '../lib/settings.svelte';
  import type { LapRow, SessionLaps } from '../lib/types';

  let { sessionId, onchange }: { sessionId: string; onchange?: () => void } = $props();

  let data = $state<SessionLaps | null>(null);
  let error = $state('');
  let onlyMine = $state(false);
  let driver = $state('');

  async function load(): Promise<void> {
    error = '';
    try {
      data = await getSessionLaps(sessionId);
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    }
  }
  onMount(() => void load());

  async function act(action: () => Promise<unknown>): Promise<void> {
    error = '';
    try {
      await action();
      await load();
      onchange?.();
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    }
  }

  // The checkbox says whether the lap counts; the flag to store follows from the lap and its session (see nextFlag).
  function toggle(l: LapRow): void {
    const key = { session_id: sessionId, car_id: l.car_id, vehicle_name: l.vehicle_name, lap: l.lap };
    void act(() => setLapExcluded(key, nextFlag(l.excluded, data?.excluded ?? false)));
  }

  const drivers = $derived([...new Set((data?.laps ?? []).map((l) => l.driver).filter(Boolean))].sort());
  const rows = $derived((data?.laps ?? []).filter((l) => (!onlyMine || l.mine) && (!driver || l.driver === driver)));

  const n = (v: number | null | undefined, d = 1) => (v == null ? '–' : v.toFixed(d));
  const avg = (v?: number[]) => (v && v.length ? (v.reduce((a, b) => a + b, 0) / v.length).toFixed(0) : '–');
  const wet = (l: LapRow) => {
    const w = Math.max(l.wetness ?? 0, l.rain ?? 0);
    return w > 0.05 ? `${Math.round(w * 100)} %` : '–';
  };
  const notes = (l: LapRow) =>
    [l.in_lap && t('laps.flag.in'), l.out_lap && t('laps.flag.out'), l.fcy && t('laps.flag.fcy')].filter(Boolean).join(', ');
</script>

{#if error}<p class="err">{error}</p>{/if}
{#if !data}
  <p class="muted">{t('laps.loading')}</p>
{:else}
  <div class="bar">
    <label class="check" title={data.can_edit_session ? '' : t('laps.sessionLocked')}>
      <input type="checkbox" checked={!data.excluded} disabled={!data.can_edit_session}
        onchange={() => void act(() => setSessionExcluded(sessionId, !data!.excluded))} />
      {t('laps.sessionCounts')}
    </label>
    {#if !data.can_edit_session}<span class="muted small">{t('laps.sessionLocked')}</span>{/if}
    <label class="check"><input type="checkbox" bind:checked={onlyMine} /> {t('laps.onlyMine')}</label>
    <select bind:value={driver} aria-label={t('laps.driver')}>
      <option value="">{t('laps.allDrivers')}</option>
      {#each drivers as d (d)}<option value={d}>{d}</option>{/each}
    </select>
  </div>
  {#if data.laps.length === 0}
    <p class="muted">{t('laps.none')}</p>
  {:else}
    <div class="scroll">
      <table>
        <thead>
          <tr>
            <th>{t('laps.counts')}</th><th>{t('laps.lap')}</th><th>{t('laps.driver')}</th><th>{t('laps.agent')}</th>
            <th>{t('laps.time')}</th><th>S1</th><th>S2</th><th>S3</th><th>{t('laps.fuel')}</th><th>{t('laps.tyreAge')}</th>
            <th>{t('laps.compound')}</th><th>{t('laps.ambient')}</th><th>{t('laps.trackTemp')}</th><th>{t('laps.wet')}</th>
            <th>{t('laps.pressure')}</th><th>{t('laps.control')}</th><th>{t('laps.flags')}</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as l (`${l.car_id}|${l.vehicle_name}|${l.lap}`)}
            <tr class:out={l.excluded} class:mine={l.mine}>
              <td>
                <input type="checkbox" checked={!l.excluded} disabled={!l.can_edit} onchange={() => toggle(l)}
                  title={l.can_edit ? (l.excluded_flag === null ? t('laps.followsSession') : t('laps.ownChoice')) : t('laps.cannotEdit')}
                  aria-label={t('laps.counts')} />
              </td>
              <td class="num">{l.lap}</td><td>{l.driver || '–'}</td><td>{l.agent || '–'}</td>
              <td class="num">{lapTime(l.lap_time)}</td><td class="num">{n(l.s1, 3)}</td><td class="num">{n(l.s2, 3)}</td><td class="num">{n(l.s3, 3)}</td>
              <td class="num">{n(l.fuel_used, 2)}</td><td class="num">{l.tyre_age ?? '–'}</td><td>{l.compound || '–'}</td>
              <td class="num">{n(l.ambient)}</td><td class="num">{n(l.track)}</td><td class="num">{wet(l)}</td><td class="num">{avg(l.pressure)}</td>
              <td>{l.control == null ? '–' : t(`laps.ctl.${l.control}` as 'laps.ctl.0')}</td><td>{notes(l)}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
{/if}

<style>
  .bar { display: flex; flex-wrap: wrap; align-items: center; gap: 12px; margin: 8px 0; }
  .check { display: inline-flex; align-items: center; gap: 6px; }
  .small { font-size: 0.8rem; }
  .scroll { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.85rem; }
  th { text-align: left; color: var(--muted); font-weight: 500; font-size: 0.72rem; padding: 4px 8px 4px 0; white-space: nowrap; }
  td { padding: 3px 8px 3px 0; border-top: 1px solid var(--border); white-space: nowrap; }
  tr.out td { opacity: 0.5; text-decoration: line-through; }
  tr.out td:first-child { text-decoration: none; opacity: 1; }
  tr.mine td:nth-child(2) { font-weight: 700; }
  .err { color: var(--bad, #e5534b); }
</style>
