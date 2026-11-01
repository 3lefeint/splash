<script lang="ts">
  import { BRAKE_MARGIN, mean, rangeFor, windowOf } from '../lib/conditions';
  import { tileFigure } from '../lib/info.svelte';
  import TyreLegend from './TyreLegend.svelte';
  import TyreWheel from './TyreWheel.svelte';
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
  const brakeWindow = $derived(windowOf(settings.brakeRange.low, settings.brakeRange.high, BRAKE_MARGIN));
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
        brake: y?.brake?.[i] ?? null,
        brakeWindow,
      };
    }),
  );

  // Top view with the nose up. Each tyre is three stripes (left, centre, right of the tread, seen from the driver)
  // in the colour of its temperature, with the brake disc on its inner side.
  const spots = [
    { x: 12, y: 22 }, // FL
    { x: 140, y: 22 }, // FR
    { x: 12, y: 130 }, // RL
    { x: 140, y: 130 }, // RR
  ];
  const compounds = $derived([y?.compound_front, y?.compound_rear]);
  // The figure behind the "i" button names every value of a wheel.
  tileFigure(TyreLegend);

  /** Short enough for the narrow car body. */
  const short = (c: string | undefined) => (c ? (c.length > 9 ? `${c.slice(0, 8)}…` : c) : '');
</script>

{#if !y}
  <p class="muted">{t('tyres.noData')}</p>
{:else}
  <div class="wrap">
    <svg class="car" viewBox="0 0 216 240" role="img" aria-label={t('tyres.title')}>
      <rect x="86" y="8" width="44" height="224" rx="20" class="body" />
      <rect x="94" y="92" width="28" height="64" rx="10" class="cabin" />
      <path d="M108 4 l0 -3" class="nose" />
      {#each [0, 1] as axle (axle)}
        {#if compounds[axle]}
          <text x="108" y={axle === 0 ? 66 : 174} class="comp">{short(compounds[axle])}</text>
        {/if}
      {/each}
      {#each wheels as w, i (w.name)}
        {@const p = spots[i]}
        <g transform="translate({p.x} {p.y})"><TyreWheel {w} {i} /></g>
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
  .comp { font-size: 8px; fill: var(--muted); text-anchor: middle; }
</style>
