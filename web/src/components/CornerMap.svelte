<script lang="ts">
  import { corners } from '../lib/corners.svelte';
  import { cornerColor, leader, distAt, stretches as cornerStretches } from '../lib/corners';
  import { getTrackMap } from '../lib/api';
  import { settings, t } from '../lib/settings.svelte';
  import { fitView } from '../lib/trackmap';
  import type { TrackMap } from '../lib/types';

  let { track, reload = 0 }: { track: string; reload?: number } = $props();

  let map = $state<TrackMap | null>(null);
  let loading = $state(true);
  let svgEl = $state<SVGSVGElement | null>(null);
  let dragLabel = $state<{ i: number; at: [number, number] } | null>(null);

  $effect(() => {
    void reload;
    const name = track;
    loading = true;
    map = null;
    getTrackMap(name).then(
      (m) => {
        if (name === track) map = m;
      },
      () => {},
    ).finally(() => {
      if (name === track) loading = false;
    });
  });

  const view = $derived(map ? fitView(map.points) : null);

  // Zoom: a factor above 1 shows a smaller part of the track, around a centre in screen coordinates.
  const MAX_ZOOM = 40;
  let zoom = $state(1);
  let centre = $state<[number, number] | null>(null);
  const full = $derived(view ? (view.viewBox.split(' ').map(Number) as [number, number, number, number]) : null);
  const box = $derived.by((): [number, number, number, number] | null => {
    if (!full) return null;
    const [x, y, w, h] = full;
    if (zoom <= 1 || !centre) return full;
    const bw = w / zoom;
    const bh = h / zoom;
    const cx = Math.min(Math.max(centre[0], x + bw / 2), x + w - bw / 2);
    const cy = Math.min(Math.max(centre[1], y + bh / 2), y + h - bh / 2);
    return [cx - bw / 2, cy - bh / 2, bw, bh];
  });
  // Size of what is shown in metres, so names and dots keep their size on screen at any zoom.
  const unit = $derived(box ? Math.max(box[2], box[3]) : 1);

  $effect(() => {
    void track;
    zoom = 1;
    centre = null;
  });

  function zoomTo(factor: number, at?: [number, number]): void {
    if (!box || !full) return;
    const next = Math.min(Math.max(zoom * factor, 1), MAX_ZOOM);
    if (next === zoom) return;
    const [bx, by, bw, bh] = box;
    // the point under the cursor stays where it is
    const p = at ?? [bx + bw / 2, by + bh / 2];
    const k = zoom / next;
    centre = [p[0] + (bx + bw / 2 - p[0]) * k, p[1] + (by + bh / 2 - p[1]) * k];
    zoom = next;
  }

  // Not passive, so the page does not scroll while the wheel zooms the map.
  $effect(() => {
    const el = svgEl;
    if (!el) return;
    el.addEventListener('wheel', wheel, { passive: false });
    return () => el.removeEventListener('wheel', wheel);
  });

  function wheel(e: WheelEvent): void {
    e.preventDefault();
    const p = screenAt(e);
    zoomTo(e.deltaY < 0 ? 1.25 : 0.8, p ?? undefined);
  }

  // Screen coordinates (the view box space) of a pointer position.
  function screenAt(e: PointerEvent | MouseEvent | WheelEvent): [number, number] | null {
    const ctm = svgEl?.getScreenCTM();
    if (!svgEl || !ctm) return null;
    const pt = svgEl.createSVGPoint();
    pt.x = e.clientX;
    pt.y = e.clientY;
    const p = pt.matrixTransform(ctm.inverse());
    return [p.x, p.y];
  }

  // A drag on the map moves it, a click without moving places the corner that is being marked.
  let pan = $state<{ x: number; y: number; start: [number, number]; centre: [number, number]; moved: boolean } | null>(null);

  function down(e: PointerEvent): void {
    if (e.button !== 0 || !box) return;
    const start = screenAt(e);
    if (!start) return;
    (e.currentTarget as Element).setPointerCapture(e.pointerId);
    pan = { x: e.clientX, y: e.clientY, start, centre: centre ?? [box[0] + box[2] / 2, box[1] + box[3] / 2], moved: false };
  }

  function move(e: PointerEvent): void {
    if (!pan || !svgEl || !box) return;
    if (!pan.moved && Math.hypot(e.clientX - pan.x, e.clientY - pan.y) < 5) return;
    pan.moved = true;
    if (zoom <= 1) return;
    const rect = svgEl.getBoundingClientRect();
    const scale = box[2] / rect.width; // view box units per pixel (the aspect ratio is kept)
    centre = [pan.centre[0] - (e.clientX - pan.x) * scale, pan.centre[1] - (e.clientY - pan.y) * scale];
  }

  function up(e: PointerEvent): void {
    const p = pan;
    pan = null;
    if (!p || p.moved || corners.selected == null || !map) return;
    const at = worldAt(e);
    if (at) void corners.place(distAt(map.points, map.length, at));
  }
  const stretches = $derived(map && view ? cornerStretches(map, view, corners.list) : []);

  // Screen y is world z upside down.
  function worldAt(e: PointerEvent | MouseEvent): [number, number] | null {
    const ctm = svgEl?.getScreenCTM();
    if (!svgEl || !ctm) return null;
    const pt = svgEl.createSVGPoint();
    pt.x = e.clientX;
    pt.y = e.clientY;
    const p = pt.matrixTransform(ctm.inverse());
    return [p.x, -p.y];
  }

  function labelDown(e: PointerEvent, i: number): void {
    if (e.button !== 0) return;
    (e.currentTarget as Element).setPointerCapture(e.pointerId);
    e.stopPropagation();
    const at = worldAt(e);
    if (at) dragLabel = { i, at };
  }

  function labelMove(e: PointerEvent): void {
    if (!dragLabel) return;
    const at = worldAt(e);
    if (at) dragLabel = { ...dragLabel, at };
  }

  function labelUp(): void {
    if (!dragLabel) return;
    const { i, at } = dragLabel;
    dragLabel = null;
    void corners.setLabel(i, [Math.round(at[0]), Math.round(at[1])]);
  }
