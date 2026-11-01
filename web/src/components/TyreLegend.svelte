<script lang="ts">
  import { SLICK_WINDOW, windowOf } from '../lib/conditions';
  import { t } from '../lib/settings.svelte';
  import TyreWheel, { type WheelView } from './TyreWheel.svelte';

  /** A fixed drawing of one front left wheel with an arrow to each value and what it is. */
  const sample: WheelView = {
    name: 'FL',
    tread: [78, 92, 104],
    avg: 91,
    inner: 84,
    pressure: 168,
    left: 0.72,
    wear: 72,
    brake: 520,
    window: windowOf(SLICK_WINDOW.low, SLICK_WINDOW.high),
    brakeWindow: windowOf(300, 700, 150),
  };
  // The wheel sits at (150, 30); the arrows end on the value they name.
  const left = [
    { y: 43, to: [160, 43], key: 'tyres.fig.wear' },
    { y: 69, to: [160, 69], key: 'tyres.fig.mean' },
    { y: 85, to: [160, 85], key: 'tyres.fig.inner' },
    { y: 99, to: [160, 97], key: 'tyres.fig.pressure' },
    { y: 125, to: [146, 108], key: 'tyres.fig.bar' },
  ] as const;
  const right = [
    { y: 36, to: [196, 36], key: 'tyres.fig.stripes' },
    { y: 72, to: [211, 72], key: 'tyres.fig.brake' },
  ] as const;
</script>

<svg viewBox="0 0 400 150" role="img" aria-label={t('tyres.title')} class="fig">
  <defs>
    <marker id="tyre-arrow" viewBox="0 0 6 6" refX="5" refY="3" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
      <path d="M0 0 L6 3 L0 6 z" fill="var(--muted)" />
    </marker>
  </defs>
  <g transform="translate(150 30)"><TyreWheel w={sample} i={0} /></g>
  {#each left as l (l.key)}
    <text x="112" y={l.y + 3} class="label" text-anchor="end">{t(l.key)}</text>
    <line x1="116" y1={l.y} x2={l.to[0]} y2={l.to[1]} class="arrow" marker-end="url(#tyre-arrow)" />
  {/each}
  {#each right as r (r.key)}
    <text x="236" y={r.y + 3} class="label">{t(r.key)}</text>
    <line x1="232" y1={r.y} x2={r.to[0]} y2={r.to[1]} class="arrow" marker-end="url(#tyre-arrow)" />
  {/each}
  <text x="236" y="24" class="label">{t('tyres.fig.name')}</text>
  <line x1="232" y1="21" x2="190" y2="24" class="arrow" marker-end="url(#tyre-arrow)" />
</svg>
<p class="note">{t('tyres.fig.compound')}</p>

<style>
  .fig { width: 100%; height: auto; display: block; }
  .label { font-size: 10px; fill: var(--text); }
  .arrow { stroke: var(--muted); stroke-width: 1; }
  .note { margin: 6px 0 0; font-size: 0.8rem; color: var(--muted); }
</style>
