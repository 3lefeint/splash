<script lang="ts">
  import { change, isWet, spark } from '../lib/conditions';
  import { fixed, signed } from '../lib/format';
  import { tileInfo } from '../lib/info.svelte';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';

  const w = $derived(live.state?.weather);
  const track = $derived(w ? spark(w.history.map((p) => p.track), 200, 48) : null);
  const ambient = $derived(w ? spark(w.history.map((p) => p.ambient), 200, 48) : null);
  const trend = $derived(w ? change(w.history, 'track') : null);
  tileInfo(() => (w && !track ? t('weather.collecting') : null));
  const wet = $derived(w ? isWet(w.rain, w.wetness) : false);
  const pct = (v: number | null | undefined) => (v == null ? '–' : `${fixed(v * 100, 0)} %`);
</script>

{#if !w}
  <p class="muted">{t('weather.noData')}</p>
{:else}
  <div class="row">
    <div class="temp">
      <span class="muted small">{t('weather.track')}</span>
      <span class="big num">{w.track == null ? '–' : fixed(w.track, 1)}<span class="unit">°C</span></span>
      {#if trend != null}<span class="num small {trend > 0.4 ? 'warn' : trend < -0.4 ? 'accent' : 'muted'}">{signed(trend, 1)} °C</span>{/if}
    </div>
    <div class="temp">
      <span class="muted small">{t('weather.air')}</span>
      <span class="big num">{w.ambient == null ? '–' : fixed(w.ambient, 1)}<span class="unit">°C</span></span>
    </div>
    <div class="temp">
      <span class="muted small">{t('weather.rain')}</span>
      <span class="big num {wet ? 'warn' : ''}">{pct(w.rain)}</span>
      <span class="small muted">{t('weather.wet')} {pct(w.wetness)}</span>
    </div>
  </div>
  {#if track}
    <svg class="chart" viewBox="0 0 200 48" preserveAspectRatio="none" role="img" aria-label={t('weather.history')}>
      {#if ambient}<path d={ambient.path} class="air" vector-effect="non-scaling-stroke" />{/if}
      <path d={track.path} class="trk" vector-effect="non-scaling-stroke" />
    </svg>
    <div class="legend small muted">
      <span><i class="dot trkc"></i>{t('weather.track')} {fixed(track.min, 0)}–{fixed(track.max, 0)} °C</span>
      {#if ambient}<span><i class="dot airc"></i>{t('weather.air')}</span>{/if}
    </div>
  {/if}
{/if}

<style>
  .row { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
  .temp { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .big { font-size: clamp(1.3rem, 2.6vw, 2rem); font-weight: 700; line-height: 1.1; }
  .unit { font-size: 0.55em; margin-left: 2px; color: var(--muted); font-weight: 500; }
  .small { font-size: 0.78rem; }
  .accent { color: var(--accent); }
  .chart { width: 100%; height: 56px; margin-top: 6px; }
  .chart path { fill: none; stroke-width: 2; stroke-linejoin: round; }
  .trk { stroke: var(--warn); }
  .air { stroke: var(--accent); opacity: 0.8; }
  .legend { display: flex; gap: 14px; flex-wrap: wrap; }
  .dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%; margin-right: 5px; }
  .trkc { background: var(--warn); }
  .airc { background: var(--accent); }
</style>
