<script lang="ts">
  import { fixed } from '../lib/format';
  import { tileInfo } from '../lib/info.svelte';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';
  import { hottest, sections } from '../lib/wear';

  const w = $derived(live.state?.wear_stats);
  const names = ['FL', 'FR', 'RL', 'RR'];
  const bars = $derived((w?.per_lap ?? []).slice(-24));
  const barMax = $derived(Math.max(...bars.map((b) => b.loss), 1e-9));
  const hot = $derived(w ? hottest(sections(w.recent, 'all')) : -1);
  const trend = $derived(w?.trend_pct ?? null);
  const hosted = tileInfo(() => (w && trend == null ? t('wear.trendWait') : null));
</script>

<div class="wear">
  <h3>{t('wear.title')}</h3>
  {#if !w}
    <p class="muted">{t('wear.noData')}</p>
  {:else}
    {#if w.previous_set}<p class="warn">{t('wear.previousSet')}</p>{/if}
    <div class="muted">{t('wear.laps', { n: w.laps })}</div>

    <div class="wheels" title={t('wear.perWheel')}>
      {#each w.per_wheel as v, i (i)}
        <div class="wheel">
          <span class="muted">{names[i]}</span>
          <span class="num">{fixed(v, 2)}</span>
        </div>
      {/each}
    </div>
    <div class="muted small">{t('wear.perLapUnit')} · {t('wear.perWheel')}</div>

    {#if trend == null}
      {#if !hosted}<p class="muted">{t('wear.trendWait')}</p>{/if}
    {:else if Math.abs(trend) < 2}
      <p>{t('wear.same')}</p>
    {:else if trend > 0}
      <p class="bad">{t('wear.worse', { n: fixed(trend, 0) })}</p>
    {:else}
      <p class="good">{t('wear.better', { n: fixed(-trend, 0) })}</p>
    {/if}

    {#if hot >= 0}
      <div class="muted">{t('wear.hottest', { n: hot + 1, m: w.segments })}</div>
    {/if}

    <svg class="bars" viewBox="0 0 {bars.length * 10} 44" role="img" aria-label={t('wear.perLap')}>
      {#each bars as b, i (b.lap)}
        <rect x={i * 10 + 1} y={44 - (b.loss / barMax) * 40} width="8" height={(b.loss / barMax) * 40} rx="1.5">
          <title>{b.lap}: {fixed(b.loss, 2)} %</title>
        </rect>
      {/each}
    </svg>
    <div class="muted small">{t('wear.perLap')}</div>
  {/if}
</div>

<style>
  .wear { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  h3 { font-size: 0.8rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.06em; }
  p { margin: 0; }
  .small { font-size: 0.8rem; }
  .wheels { display: grid; grid-template-columns: repeat(4, 1fr); gap: 6px; }
  .wheel { display: flex; flex-direction: column; align-items: center; background: var(--surface-2); border-radius: 10px; padding: 6px 2px; }
  .bars { width: 100%; height: 56px; }
  .bars rect { fill: var(--accent); opacity: 0.85; }
</style>
