<script lang="ts">
  import { ApiError, clearOverride, getParams, putParams, setOverride } from '../lib/api';
  import { fixed } from '../lib/format';
  import { settings, t } from '../lib/settings.svelte';
  import { live } from '../lib/store';
  import type { OverrideKey, Params } from '../lib/types';

  const track = $derived(live.state?.track ?? '');
  const car = $derived(live.state?.car?.vehicle_name ?? '');

  const blank = (track: string, car: string): Params => ({
    track, car, pit_through_green: 0, pit_through_fcy: 0, refuel_rate: 0, tyre_change: 0, driver_swap: 0,
    parallel_service: false, fuel_effect: 0, deg_per_lap: 0, save_cost: 0, reserve: 3,
  });

  let form = $state<Params>(blank('', ''));
  let stored = $state(true);
  let paramMsg = $state<{ kind: 'ok' | 'error'; text: string } | null>(null);
  let overrideInput = $state<Record<OverrideKey, number | null>>({ fuel: null, fuel_per_lap: null });
  let overrideError = $state('');

  // Load the parameter set of the running track and car whenever they change.
  $effect(() => {
    const tr = track;
    const c = car;
    paramMsg = null;
    if (!tr || !c) return;
    getParams(tr, c)
      .then((p) => {
        stored = p != null;
        form = p ?? blank(tr, c);
      })
      .catch((e) => (paramMsg = { kind: 'error', text: t('err.generic', { msg: String(e) }) }));
  });

  async function saveParams() {
    paramMsg = null;
    try {
      form = await putParams({ ...form });
      stored = true;
      paramMsg = { kind: 'ok', text: t('set.saved') };
    } catch (e) {
      paramMsg = { kind: 'error', text: t('err.generic', { msg: e instanceof ApiError ? e.message : String(e) }) };
    }
  }

  async function apply(key: OverrideKey) {
    overrideError = '';
    const v = overrideInput[key];
    if (v == null || !Number.isFinite(v)) return;
    try {
      await setOverride(key, v);
      overrideInput[key] = null;
    } catch (e) {
      overrideError = t('err.generic', { msg: e instanceof ApiError ? e.message : String(e) });
    }
  }

  async function clear(key: OverrideKey) {
    overrideError = '';
    try {
      await clearOverride(key);
    } catch (e) {
      overrideError = t('err.generic', { msg: String(e) });
    }
  }

  const fields: { key: keyof Params; label: Parameters<typeof t>[0]; step: number }[] = [
    { key: 'pit_through_green', label: 'set.pitThroughGreen', step: 0.1 },
    { key: 'pit_through_fcy', label: 'set.pitThroughFcy', step: 0.1 },
    { key: 'refuel_rate', label: 'set.refuelRate', step: 0.05 },
    { key: 'tyre_change', label: 'set.tyreChange', step: 0.1 },
    { key: 'driver_swap', label: 'set.driverSwap', step: 0.1 },
    { key: 'fuel_effect', label: 'set.fuelEffect', step: 0.001 },
    { key: 'deg_per_lap', label: 'set.degPerLap', step: 0.001 },
    { key: 'save_cost', label: 'set.saveCost', step: 0.01 },
    { key: 'reserve', label: 'set.reserve', step: 0.1 },
  ];

  const overrides: { key: OverrideKey; label: Parameters<typeof t>[0]; live: () => number | null | undefined }[] = [
    { key: 'fuel', label: 'set.overrideFuel', live: () => live.state?.car?.fuel },
    { key: 'fuel_per_lap', label: 'set.overrideFuelPerLap', live: () => live.state?.car?.fuel_per_lap },
  ];
</script>

<h1>{t('set.title')}</h1>

<section>
  <h2>{t('set.params')}</h2>
  {#if !track || !car}
    <p class="muted">{t('set.noSession')}</p>
  {:else}
    <p class="muted">{t('set.paramsFor', { track, car })}</p>
    {#if !stored}<p class="warn">{t('set.defaults')}</p>{/if}
    <form
      onsubmit={(e) => {
        e.preventDefault();
        saveParams();
      }}
    >
      <div class="fields">
        {#each fields as f (f.key)}
          <label>{t(f.label)}
            <input type="number" min="0" step={f.step} bind:value={form[f.key] as number} />
          </label>
        {/each}
      </div>
      <label class="check"><input type="checkbox" bind:checked={form.parallel_service} />{t('set.parallel')}</label>
      <button class="primary" type="submit">{t('set.save')}</button>
    </form>
  {/if}
  {#if paramMsg}<p class={paramMsg.kind === 'ok' ? 'ok' : 'error'} role="status">{paramMsg.text}</p>{/if}
</section>

<section>
  <h2>{t('set.overrides')}</h2>
  <p class="muted">{t('set.overridesHelp')}</p>
  {#each overrides as o (o.key)}
    {@const active = live.state?.overrides[o.key]}
    <div class="override">
      <div class="olabel">
        {t(o.label)}
        <span class="muted num">{t('set.live', { v: fixed(o.live(), 2) })}</span>
        {#if active}<span class="badge">{t('set.active')}: {fixed(active.value, 2)}</span>{/if}
      </div>
      <input type="number" min="0" step="0.1" bind:value={overrideInput[o.key]} aria-label={t(o.label)} />
      <button onclick={() => apply(o.key)} disabled={overrideInput[o.key] == null}>{t('set.apply')}</button>
      <button class="danger" onclick={() => clear(o.key)} disabled={!active}>{t('set.clear')}</button>
    </div>
  {/each}
  {#if overrideError}<p class="error">{overrideError}</p>{/if}
</section>

<section>
  <h2>{t('set.display')}</h2>
  <label class="check">
    {t('set.language')}
    <select bind:value={settings.lang} onchange={() => settings.save()}>
      <option value="de">Deutsch</option>
      <option value="en">English</option>
    </select>
  </label>
  <label class="check">
    <input type="checkbox" bind:checked={settings.wearInverted} onchange={() => settings.save()} />
    {t('set.wearInverted')}
  </label>
  <p class="muted">{t('set.wearHelp')}</p>
</section>

<style>
  section { margin: 24px 0; max-width: 900px; }
  .fields { display: grid; gap: 12px; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); margin: 12px 0; }
  label { display: flex; flex-direction: column; gap: 4px; font-size: 0.9rem; color: var(--muted); }
  label.check { flex-direction: row; align-items: center; gap: 10px; color: var(--text); margin: 10px 0; }
  form button { margin-top: 8px; }
  .override { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin: 10px 0; }
  .olabel { flex: 1 1 220px; display: flex; flex-direction: column; }
  .override input { width: 8rem; }
  .badge { font-size: 0.75rem; font-weight: 600; padding: 2px 8px; border-radius: 999px; background: var(--warn); color: #231600; align-self: flex-start; margin-top: 2px; }
</style>
