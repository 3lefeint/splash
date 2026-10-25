<script lang="ts">
  import { onMount } from 'svelte';
  import CornerMap from '../components/CornerMap.svelte';
  import CornersPanel from '../components/CornersPanel.svelte';
  import { deleteTrackMap, listTracks } from '../lib/api';
  import { corners } from '../lib/corners.svelte';
  import { fixed } from '../lib/format';
  import { t } from '../lib/settings.svelte';
  import { readJson, safeStorage, writeJson } from '../lib/storage';
  import { live } from '../lib/store';
  import { trackMaps } from '../lib/trackmaps.svelte';
  import type { TrackInfo } from '../lib/types';

  let tracks = $state<TrackInfo[]>([]);
  let loaded = $state(false);
  let selected = $state<string | null>(null);
  let reload = $state(0);
  let error = $state('');

  // Widths of the track list and the corner panel, set by dragging the bars between the columns.
  const KEY = 'splash.tracks.widths';
  const LIMITS = { list: [140, 420], panel: [240, 640] } as const;
  const clamp = (n: number, [lo, hi]: readonly [number, number]) => Math.min(Math.max(Math.round(n), lo), hi);
  const saved = readJson<{ list?: number; panel?: number }>(safeStorage(), KEY);
  let widths = $state({ list: clamp(saved?.list ?? 200, LIMITS.list), panel: clamp(saved?.panel ?? 320, LIMITS.panel) });
  let drag = $state<{ which: 'list' | 'panel'; x: number; width: number } | null>(null);

  function begin(e: PointerEvent, which: 'list' | 'panel'): void {
    if (e.button !== 0) return;
    (e.currentTarget as Element).setPointerCapture(e.pointerId);
    drag = { which, x: e.clientX, width: widths[which] };
    e.preventDefault();
  }

  function move(e: PointerEvent): void {
    if (!drag) return;
    // the list grows to the right, the panel to the left
    const dx = (e.clientX - drag.x) * (drag.which === 'list' ? 1 : -1);
    widths[drag.which] = clamp(drag.width + dx, LIMITS[drag.which]);
  }

  function end(): void {
    if (!drag) return;
    drag = null;
    writeJson(safeStorage(), KEY, widths);
  }

  function key(e: KeyboardEvent, which: 'list' | 'panel'): void {
    const step = e.key === 'ArrowRight' ? 20 : e.key === 'ArrowLeft' ? -20 : 0;
    if (!step) return;
    e.preventDefault();
    widths[which] = clamp(widths[which] + step * (which === 'list' ? 1 : -1), LIMITS[which]);
    writeJson(safeStorage(), KEY, widths);
  }

  async function refresh(): Promise<void> {
    try {
      tracks = await listTracks();
      if (selected && !tracks.some((x) => x.track === selected)) selected = null;
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    }
    loaded = true;
  }

  onMount(() => {
    void refresh().then(() => {
      // start with the track of the running session, else the only one
      const now = live.state?.track;
      if (!selected) selected = tracks.find((x) => x.track === now)?.track ?? (tracks.length === 1 ? tracks[0].track : null);
    });
  });

  $effect(() => {
    corners.stop();
    if (selected) corners.watch(selected);
  });

  async function rerecord(): Promise<void> {
    if (!selected || !confirm(t('map.confirm'))) return;
    error = '';
    try {
      await deleteTrackMap(selected);
      if (selected === live.state?.track) trackMaps.reload();
      await refresh();
      reload++;
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    }
  }
</script>

<div class="head">
  <h1>{t('tracks.title')}</h1>
</div>

{#if loaded && tracks.length === 0}
  <p class="muted">{t('tracks.none')}</p>
{:else}
  <div class="layout" style="--list:{widths.list}px;--panel:{widths.panel}px">
    <nav class="list" aria-label={t('tracks.title')}>
      {#each tracks as x (x.track)}
        <button class:on={selected === x.track} aria-pressed={selected === x.track} onclick={() => (selected = x.track)}>
          <span>{x.track}</span>
          <span class="muted small">{t('tracks.length', { n: fixed(x.length / 1000, 1) })}</span>
        </button>
      {/each}
    </nav>
    {#if selected}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (a focusable separator is a widget in ARIA) -->
      <div class="bar" class:on={drag?.which === 'list'} role="separator" aria-orientation="vertical" aria-label={t('tracks.resize')} aria-valuenow={widths.list}
        aria-valuemin={LIMITS.list[0]} aria-valuemax={LIMITS.list[1]} tabindex="0"
        onpointerdown={(e) => begin(e, 'list')} onpointermove={move} onpointerup={end} onpointercancel={end} onkeydown={(e) => key(e, 'list')}></div>
      <section class="panel"><CornerMap track={selected} {reload} /></section>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions (a focusable separator is a widget in ARIA) -->
      <div class="bar" class:on={drag?.which === 'panel'} role="separator" aria-orientation="vertical" aria-label={t('tracks.resize')} aria-valuenow={widths.panel}
        aria-valuemin={LIMITS.panel[0]} aria-valuemax={LIMITS.panel[1]} tabindex="0"
        onpointerdown={(e) => begin(e, 'panel')} onpointermove={move} onpointerup={end} onpointercancel={end} onkeydown={(e) => key(e, 'panel')}></div>
      <aside class="panel">
        <CornersPanel />
        <button class="danger" onclick={rerecord}>{t('map.rerecord')}</button>
        {#if error}<p class="error">{error}</p>{/if}
      </aside>
    {:else}
      <p class="muted pick">{t('tracks.pick')}</p>
    {/if}
  </div>
{/if}

<style>
  .head { display: flex; align-items: baseline; gap: 12px; margin-bottom: var(--gap); }
  p { margin: 0; }
  .small { font-size: 0.8rem; }
  .layout { display: grid; gap: var(--gap); grid-template-columns: 1fr; align-items: start; }
  @media (min-width: 900px) {
    .layout { grid-template-columns: var(--list) 10px minmax(0, 1fr) 10px var(--panel); gap: 0; }
    .layout > .panel { margin: 0; }
  }
  .bar { display: none; }
  @media (min-width: 900px) {
    .bar { display: block; align-self: stretch; min-height: 12rem; cursor: col-resize; touch-action: none; position: relative; }
    .bar::after { content: ''; position: absolute; top: 0; bottom: 0; left: 3px; width: 4px; border-radius: 2px; background: var(--border); transition: background 0.1s; }
    .bar:hover::after, .bar:focus-visible::after, .bar.on::after { background: var(--accent); }
    .list { padding-right: 0; }
  }
  .list { display: flex; flex-direction: column; gap: 6px; }
  .list button { display: flex; flex-direction: column; align-items: flex-start; text-align: left; gap: 2px; }
  .list button.on { outline: 2px solid var(--accent); outline-offset: 1px; }
  .panel { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 14px 16px; display: flex; flex-direction: column; gap: 12px; min-width: 0; }
  .pick { grid-column: 3 / -1; }
</style>
