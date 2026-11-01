<script lang="ts">
  import { onMount } from 'svelte';
  import { getMyAgent, issueAgentConfig } from '../lib/api';
  import { saveText } from '../lib/download';
  import { t } from '../lib/settings.svelte';
  import type { MyAgent } from '../lib/types';

  let info = $state<MyAgent | null>(null);
  let error = $state('');
  let busy = $state(false);
  let issued = $state(false);

  async function load(): Promise<void> {
    try {
      info = await getMyAgent();
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    }
  }
  onMount(() => void load());

  async function download(): Promise<void> {
    if (info?.agent && !confirm(t('myagent.confirmNew'))) return;
    busy = true;
    error = '';
    try {
      saveText('agent.toml', await issueAgentConfig());
      issued = true;
      await load();
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    } finally {
      busy = false;
    }
  }

  const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString() : t('admin.never'));
</script>

<section>
  <h2>{t('myagent.title')}</h2>
  {#if error}<p class="err">{error}</p>{/if}
  {#if !info}
    <p class="muted">…</p>
  {:else if info.reason === 'no_member'}
    <p class="muted">{t('myagent.noMember')}</p>
  {:else}
    <p class="muted">{t('myagent.help')}</p>
    {#if info.agent}
      <p>{t('myagent.agent', { name: info.agent.name })} <span class="muted">· {t('admin.lastSeen')}: {when(info.agent.last_seen)}</span></p>
    {:else}
      <p class="muted">{t('myagent.none')}</p>
    {/if}
    {#if info.reason === 'no_short'}
      <p class="warn">{t('myagent.noShort')}</p>
    {:else if info.reason === 'no_cloudflare'}
      <p class="warn">{t('myagent.noCloudflare')}</p>
    {/if}
    <button class="primary" disabled={!info.can_download || busy} onclick={download}>
      {info.agent ? t('myagent.renew') : t('myagent.download')}
    </button>
    {#if issued}
      <div class="steps" role="status">
        <b>{t('myagent.next')}</b>
        <ol>
          <li>{t('myagent.step1')}</li>
          <li>{t('myagent.step2')}</li>
          <li>{t('myagent.step3')}</li>
        </ol>
      </div>
    {/if}
  {/if}
</section>

<style>
  section { margin-top: 24px; }
  .warn { color: var(--warn, #e5a50a); }
  .err { color: var(--bad, #e5534b); }
  .steps { margin-top: 14px; padding: 10px 14px; border-radius: 10px; background: var(--surface-2); border-left: 4px solid var(--accent); }
  .steps ol { margin: 6px 0 0; padding-left: 20px; }
</style>
