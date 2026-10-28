<script lang="ts">
  import { onMount } from 'svelte';
  import { corners } from '../lib/corners.svelte';
  import { cornerColor, leader, stretches as cornerStretches } from '../lib/corners';
  import { settings, t } from '../lib/settings.svelte';
  import { live } from '../lib/store';
  import { Animator, carNumber, classColor, fitView, hideKey, pointAt } from '../lib/trackmap';
  import { trackMaps } from '../lib/trackmaps.svelte';
  import { paintFor } from '../lib/overlays';
  import { deltaColor, heatColor } from '../lib/wear';
  import type { FieldCar } from '../lib/types';

  let { compact = false }: { compact?: boolean } = $props();

  const s = $derived(live.state);
  const map = $derived(trackMaps.map);
  const view = $derived(map ? fitView(map.points) : null);
  // While the outline is recorded: the lap driven so far, as an open line that grows.
  const progress = $derived(!map ? trackMaps.progress : null);
  const partial = $derived.by(() => {
    if (!progress || progress.points.length < 2) return null;
    const v = fitView(progress.points);
    const [x, y] = v.project(progress.points[progress.points.length - 1]);
    return { viewBox: v.viewBox, path: v.path.replace(/ Z$/, ''), x, y };
  });
  const classes = $derived((s?.field ?? []).map((c) => c.class));

  const paint = $derived(paintFor(s, settings.mapOverlay, settings.mapWheel, settings.mapTarget));

  // One short path per track section, painted in the colour of its value.
  const sectionPaths = $derived.by(() => {
    if (!map || !view || !paint) return [];
    const n = paint.values.length;
    const per = map.points.length / n;
    const out: { d: string; color: string }[] = [];
    paint.values.forEach((v, i) => {
      if (v == null) return;
      const a = Math.round(i * per);
      const b = Math.round((i + 1) * per);
      const pts = [];
      for (let k = a; k <= b; k++) {
        const [x, y] = view.project(map.points[k % map.points.length]);
        pts.push(`${k === a ? 'M' : 'L'}${Math.round(x * 10) / 10} ${Math.round(y * 10) / 10}`);
      }
      out.push({ d: pts.join(' '), color: paint.mode === 'diverging' ? deltaColor(v, paint.max) : heatColor(v, paint.max) });
    });
    return out;
  });

  // How fast each rival is against us: green where we gain on it, red where it gains on us.
  const rates = $derived(new Map((s?.strategy?.rivals ?? []).map((r) => [`${r.car_id}|${r.vehicle_name}`, r.rate])));
  const passing = $derived(new Set(settings.mapGhost ? (s?.strategy?.rejoin?.passed_by ?? []) : []));
  const pits = $derived(settings.mapPits ? s?.sections?.pits : undefined);
  const rejoin = $derived(settings.mapGhost ? s?.strategy?.rejoin : undefined);

  function paceColor(c: FieldCar): string | null {
    const rate = rates.get(`${c.car_id}|${c.vehicle_name}`);
    if (!settings.mapPace || rate == null || Math.abs(rate) < 0.15) return null;
    return rate > 0 ? 'var(--good)' : 'var(--bad)';
  }

  const animator = new Animator(1);
  let now = $state(0);

  $effect(() => trackMaps.watch(s?.track));

  $effect(() => {
    if (s?.field && s.track_length) animator.update(s.field, performance.now(), s.track_length);
  });

  onMount(() => {
    let raf = 0;
    const loop = () => {
      now = performance.now();
      raf = requestAnimationFrame(loop);
    };
    raf = requestAnimationFrame(loop);
    return () => cancelAnimationFrame(raf);
  });

  function visible(c: FieldCar): boolean {
    if (c.ours) return true;
    return (
      settings.mapOpponents && !settings.mapHiddenClasses.includes(c.class) && !settings.mapHiddenCars.includes(hideKey(c))
    );
  }

  // Size of the view box in metres, so dots and text keep the same size on screen for any track.
  const unit = $derived.by(() => {
    if (!view) return 1;
    const vb = view.viewBox.split(' ').map(Number);
    return Math.max(vb[2], vb[3]);
  });

  const dots = $derived.by(() => {
    if (!map || !view || !s?.field) return [];
    // Ours is drawn last so it is never hidden behind another car.
    return s.field
      .filter(visible)
      .map((c) => {
        const d = animator.at(c, now) ?? c.lap_dist;
        const [x, y] = view.project(pointAt(map, d));
        const pace = c.ours ? null : paceColor(c);
        return {
          c, x, y,
          color: c.ours ? 'var(--accent)' : (pace ?? classColor(classes, c.class)),
          fill: c.ours ? 'var(--text)' : null,
          passes: passing.has(c.driver || c.vehicle_name),
        };
      })
      .sort((a, b) => Number(a.c.ours) - Number(b.c.ours));
  });

  // Named corners, as marked in the track editor: the stretch in a colour and the name beside it.
  const stretches = $derived(compact || !map || !view ? [] : cornerStretches(map, view, corners.list));

  // A short bar across the track marks the start line.
  const startBar = $derived.by(() => {
    if (!map || !view) return null;
    const [x0, y0] = view.project(map.points[0]);
    const [x1, y1] = view.project(map.points[1]);
    const len = Math.hypot(x1 - x0, y1 - y0) || 1;
    const nx = (-(y1 - y0) / len) * unit * 0.018;
    const ny = ((x1 - x0) / len) * unit * 0.018;
    return { x1: x0 - nx, y1: y0 - ny, x2: x0 + nx, y2: y0 + ny };
  });
