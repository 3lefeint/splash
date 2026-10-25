<script lang="ts">
  import { corners } from '../lib/corners.svelte';
  import { byPosition, cornerColor, parseNames } from '../lib/corners';
  import { fixed } from '../lib/format';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';

  let text = $state('');
  // the car position is only of use while the session runs on the track being edited
  const here = $derived(live.state?.track === corners.track ? (live.state?.field?.find((c) => c.ours)?.lap_dist ?? null) : null);
  const active = $derived(corners.selected != null ? corners.list[corners.selected] : null);

  function add(): void {
    const added = parseNames(text, corners.list);
    if (added.length === 0) return;
    text = '';
    void corners.save([...corners.list, ...added]);
  }

  function rename(i: number, name: string): void {
    const n = name.trim().slice(0, 40);
    if (!n) return;
    void corners.save(corners.list.map((c, k) => (k === i ? { ...c, name: n } : c)));
  }

  function remove(i: number): void {
    corners.stop();
    void corners.save(corners.list.filter((_, k) => k !== i));
  }

  function edit(i: number, end: 'from' | 'to', value: string): void {
    const d = value === '' ? null : Number(value);
    if (d != null && (!Number.isFinite(d) || d < 0)) return;
    void corners.save(corners.list.map((c, k) => (k === i ? { ...c, [end]: d == null ? null : Math.round(d) } : c)));
  }
</script>

<div class="corners">
  <h3>{t('corner.editor')}</h3>
  {#if active}
    <p class="active">{t(corners.step === 'from' ? 'corner.clickFrom' : 'corner.clickTo', { name: active.name })}</p>
    {#if here != null}
      <button onclick={() => void corners.place(here)}>{t('corner.here')} ({fixed(here, 0)} m)</button>
    {/if}
  {:else}
    <p class="muted small">{t('corner.hint')}</p>
  {/if}
  <ul>
    {#each byPosition(corners.list) as { corner: c, i } (i)}
      <li class:sel={corners.selected === i}>
        <span class="dot" style="background:{cornerColor(i)}"></span>
        <input class="name" value={c.name} maxlength="40" aria-label={t('corner.name')} onchange={(e) => rename(i, e.currentTarget.value)} />
        <button class="pick" onclick={() => (corners.selected === i ? corners.stop() : corners.mark(i))} aria-pressed={corners.selected === i}>
          {corners.selected === i ? t('corner.cancel') : c.from == null || c.to == null ? t('corner.set') : t('corner.again')}
        </button>
        <button onclick={() => remove(i)} aria-label={t('corner.remove')} title={t('corner.remove')}>×</button>
        <div class="range">
          <input type="number" min="0" step="10" value={c.from ?? ''} placeholder={t('corner.from')} aria-label={t('corner.from')} onchange={(e) => edit(i, 'from', e.currentTarget.value)} />
          <span class="muted">–</span>
          <input type="number" min="0" step="10" value={c.to ?? ''} placeholder={t('corner.to')} aria-label={t('corner.to')} onchange={(e) => edit(i, 'to', e.currentTarget.value)} />
          <span class="muted small">m</span>
          {#if c.label}<button class="reset" onclick={() => void corners.setLabel(i, null)}>{t('corner.labelReset')}</button>{/if}
        </div>
      </li>
    {/each}
  </ul>
  <label class="muted small" for="corner-names">{t('corner.names')}</label>
  <textarea id="corner-names" rows="4" bind:value={text}></textarea>
  <button onclick={add} disabled={!text.trim()}>{t('corner.add')}</button>
  {#if corners.error}<p class="error small">{corners.error}</p>{/if}
</div>

<style>
  .corners { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  h3 { font-size: 0.8rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.06em; }
  p { margin: 0; }
  .small { font-size: 0.8rem; }
  .active { padding: 8px 10px; border-radius: 8px; background: var(--accent); color: var(--bg); font-weight: 600; }
  ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 8px; max-height: 45vh; overflow-y: auto; }
  li { display: grid; grid-template-columns: 0.8rem minmax(0, 1fr) auto auto; gap: 4px 6px; align-items: center; padding: 2px; }
  li.sel { outline: 2px solid var(--accent); outline-offset: 2px; border-radius: 8px; }
  .dot { width: 0.8rem; height: 0.8rem; border-radius: 50%; }
  .range { grid-column: 2 / 5; display: flex; align-items: center; gap: 4px; flex-wrap: wrap; }
  .range input { width: 4.8rem; }
  .pick { padding: 4px 8px; white-space: nowrap; }
  .reset { padding: 2px 6px; font-size: 0.75rem; margin-left: auto; }
  input { min-width: 0; }
  textarea { width: 100%; resize: vertical; }
</style>
