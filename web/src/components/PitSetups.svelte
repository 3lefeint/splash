<script lang="ts">
  import { applyPitSetup, deletePitSetup, getPitSetups, pitRequest, putPitSetup } from '../lib/api';
  import { EXAMPLES, stepFromRow, stepText } from '../lib/setups';
  import { tileInfo } from '../lib/info.svelte';
  import { setupsUi } from '../lib/setups.svelte';
  import { t } from '../lib/settings.svelte';
  import { live } from '../lib/store';
  import type { PitSetup, Step } from '../lib/types';

  const car = $derived(live.state?.car?.vehicle_name ?? '');
  const menu = $derived(live.state?.pit_menu);
  const command = $derived(live.state?.command);
  let setups = $state<PitSetup[]>([]);
  let error = $state('');
  let busy = $state(false);
  let editing = $state<PitSetup | null>(null);

  async function load(): Promise<void> {
    if (!car) {
      setups = [];
      return;
    }
    try {
      setups = await getPitSetups(car);
    } catch (e) {
      error = message(e);
    }
  }

  const message = (e: unknown) => t('err.generic', { msg: e instanceof Error ? e.message : String(e) });

  $effect(() => {
    car;
    void load();
  });

  async function run(fn: () => Promise<unknown>): Promise<void> {
    busy = true;
    error = '';
    try {
      await fn();
    } catch (e) {
      error = message(e);
    } finally {
      busy = false;
    }
  }

  const requested = $derived(live.state?.car?.pit_state === 1);
  const inLane = $derived((live.state?.car?.pit_state ?? 0) > 1);
  const request = () => run(() => pitRequest(requested ? t('setups.requestOff') : t('setups.request')));
  const apply = (s: PitSetup) => run(() => applyPitSetup(s.name, s.steps));
  const addExamples = () =>
    run(async () => {
      for (const ex of EXAMPLES) await putPitSetup({ car, name: ex.name, steps: ex.steps });
      await load();
    });
  const save = () =>
    run(async () => {
      if (!editing) return;
      const steps = editing.steps.filter((s) => s.category.trim() && s.choice.trim());
      await putPitSetup({ ...editing, car, name: editing.name.trim(), steps });
      editing = null;
      await load();
    });
  const remove = (s: PitSetup) =>
    run(async () => {
      await deletePitSetup(s.car, s.name);
      editing = null;
      await load();
    });

  const copy = (s: PitSetup): PitSetup => structuredClone($state.snapshot(s));

  // A short press applies the setup, a long press (or a right click) opens it for editing.
  const LONG_PRESS_MS = 600;
  let pressTimer: ReturnType<typeof setTimeout> | null = null;
  let longPressed = false;
  function pressStart(s: PitSetup): void {
    longPressed = false;
    pressCancel();
    pressTimer = setTimeout(() => {
      longPressed = true;
      pressTimer = null;
      editing = copy(s);
    }, LONG_PRESS_MS);
  }
  function pressEnd(s: PitSetup): void {
    if (!pressTimer) return; // it was a long press, or the press did not start on this button
    pressCancel();
    if (!longPressed) void apply(s);
  }
  function pressCancel(): void {
    if (pressTimer) clearTimeout(pressTimer);
    pressTimer = null;
  }

  tileInfo(() => (editing ? t('setups.valueHint') : setups.length > 0 ? t('setups.hint') : null));

  // The "+" in the header of the tile asks for a new setup.
  let seen = setupsUi.requests;
  $effect(() => {
    if (setupsUi.requests !== seen) {
      seen = setupsUi.requests;
      editing = blank();
    }
  });

  const blank = (): PitSetup => ({ car, name: '', steps: [{ category: '', choice: '' }] });
  const addStep = (step?: Step) => editing?.steps.push(step ?? { category: '', choice: '' });
</script>

