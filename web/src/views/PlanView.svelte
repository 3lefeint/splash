<script lang="ts">
  import { onMount } from 'svelte';
  import { ApiError, getPlan, putPlan } from '../lib/api';
  import { clockAt, duration } from '../lib/format';
  import { drivers, generate, toCsv } from '../lib/plan';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';
  import type { PlanRow } from '../lib/types';

  const s = $derived(live.state);
  const entries = $derived(s?.stint_plan?.entries ?? []);

  let rows = $state<PlanRow[]>([]);
  let minutes = $state<number[]>([]);
  let loadFailed = $state(false);
  let message = $state<{ kind: 'ok' | 'error'; text: string } | null>(null);
  let fill = $state({ count: 22, minutes: 65, names: '' });

  function setRows(list: PlanRow[]) {
    rows = list.map((r) => ({ ...r }));
    minutes = list.map((r) => Math.round((r.duration / 60) * 10) / 10);
  }

  onMount(async () => {
    try {
      setRows(await getPlan());
      fill.names = drivers(rows).join(', ');
    } catch {
      loadFailed = true;
    }
  });

  function add() {
    rows.push({ driver: rows.at(-1)?.driver ?? '', duration: (minutes.at(-1) ?? 65) * 60 });
    minutes.push(minutes.at(-1) ?? 65);
  }

  function remove(i: number) {
    rows.splice(i, 1);
    minutes.splice(i, 1);
  }

  function move(i: number, d: -1 | 1) {
    const j = i + d;
    if (j < 0 || j >= rows.length) return;
    [rows[i], rows[j]] = [rows[j], rows[i]];
    [minutes[i], minutes[j]] = [minutes[j], minutes[i]];
  }

  function applyFill() {
    const names = fill.names.split(',').map((n) => n.trim()).filter(Boolean);
    setRows(generate(fill.count, fill.minutes, names));
  }

  async function save() {
    message = null;
    try {
      const body = rows.map((r, i) => ({ driver: r.driver.trim(), duration: Math.round(minutes[i] * 60) }));
      setRows(await putPlan(body));
      loadFailed = false;
      message = { kind: 'ok', text: t('plan.saved') };
    } catch (e) {
      message = { kind: 'error', text: t('err.generic', { msg: e instanceof ApiError ? e.message : String(e) }) };
    }
  }

  function exportCsv() {
    if (!s) return;
    const csv = toCsv(s, Date.now(), { done: t('plan.done'), current: t('plan.current'), planned: t('plan.planned') });
    const url = URL.createObjectURL(new Blob(['﻿' + csv], { type: 'text/csv;charset=utf-8' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = 'stint-plan.csv';
    a.click();
    URL.revokeObjectURL(url);
  }
</script>

<div class="head">
  <h1>{t('plan.title')}</h1>
  <div class="actions no-print">
    <button onclick={exportCsv} disabled={entries.length === 0}>{t('plan.exportCsv')}</button>
    <button onclick={() => window.print()} disabled={entries.length === 0}>{t('plan.print')}</button>
  </div>
</div>

<h2>{t('plan.recomputed')}</h2>
{#if entries.length === 0}
  <p class="muted">{t('plan.empty')}</p>
{:else if s}
  <div class="scroll">
    <table>
      <thead>
        <tr>
          <th>{t('plan.no')}</th><th>{t('plan.driver')}</th><th>{t('plan.start')}</th><th>{t('plan.end')}</th>
          <th>{t('plan.duration')}</th><th>{t('plan.status')}</th>
        </tr>
      </thead>
      <tbody>
        {#each entries as e (e.no)}
          <tr class:done={e.done} class:current={e.current}>
            <td class="num">{e.no}</td>
            <td>{e.driver}{#if e.driver_swap && !e.done}<span class="swap" title={t('plan.swap')}>⇄</span>{/if}</td>
            <td class="num">{clockAt(e.start, live.etNow, live.nowMs)}</td>
            <td class="num">{clockAt(e.end, live.etNow, live.nowMs)}</td>
            <td class="num">{duration(e.end - e.start)}</td>
            <td>{e.done ? t('plan.done') : e.current ? t('plan.current') : t('plan.planned')}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/if}

<section class="edit no-print">
  <h2>{t('plan.edit')}</h2>
  {#if loadFailed}<p class="warn">{t('plan.offlineNote')}</p>{/if}

  <details>
    <summary>{t('plan.fill')}</summary>
    <div class="fill">
      <label>{t('plan.fillCount')}<input type="number" min="1" max="200" bind:value={fill.count} /></label>
      <label>{t('plan.fillMinutes')}<input type="number" min="1" step="0.5" bind:value={fill.minutes} /></label>
      <label class="names">{t('plan.fillDrivers')}<input type="text" bind:value={fill.names} /></label>
      <button onclick={applyFill}>{t('plan.fillApply')}</button>
    </div>
  </details>

  <ol>
    {#each rows as row, i (i)}
      <li>
        <span class="num idx">{i + 1}</span>
        <input type="text" class="driver" aria-label={t('plan.driver')} bind:value={row.driver} />
        <input type="number" class="mins" min="1" step="0.5" aria-label={t('plan.minutes')} bind:value={minutes[i]} />
        <span class="muted">{t('plan.minutes')}</span>
        <button onclick={() => move(i, -1)} aria-label={t('plan.up')} disabled={i === 0}>↑</button>
        <button onclick={() => move(i, 1)} aria-label={t('plan.down')} disabled={i === rows.length - 1}>↓</button>
        <button class="danger" onclick={() => remove(i)} aria-label={t('plan.remove')}>✕</button>
      </li>
    {/each}
  </ol>

  <div class="actions">
    <button onclick={add}>{t('plan.add')}</button>
    <button class="primary" onclick={save}>{t('plan.save')}</button>
  </div>
  {#if message}<p class={message.kind === 'ok' ? 'ok' : 'error'} role="status">{message.text}</p>{/if}
</section>

<style>
  .head { display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap; margin-bottom: 12px; }
  .actions { display: flex; gap: 8px; flex-wrap: wrap; }
  .scroll { overflow-x: auto; margin: 8px 0 24px; }
  table { border-collapse: collapse; width: 100%; min-width: 480px; }
  th, td { text-align: left; padding: 10px 12px; border-bottom: 1px solid var(--border); }
  th { color: var(--muted); font-size: 0.8rem; text-transform: uppercase; letter-spacing: 0.05em; }
  tr.done { color: var(--muted); }
  tr.current { background: var(--surface-2); font-weight: 600; }
  .swap { color: var(--accent); margin-left: 8px; }
  .edit { margin-top: 16px; }
  details { margin: 12px 0; }
  summary { cursor: pointer; padding: 8px 0; }
  .fill { display: flex; gap: 12px; flex-wrap: wrap; align-items: end; }
  .fill label { display: flex; flex-direction: column; gap: 4px; font-size: 0.85rem; color: var(--muted); }
  .fill .names { flex: 1 1 260px; }
  ol { list-style: none; padding: 0; margin: 12px 0; display: grid; gap: 8px; }
  li { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .idx { width: 2rem; text-align: right; color: var(--muted); }
  .driver { flex: 1 1 140px; min-width: 0; }
  .mins { width: 6rem; }
  @media print {
    .edit { display: none; }
    tr.current { background: #eee; }
  }
</style>