</script>

{#if !s?.field || !s.track_length}
  <p class="muted">{t('map.noPositions')}</p>
{:else if (!map || !view) && partial && progress}
  <svg class="map" class:compact viewBox={partial.viewBox} role="img" aria-label={t('map.title')}>
    <path d={partial.path} class="edge" vector-effect="non-scaling-stroke" />
    <path d={partial.path} class="line recording" vector-effect="non-scaling-stroke" />
    <circle cx={partial.x} cy={partial.y} r="1" class="head" vector-effect="non-scaling-stroke" />
  </svg>
  <p class="muted small">{t('map.recording', { pct: Math.round(progress.covered * 100) })}</p>
{:else if !map || !view}
  <p class="muted">{t('map.waiting')}</p>
{:else}
  <svg class="map" class:compact class:fcy={s?.fcy} viewBox={view.viewBox} role="img" aria-label={t('map.title')}>
    <path d={view.path} class="edge" vector-effect="non-scaling-stroke" />
    <path d={view.path} class="line" vector-effect="non-scaling-stroke" />
    {#each sectionPaths as p, i (i)}
      <path d={p.d} class="section" stroke={p.color} vector-effect="non-scaling-stroke" />
    {/each}
    {#if pits}
      {#each pits.boxes as b}
        {@const [bx, by] = view.project(b)}
        <circle cx={bx} cy={by} r={unit * 0.0065} class="box" />
      {/each}
      {#if pits.our_box}
        {@const [ox, oy] = view.project(pits.our_box)}
        <circle cx={ox} cy={oy} r={unit * 0.011} class="ourbox" vector-effect="non-scaling-stroke" />
      {/if}
      {#if pits.entry}
        {@const [ex, ey] = view.project(pits.entry)}
        <circle cx={ex} cy={ey} r={unit * 0.008} class="gate" />
        {#if !compact}<text x={ex + unit * 0.012} y={ey - unit * 0.008} font-size={unit * 0.02} class="pitlabel">{t('map.pitIn')}</text>{/if}
      {/if}
      {#if pits.exit}
        {@const [xx, xy] = view.project(pits.exit)}
        <circle cx={xx} cy={xy} r={unit * 0.008} class="gate" />
        {#if !compact}<text x={xx + unit * 0.012} y={xy + unit * 0.038} font-size={unit * 0.02} class="pitlabel">{t('map.pitOut')}</text>{/if}
      {/if}
    {/if}
    {#if rejoin && pits?.exit}
      {@const [gx, gy] = view.project(pits.exit)}
      <circle cx={gx} cy={gy} r={unit * 0.02} class="ghost" vector-effect="non-scaling-stroke" />
      <text x={gx + unit * 0.025} y={gy - unit * 0.028} font-size={unit * 0.022} class="ghosttext">{t('map.rejoin', { pos: rejoin.position })}</text>
    {/if}
    {#each stretches as st (st.i)}
      {#if st.d}<path d={st.d} class="stretch" stroke={cornerColor(st.i)} vector-effect="non-scaling-stroke" />{/if}
    {/each}
    {#each stretches as st (st.i)}
      {#if st.d}
        {@const color = cornerColor(st.i)}
        {@const [lx, ly] = st.corner.label ? view.project(st.corner.label) : [st.mid[0] + unit * 0.02, st.mid[1] - unit * 0.02]}
        <line {...leader(st.pts, st.corner.name, lx, ly, unit * 0.02)} class="leader" stroke={color} vector-effect="non-scaling-stroke" />
        <text x={lx} y={ly} font-size={unit * 0.02} class="cornername" fill={color}>{st.corner.name}</text>
      {/if}
    {/each}
    {#if startBar}
      <line {...startBar} class="start" vector-effect="non-scaling-stroke" />
    {/if}
    {#each dots as d (d.c.car_id + '|' + d.c.vehicle_name)}
      <g transform="translate({d.x} {d.y})">
        <circle
          r={unit * (d.c.ours ? 0.017 : 0.011)}
          fill={d.c.in_pits ? 'var(--bg)' : (d.fill ?? d.color)}
          stroke={d.color}
          stroke-width={d.c.ours ? 3.5 : 1.5}
          vector-effect="non-scaling-stroke"
        >
          <title>{carNumber(d.c.vehicle_name)} {d.c.driver || d.c.vehicle_name}{d.c.in_pits ? ` (${t('map.pit')})` : ''}</title>
        </circle>
        {#if d.passes}
          <circle r={unit * (d.c.ours ? 0.017 : 0.011) + unit * 0.01} class="passes" vector-effect="non-scaling-stroke" />
        {/if}
        {#if d.c.off_track}
          <circle r={unit * 0.022} class="off" vector-effect="non-scaling-stroke"><title>{t('map.offTrack')}</title></circle>
        {/if}
        {#if !compact}
          <text x={unit * 0.016} y={unit * 0.006} font-size={unit * 0.022} fill={d.c.ours ? 'var(--accent)' : 'var(--muted)'}>
            {d.c.ours ? t('map.you') : carNumber(d.c.vehicle_name)}
          </text>
        {/if}
      </g>
    {/each}
  </svg>
{/if}

<style>
  .map { width: 100%; height: auto; max-height: 70vh; display: block; }
  .map.compact { height: 100%; max-height: none; min-height: 0; flex: 1; }
  .edge { fill: none; stroke: var(--border); stroke-width: 12; stroke-linejoin: round; }
  .line { fill: none; stroke: var(--surface-2); stroke-width: 8; stroke-linejoin: round; }
  .section { fill: none; stroke-width: 8; stroke-linecap: butt; stroke-linejoin: round; }
  .line.recording { stroke: var(--accent); }
  .head { fill: var(--accent); stroke: var(--text); stroke-width: 10; }
  .small { font-size: 0.8rem; margin: 4px 0 0; }
  .fcy .edge { stroke: #f5c542; }
  .box { fill: var(--muted); opacity: 0.8; }
  .ourbox { fill: none; stroke: var(--accent); stroke-width: 3; }
  .gate { fill: var(--surface-2); stroke: var(--text); stroke-width: 2; vector-effect: non-scaling-stroke; }
  .pitlabel { fill: var(--muted); }
  .ghost { fill: none; stroke: var(--accent); stroke-width: 3; stroke-dasharray: 6 5; }
  .ghosttext { fill: var(--accent); font-weight: 600; }
  .passes { fill: none; stroke: #ffb454; stroke-width: 3; stroke-dasharray: 3 3; }
  .off { fill: none; stroke: var(--bad); stroke-width: 3; }
  .stretch { fill: none; stroke-width: 8; stroke-linecap: butt; stroke-linejoin: round; opacity: 0.9; }
  .leader { stroke-width: 1; opacity: 0.7; }
  .cornername { font-weight: 700; paint-order: stroke; stroke: var(--bg); stroke-width: 3px; stroke-linejoin: round; }
  .start { stroke: var(--text); stroke-width: 3; }
  text { font-variant-numeric: tabular-nums; pointer-events: none; }
</style>