</script>

{#if loading}
  <p class="muted">{t('tracks.loading')}</p>
{:else if !map || !view}
  <p class="muted">{t('map.waiting')}</p>
{:else}
  <div class="zoom">
    <button onclick={() => zoomTo(1.5)} aria-label={t('tracks.zoomIn')} title={t('tracks.zoomIn')}>+</button>
    <button onclick={() => zoomTo(1 / 1.5)} disabled={zoom <= 1} aria-label={t('tracks.zoomOut')} title={t('tracks.zoomOut')}>−</button>
    <button onclick={() => { zoom = 1; centre = null; }} disabled={zoom <= 1}>{t('tracks.zoomReset')}</button>
    <span class="muted small">{t('tracks.zoomHint')}</span>
  </div>
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <svg bind:this={svgEl} class="map" class:picking={corners.selected != null} class:panning={pan?.moved} viewBox={box?.join(' ')} role="img" aria-label={t('map.title')}
    onpointerdown={down} onpointermove={move} onpointerup={up} onpointercancel={() => (pan = null)}>
    <path d={view.path} class="edge" vector-effect="non-scaling-stroke" />
    <path d={view.path} class="line" vector-effect="non-scaling-stroke" />
    {#each stretches as st (st.i)}
      {#if st.d}<path d={st.d} class="stretch" stroke={cornerColor(st.i)} vector-effect="non-scaling-stroke" />{/if}
    {/each}
    {#each stretches as st (st.i)}
      {@const color = cornerColor(st.i)}
      {#if !st.d}<circle cx={st.start[0]} cy={st.start[1]} r={unit * 0.01} fill={color} class="startmark" />{/if}
      {#if st.d}
        {@const lp = dragLabel?.i === st.i ? dragLabel.at : st.corner.label}
        {@const [lx, ly] = lp ? view.project(lp) : [st.mid[0] + unit * 0.02, st.mid[1] - unit * 0.02]}
        <line {...leader(st.pts, st.corner.name, lx, ly, unit * 0.02)} class="leader" stroke={color} vector-effect="non-scaling-stroke" />
        <text x={lx} y={ly} font-size={unit * 0.02} class="cornername" fill={color} role="presentation"
          onpointerdown={(e) => labelDown(e, st.i)} onpointermove={labelMove} onpointerup={labelUp} onpointercancel={labelUp}>{st.corner.name}</text>
      {/if}
    {/each}
  </svg>
{/if}

<style>
  p { margin: 0; }
  .map { width: 100%; height: auto; max-height: 78vh; display: block; }
  .map { touch-action: none; cursor: grab; }
  .map.picking { cursor: crosshair; }
  .map.panning { cursor: grabbing; }
  .zoom { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .small { font-size: 0.8rem; }
  .edge { fill: none; stroke: var(--border); stroke-width: 12; stroke-linejoin: round; }
  .line { fill: none; stroke: var(--surface-2); stroke-width: 8; stroke-linejoin: round; }
  .stretch { fill: none; stroke-width: 8; stroke-linecap: butt; stroke-linejoin: round; opacity: 0.9; }
  .startmark { stroke: var(--bg); stroke-width: 1.5; vector-effect: non-scaling-stroke; }
  .leader { stroke-width: 1; opacity: 0.7; }
  .cornername { font-weight: 700; font-variant-numeric: tabular-nums; paint-order: stroke; stroke: var(--bg); stroke-width: 3px; stroke-linejoin: round; pointer-events: all; cursor: grab; touch-action: none; user-select: none; }
</style>