{#if !car}
  <p class="muted">{t('setups.noCar')}</p>
{:else if editing}
  <div class="form">
    <input class="name" placeholder={t('setups.name')} bind:value={editing.name} maxlength="40" />
    {#each editing.steps as s, i}
      <div class="step">
        <input list="rows" placeholder={t('setups.row')} bind:value={s.category} maxlength="40" />
        <input placeholder={t('setups.value')} bind:value={s.choice} maxlength="40" />
        <button onclick={() => editing?.steps.splice(i, 1)} aria-label={t('setups.removeStep')}>×</button>
      </div>
    {/each}
    <datalist id="rows">
      <option value="FUEL:"></option>
      <option value="TIRES:"></option>
      <option value="F TIRES:"></option>
      <option value="R TIRES:"></option>
      <option value="FL TIRE:"></option>
      <option value="FR TIRE:"></option>
      <option value="RL TIRE:"></option>
      <option value="RR TIRE:"></option>
      <option value="DRIVER:"></option>
      <option value="DAMAGE:"></option>
    </datalist>
    <div class="row">
      <button onclick={() => addStep()}>{t('setups.addStep')}</button>
      <button disabled={!menu} onclick={() => menu && addStep(stepFromRow(menu))}>{t('setups.fromGame')}</button>
    </div>
    <div class="row">
      <button class="primary" disabled={busy || !editing.name.trim() || !editing.steps.some((s) => s.category.trim() && s.choice.trim())} onclick={save}>{t('setups.save')}</button>
      <button onclick={() => (editing = null)}>{t('setups.cancel')}</button>
      {#if setups.some((s) => s.name === editing?.name)}
        <button class="danger" disabled={busy} onclick={() => editing && remove(editing)}>{t('setups.delete')}</button>
      {/if}
    </div>
  </div>
{:else}
  {#if setups.length === 0}
    <p class="muted">{t('setups.none')}</p>
    <button disabled={busy} onclick={addExamples}>{t('setups.examples')}</button>
  {:else}
    <div class="grid">
      {#each setups as s (s.name)}
        <button
          class="setup"
          disabled={busy || command?.state === 'pending'}
          onpointerdown={() => pressStart(s)}
          onpointerup={() => pressEnd(s)}
          onpointerleave={pressCancel}
          onpointercancel={pressCancel}
          oncontextmenu={(e) => { e.preventDefault(); editing = copy(s); }}
        >
          <b>{s.name}</b>
          <span class="muted small">{s.steps.map(stepText).join(', ')}</span>
        </button>
      {/each}
    </div>
  {/if}
  <div class="row">
    <button class="request" class:on={requested} disabled={busy || inLane || command?.state === 'pending'} onclick={request}>{requested ? t('setups.requestOff') : t('setups.request')}</button>
  </div>
  {#if command}
    <p class="status {command.state}">
      <b>{command.name}</b>:
      {#if command.state === 'pending'}{t('setups.pending')}
      {:else if command.state === 'done'}{t('setups.done')}{command.applied?.length ? ' – ' + command.applied.map((a) => `${a.category} ${a.choice}`).join(', ') : ''}
      {:else}{t('setups.failed')}: {command.message}{/if}
    </p>
  {/if}
{/if}
{#if error}<p class="err">{error}</p>{/if}

<style>
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(8.5rem, 1fr)); gap: 8px; }
  .setup {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 4px;
    padding: 10px 12px;
    text-align: left;
    background: var(--surface-2);
    border: 1px solid var(--border);
    border-radius: 10px;
    touch-action: manipulation;
    user-select: none;
    -webkit-user-select: none;
    -webkit-touch-callout: none;
  }
  .setup:hover:not(:disabled) { border-color: var(--accent); }
  .setup:disabled { opacity: 0.6; }
  .request.on { border-color: var(--warn, #e5a50a); color: var(--warn, #e5a50a); }
  .form { display: grid; gap: 6px; min-width: 0; }
  .form input { min-width: 0; width: 100%; box-sizing: border-box; }
  .step { display: grid; grid-template-columns: 1fr 1fr auto; gap: 6px; }
  .row { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 4px; }
  .small { font-size: 0.8rem; margin: 0; }
  .status { margin: 8px 0 0; font-size: 0.9rem; }
  .status.failed, .err { color: var(--bad, #e5534b); }
  .status.done { color: var(--good, #3fb950); }
</style>
