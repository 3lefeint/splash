<script lang="ts">
  import { bar, mean, rangeFor, tempColor, windowOf } from '../lib/conditions';
  import { fixed } from '../lib/format';
  import { wearColor } from '../lib/wear';
  import { settings, t } from '../lib/settings.svelte';
  import { view } from '../lib/view.svelte';

  const y = $derived(view.state?.tyres);
  const car = $derived(view.state?.car);
  const names = ['FL', 'FR', 'RL', 'RR'];
  // Front and rear can run different compounds, each with its own range.
  const ranges = $derived([
    rangeFor(y?.compound_front, settings.tyreRanges, settings.tyreFallback),
    rangeFor(y?.compound_rear, settings.tyreRanges, settings.tyreFallback),
  ]);
  const wheels = $derived(
    names.map((name, i) => {
      const tread = y?.surface?.[i];
      return {
        name,
        tread,
        avg: tread ? mean(tread) : null,
        inner: y?.inner?.[i] ? mean(y.inner[i]) : null,
        carcass: y?.carcass?.[i] ?? null,
        pressure: y?.pressure?.[i] ?? null,
        left: car?.wear?.[i] ?? null,
        wear: car?.wear?.[i] == null ? null : (settings.wearInverted ? 1 - car.wear[i] : car.wear[i]) * 100,
        window: windowOf(ranges[i < 2 ? 0 : 1].low, ranges[i < 2 ? 0 : 1].high),
      };
    }),
  );

  // Top view with the nose up. Each tyre is three stripes (left, centre, right of the tread, seen from the driver)
  // in the colour of its temperature.
  const W = 54;
  const H = 82;
  const spots = [
    { x: 8, y: 22 }, // FL
    { x: 158, y: 22 }, // FR
    { x: 8, y: 166 }, // RL
    { x: 158, y: 166 }, // RR
  ];
</script>

{#if !y}
  <p class="muted">{t('tyres.noData')}</p>
{:else}
  <div class="wrap">
    <svg class="car" viewBox="0 0 220 280" role="img" aria-label={t('tyres.title')}>
      <rect x="60" y="8" width="100" height="264" rx="44" class="body" />
      <rect x="78" y="96" width="64" height="86" rx="14" class="cabin" />
      <path d="M110 4 l0 -3" class="nose" />
      {#each wheels as w, i (w.name)}
        {@const p = spots[i]}
        <g transform="translate({p.x} {p.y})">
          {#if w.tread}
            {#each w.tread as temp, k}
              <rect x={k * (W / 3)} y="0" width={W / 3} height={H} fill={tempColor(temp, w.window)} />
            {/each}
          {:else}
            <rect x="0" y="0" width={W} height={H} class="nodata" />
          {/if}
          <rect x="0" y="0" width={W} height={H} rx="8" class="outline" />
          {#if w.left != null}
            {@const bx = i % 2 === 0 ? -7 : W + 2}
            {@const bh = Math.min(Math.max(w.left, 0), 1) * H}
            <rect x={bx} y="0" width="5" height={H} rx="2.5" class="track" />
            <rect x={bx} y={H - bh} width="5" height={bh} rx="2.5" fill={wearColor(w.left)} />
          {/if}
          <text x={W / 2} y="13" class="sub">{w.wear == null ? '' : `${fixed(w.wear, 0)} %`}</text>
          <text x={W / 2} y={H / 2 - 2} class="avg">{w.avg == null ? '–' : fixed(w.avg, 0)}<tspan class="deg">°</tspan></text>
          <text x={W / 2} y={H / 2 + 14} class="sub">{w.inner == null ? '' : `${fixed(w.inner, 0)}°`}</text>
          <text x={W / 2} y={H / 2 + 26} class="sub">{w.pressure == null ? '' : `${fixed(bar(w.pressure), 2)} bar`}</text>
          <text x={W / 2} y={i < 2 ? -6 : H + 14} class="name">{w.name}</text>
        </g>
      {/each}
    </svg>
  </div>
{/if}

<style>
  .wrap { display: flex; justify-content: center; align-items: center; height: 100%; min-height: 0; }
  .car { height: 100%; width: auto; max-width: 100%; flex: 0 1 auto; min-height: 0; display: block; }
  .body { fill: var(--surface-2); stroke: var(--border); stroke-width: 1.5; }
  .cabin { fill: var(--bg); opacity: 0.6; }
  .nose { stroke: var(--muted); stroke-width: 3; stroke-linecap: round; }
  .outline { fill: none; stroke: var(--bg); stroke-width: 2; }
  .track { fill: var(--border); }
  .nodata { fill: var(--border); }
  text { text-anchor: middle; font-variant-numeric: tabular-nums; }
  .avg { font-size: 20px; font-weight: 700; fill: #fff; paint-order: stroke; stroke: rgb(0 0 0 / 0.55); stroke-width: 3px; }
  .deg { font-size: 12px; }
  .sub { font-size: 10px; fill: #fff; paint-order: stroke; stroke: rgb(0 0 0 / 0.55); stroke-width: 2.5px; }
  .name { font-size: 11px; fill: var(--muted); }
</style>
