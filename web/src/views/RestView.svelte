<script lang="ts">
  import { clockAt, duration } from '../lib/format';
  import { drivers, nextStintOf, until } from '../lib/plan';
  import { settings, t } from '../lib/settings.svelte';
  import { live } from '../lib/store';

  const s = $derived(live.state);
  const entries = $derived(s?.stint_plan?.entries ?? []);
  const names = $derived(drivers(entries.map((e) => ({ driver: e.driver, duration: 0 }))));
  const me = $derived(settings.restDriver);
  const mine = $derived(s && me ? nextStintOf(s, me) : undefined);
  const driving = $derived(mine?.current === true);
  // The stint before mine is the one I spot: be online from its start.
  const spotFrom = $derived.by(() => {
    if (!mine || driving) return null;
    const i = entries.findIndex((e) => e.no === mine.no);
    const prev = i > 0 ? entries[i - 1] : undefined;
    return prev && prev.driver !== me && !prev.done ? prev.start : null;
  });
  const upcoming = $derived(entries.filter((e) => !e.done && e.driver === me).slice(0, 4));
</script>

<h1>{t('rest.title')}</h1>

{#if entries.length === 0}
  <p class="muted">{t('rest.noPlan')}</p>
{:else}
  <label class="who">
    {t('rest.who')}
    <select bind:value={settings.restDriver} onchange={() => settings.save()}>
      <option value="">{t('rest.pick')}</option>
      {#each names as n}<option value={n}>{n}</option>{/each}
    </select>
  </label>

  {#if me && s}
    {#if !mine}
      <p class="muted big-text">{t('rest.none')}</p>
    {:else if driving}
      <div class="panel">
        <div class="muted">{t('rest.driving')}</div>
        <div class="count num">{duration(until(mine.end, live.etNow))}</div>
        <div class="muted">{t('rest.endsIn')}</div>
      </div>
    {:else}
      <div class="panel">
        <div class="muted">{t('rest.inSeconds')}</div>
        <div class="count num">{duration(until(mine.start, live.etNow))}</div>
        <div class="muted">{t('rest.at', { t: clockAt(mine.start, live.etNow, live.nowMs) })}</div>
        {#if spotFrom != null}
          <div class="spot">{t('rest.spotter', { t: clockAt(spotFrom, live.etNow, live.nowMs) })}</div>
        {/if}
      </div>
    {/if}

    {#if upcoming.length > 0}
      <ul class="list">
        {#each upcoming as e (e.no)}
          <li class="num">
            <span>#{e.no}</span>
            <span>{clockAt(e.start, live.etNow, live.nowMs)} – {clockAt(e.end, live.etNow, live.nowMs)}</span>
            <span class="muted">{duration(e.end - e.start)}</span>
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
{/if}

<style>
  .who { display: flex; flex-direction: column; gap: 6px; max-width: 360px; margin: 12px 0 20px; color: var(--muted); }
  .panel { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 24px 16px; text-align: center; max-width: 520px; }
  .count { font-size: clamp(3.5rem, 18vw, 6.5rem); font-weight: 700; line-height: 1.1; }
  .spot { margin-top: 12px; color: var(--accent); }
  .big-text { font-size: 1.2rem; }
  .list { list-style: none; padding: 0; margin: 20px 0; max-width: 520px; }
  .list li { display: flex; justify-content: space-between; gap: 12px; padding: 10px 4px; border-bottom: 1px solid var(--border); }
</style>
