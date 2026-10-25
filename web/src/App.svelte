<script lang="ts">
  import { onMount } from 'svelte';
  import Banner from './components/Banner.svelte';
  import StatusChip from './components/StatusChip.svelte';
  import Dashboard from './views/Dashboard.svelte';
  import MapView from './views/MapView.svelte';
  import PlanView from './views/PlanView.svelte';
  import RestView from './views/RestView.svelte';
  import SessionsView from './views/SessionsView.svelte';
  import SettingsView from './views/SettingsView.svelte';
  import TracksView from './views/TracksView.svelte';
  import { announcements } from './lib/announce';
  import { announcer } from './lib/announcer.svelte';
  import { duration } from './lib/format';
  import { dashboardLayout } from './lib/layout.svelte';
  import { router, routes } from './lib/router.svelte';
  import { t, settings } from './lib/settings.svelte';
  import { alerts } from './lib/strategy';
  import { live } from './lib/store';

  onMount(() => {
    router.start();
    live.start();
    return () => live.stop();
  });

  const state = $derived(live.state);
  // The session as the team sees it: track and the car of the driven team.
  const title = $derived(state?.ready && state.car ? `${state.track} · ${state.car.vehicle_name}` : '');
  const timeLeft = $derived(state?.ready ? Math.max(state.time_left - (live.etNow - state.et), 0) : null);

  $effect(() => {
    announcer.update(announcements(state, live.etNow, alerts(state, settings.tyreRanges, settings.tyreFallback)), !!state?.ready);
  });

  // off -> beep -> voice -> off; the click also lets the browser play sound
  function cycleSound(): void {
    announcer.unlock();
    settings.soundMode = settings.soundMode === 'off' ? 'beep' : settings.soundMode === 'beep' ? 'voice' : 'off';
    settings.save();
    if (settings.soundMode !== 'off') announcer.test();
  }

  $effect(() => {
    document.documentElement.lang = settings.lang;
  });
</script>

<header class="no-print">
  <div class="start">
    {#if router.route === 'dashboard'}
      <button class="gear" class:on={dashboardLayout.edit} onclick={() => (dashboardLayout.edit = !dashboardLayout.edit)} aria-pressed={dashboardLayout.edit} aria-label={t('layout.edit')} title={t('layout.edit')}>
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
        </svg>
      </button>
    {/if}
    <nav aria-label="Main">
    {#each routes as r}
      <a href="#/{r}" class:active={router.route === r} aria-current={router.route === r ? 'page' : undefined}>
        {t(`nav.${r}` as 'nav.plan')}
      </a>
    {/each}
    </nav>
  </div>
  <div class="title" title={title}>{title}</div>
  <div class="right">
    {#if timeLeft != null}
      <span class="left num" title={t('dash.timeLeft')}><span class="muted label">{t('dash.timeLeft')}</span> <strong>{duration(timeLeft)}</strong></span>
    {/if}
    <button class="snd" class:on={settings.soundMode !== 'off'} onclick={cycleSound} title={t('sound.title')} aria-label={t('sound.title')}>{t(`sound.${settings.soundMode}` as 'sound.off')}</button>
    <StatusChip />
  </div>
  <Banner />
</header>

<main>
  {#if router.route === 'dashboard'}
    <Dashboard />
  {:else if router.route === 'plan'}
    <PlanView />
  {:else if router.route === 'map'}
    <MapView />
  {:else if router.route === 'sessions'}
    <SessionsView />
  {:else if router.route === 'settings'}
    <SettingsView />
  {:else if router.route === 'tracks'}
    <TracksView />
  {:else}
    <RestView />
  {/if}
</main>

<style>
  header {
    position: sticky;
    top: 0;
    z-index: 5;
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 12px;
    padding: 8px 16px;
    padding-top: max(8px, env(safe-area-inset-top));
    background: color-mix(in srgb, var(--bg) 92%, transparent);
    backdrop-filter: blur(6px);
    border-bottom: 1px solid var(--border);
  }
  .start { display: flex; align-items: center; gap: 4px; min-width: 0; }
  .gear { width: 44px; min-width: 44px; padding: 0; color: var(--muted); background: transparent; border-color: transparent; display: inline-flex; align-items: center; justify-content: center; }
  .gear:hover, .gear.on { color: var(--text); background: var(--surface-2); }
  @media (max-width: 639px) { .gear { display: none; } }
  .title { text-align: center; color: var(--muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
  .right { display: flex; align-items: center; gap: 14px; }
  .snd { min-height: 32px; padding: 0 10px; font-size: 0.8rem; color: var(--muted); background: transparent; border-color: var(--border); white-space: nowrap; }
  .snd.on { color: var(--accent); border-color: var(--accent); }
  .left { white-space: nowrap; }
  .left strong { font-size: 1.1rem; }
  .left .label { font-size: 0.8rem; }
  @media (max-width: 1000px) { .title { display: none; } header { grid-template-columns: minmax(0, 1fr) auto; } }
  nav { display: flex; gap: 2px; overflow-x: auto; min-width: 0; }
  nav a {
    padding: 10px 12px;
    border-radius: 10px;
    color: var(--muted);
    text-decoration: none;
    white-space: nowrap;
    min-height: 44px;
    display: inline-flex;
    align-items: center;
  }
  nav a.active { background: var(--surface-2); color: var(--text); }
  @media (max-width: 520px) {
    header { padding-left: 8px; padding-right: 12px; gap: 8px; }
    .left .label { display: none; }
    .right { gap: 10px; }
    nav a { padding: 10px 6px; font-size: 0.85rem; }
  }
  main { padding: 12px 16px; padding-bottom: max(16px, env(safe-area-inset-bottom)); }
</style>
