<script module lang="ts">
  /** Size of one drawn wheel and the width of the strip on its inner side that holds the brake disc. */
  export const W = 62;
  export const H = 82;
  export const DW = 14;

  export interface WheelView {
    name: string;
    tread?: number[];
    avg: number | null;
    inner: number | null;
    pressure: number | null;
    /** Wear left, 1 is new; null when unknown. */
    left: number | null;
    /** Wear in percent as shown. */
    wear: number | null;
    brake: number | null;
    window: TempWindow;
    brakeWindow: TempWindow;
  }
</script>

<script lang="ts">
  import { bar, tempColor, type TempWindow } from '../lib/conditions';
  import { fixed } from '../lib/format';
  import { wearColor } from '../lib/wear';

  /** One wheel seen from above in its own coordinates; an even index is a left wheel, so its inner side is on the right. */
  let { w, i }: { w: WheelView; i: number } = $props();

  const leftSide = $derived(i % 2 === 0);
  // The numbers sit in the part of the tyre that is not taken by the disc.
  const cx = $derived(leftSide ? (W - DW) / 2 : DW + (W - DW) / 2);
  const discX = $derived(leftSide ? W - DW + 2 : 2);
  const discH = 56;
  const discY = (H - discH) / 2;
</script>

{#if w.tread}
  {#each w.tread as temp, k}
    <rect x={k * (W / 3)} y="0" width={W / 3} height={H} fill={tempColor(temp, w.window)} />
  {/each}
{:else}
  <rect x="0" y="0" width={W} height={H} class="nodata" />
{/if}
<rect x="0" y="0" width={W} height={H} rx="8" class="outline" />
<rect x={discX} y={discY} width={DW - 4} height={discH} rx="3" class="disc" fill={w.brake == null ? 'var(--border)' : tempColor(w.brake, w.brakeWindow)} />
{#if w.brake != null}
  <text transform="translate({discX + (DW - 4) / 2 + 3} {H / 2}) rotate(-90)" class="brake">{fixed(w.brake, 0)}°</text>
{/if}
{#if w.left != null}
  {@const bx = leftSide ? -7 : W + 2}
  {@const bh = Math.min(Math.max(w.left, 0), 1) * H}
  <rect x={bx} y="0" width="5" height={H} rx="2.5" class="track" />
  <rect x={bx} y={H - bh} width="5" height={bh} rx="2.5" fill={wearColor(w.left)} />
{/if}
<text x={cx} y="13" class="sub">{w.wear == null ? '' : `${fixed(w.wear, 0)} %`}</text>
<text x={cx} y={H / 2 - 2} class="avg">{w.avg == null ? '–' : fixed(w.avg, 0)}<tspan class="deg">°</tspan></text>
<text x={cx} y={H / 2 + 14} class="sub">{w.inner == null ? '' : `${fixed(w.inner, 0)}°`}</text>
<text x={cx} y={H / 2 + 26} class="sub">{w.pressure == null ? '' : `${fixed(bar(w.pressure), 2)} bar`}</text>
<text x={W / 2} y={i < 2 ? -6 : H + 14} class="name">{w.name}</text>

<style>
  .outline { fill: none; stroke: var(--bg); stroke-width: 2; }
  .disc { stroke: var(--bg); stroke-width: 1.5; }
  .track { fill: var(--border); }
  .nodata { fill: var(--border); }
  text { text-anchor: middle; font-variant-numeric: tabular-nums; }
  .avg { font-size: 20px; font-weight: 700; fill: #fff; paint-order: stroke; stroke: rgb(0 0 0 / 0.55); stroke-width: 3px; }
  .deg { font-size: 12px; }
  .sub { font-size: 10px; fill: #fff; paint-order: stroke; stroke: rgb(0 0 0 / 0.55); stroke-width: 2.5px; }
  .brake { font-size: 9px; font-weight: 700; fill: #fff; paint-order: stroke; stroke: rgb(0 0 0 / 0.6); stroke-width: 2px; }
  .name { font-size: 11px; fill: var(--muted); }
</style>
