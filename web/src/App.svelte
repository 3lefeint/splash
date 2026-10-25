<script lang="ts">
  import { onMount } from 'svelte';
  import StatusChip from './components/StatusChip.svelte';
  import Dashboard from './views/Dashboard.svelte';
  import PlanView from './views/PlanView.svelte';
  import RestView from './views/RestView.svelte';
  import SettingsView from './views/SettingsView.svelte';
  import { router, routes } from './lib/router.svelte';
  import { t, settings } from './lib/settings.svelte';
  import { live } from './lib/store';

  onMount(() => {
    router.start();
    live.start();
    return () => live.stop();
  });

  $effect(() => {
    document.documentElement.lang = settings.lang;
  });
</script>

<header class="no-print">
  <nav aria-label="Main">
    {#each routes as r}
      <a href="#/{r}" class:active={router.route === r} aria-current={router.route === r ? 'page' : undefined}>
        {t(`nav.${r}` as 'nav.plan')}
      </a>
    {/each}
  </nav>
  <StatusChip />
</header>

<main>
  {#if router.route === 'dashboard'}
    <Dashboard />
  {:else if router.route === 'plan'}
    <PlanView />
  {:else if router.route === 'settings'}
    <SettingsView />
  {:else}
    <RestView />
  {/if}
</main>

<style>
  header {
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 16px;
    padding-top: max(8px, env(safe-area-inset-top));
    background: color-mix(in srgb, var(--bg) 92%, transparent);
    backdrop-filter: blur(6px);
    border-bottom: 1px solid var(--border);
  }
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
    nav a { padding: 10px 8px; font-size: 0.9rem; }
  }
  main { padding: 16px; padding-bottom: max(16px, env(safe-area-inset-bottom)); max-width: 1400px; margin: 0 auto; }
</style>
