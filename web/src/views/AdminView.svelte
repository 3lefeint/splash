<script lang="ts">
  import { untrack } from 'svelte';
  import {
    adminAgents, adminCreateAgent, adminCreateTeam, adminDeleteMember, adminMembers, adminRevokeAgent, adminSetAgentEmail,
    adminSetMember, adminTeams,
  } from '../lib/api';
  import { identity } from '../lib/me.svelte';
  import { t } from '../lib/settings.svelte';
  import type { AdminAgent, AdminMember, AdminTeam, CreatedAgent } from '../lib/types';

  let teams = $state<AdminTeam[]>([]);
  let members = $state<AdminMember[]>([]);
  let agents = $state<AdminAgent[]>([]);
  let error = $state('');

  let newTeam = $state('');
  let memberEmail = $state('');
  let memberTeam = $state(0);
  let memberAdmin = $state(false);
  // The name typed into the "new agent" field of a member, by address, and the member picked for an agent without one.
  let agentNames = $state<Record<string, string>>({});
  let assignTo = $state<Record<number, string>>({});
  // The token of the agent created last. It is held here only until the admin has copied it.
  let created = $state<CreatedAgent | null>(null);

  const teamName = (id: number) => teams.find((x) => x.id === id)?.name ?? String(id);
  const agentsOf = (email: string) => agents.filter((a) => a.email === email);
  // Agents that nobody sees by default: no address, or one that is no member any more.
  const unowned = $derived(agents.filter((a) => !a.revoked && !members.some((m) => m.email === a.email)));
  const membersOfTeam = (teamId: number) => members.filter((m) => m.team_id === teamId);
  const when = (iso: string | null) => (iso ? new Date(iso).toLocaleString() : t('admin.never'));

  async function run(action: () => Promise<unknown>): Promise<void> {
    error = '';
    try {
      await action();
      await load();
    } catch (e) {
      error = t('err.generic', { msg: e instanceof Error ? e.message : String(e) });
    }
  }

  async function load(): Promise<void> {
    [teams, members, agents] = await Promise.all([adminTeams(), adminMembers(), adminAgents()]);
    if (!teams.some((x) => x.id === memberTeam)) memberTeam = teams[0]?.id ?? 0;
  }

  // The identity arrives after the page may have been opened, so load as soon as it says administrator.
  $effect(() => {
    if (identity.isAdmin) untrack(() => void run(async () => {}));
  });

  const addTeam = () =>
    run(async () => {
      await adminCreateTeam(newTeam.trim());
      newTeam = '';
    });
  const saveMember = () =>
    run(async () => {
      await adminSetMember({ email: memberEmail.trim(), team_id: memberTeam, is_admin: memberAdmin });
      memberEmail = '';
      memberAdmin = false;
    });
  const removeMember = (email: string) => {
    if (confirm(t('admin.confirmRemove', { email }))) void run(() => adminDeleteMember(email));
  };
  const addAgent = (m: AdminMember) =>
    run(async () => {
      created = await adminCreateAgent((agentNames[m.email] ?? '').trim(), m.team_id, m.email);
      agentNames[m.email] = '';
    });
  const release = (a: AdminAgent) => run(() => adminSetAgentEmail(a.id, ''));
  const assign = (a: AdminAgent) => run(async () => {
    await adminSetAgentEmail(a.id, assignTo[a.id] ?? '');
    delete assignTo[a.id];
  });
  const revoke = (a: AdminAgent) => {
    if (confirm(t('admin.confirmRevoke', { name: a.name }))) void run(() => adminRevokeAgent(a.id));
  };
  async function copyToken(): Promise<void> {
    if (created) await navigator.clipboard?.writeText(created.token).catch(() => {});
  }
</script>

<h1>{t('admin.title')}</h1>
{#if !identity.isAdmin}
  <p class="muted">{t('admin.only')}</p>
{:else}
  {#if error}<p class="err">{error}</p>{/if}

  {#if created}
    <section class="token" role="alert">
      <h2>{t('admin.tokenTitle', { name: created.name })}</h2>
      <p class="muted">{t('admin.tokenWarn')}</p>
      <code>{created.token}</code>
      <div class="row">
        <button onclick={copyToken}>{t('admin.copy')}</button>
        <button class="primary" onclick={() => (created = null)}>{t('admin.tokenDismiss')}</button>
      </div>
    </section>
  {/if}

  <section>
    <h2>{t('admin.teams')}</h2>
    <ul>
      {#each teams as x (x.id)}<li>{x.name}</li>{/each}
    </ul>
    <form class="row" onsubmit={(e) => { e.preventDefault(); void addTeam(); }}>
      <input bind:value={newTeam} placeholder={t('admin.teamName')} maxlength="60" required />
      <button class="primary" type="submit">{t('admin.addTeam')}</button>
    </form>
  </section>

  <section>
    <h2>{t('admin.members')}</h2>
    <p class="muted">{t('admin.membersHelp')}</p>
    <p class="muted">{t('admin.agentsHelp')}</p>
    <div class="scroll">
      <table>
        <thead><tr><th>{t('admin.email')}</th><th>{t('admin.team')}</th><th>{t('admin.isAdmin')}</th><th></th></tr></thead>
        {#each members as m (m.email)}
          <tbody>
            <tr class="member">
              <td>{m.email}</td><td>{teamName(m.team_id)}</td><td>{m.is_admin ? '✓' : ''}</td>
              <td><button class="danger" onclick={() => removeMember(m.email)}>{t('admin.remove')}</button></td>
            </tr>
            {#each agentsOf(m.email) as a (a.id)}
              <tr class="agent" class:gone={a.revoked}>
                <td colspan="2">↳ {a.name} <span class="muted">· {a.revoked ? t('admin.revoked') : t('admin.active')} · {t('admin.lastSeen')}: {when(a.last_seen)}</span></td>
                <td></td>
                <td class="actions">
                  {#if !a.revoked}
                    <button onclick={() => release(a)}>{t('admin.release')}</button>
                    <button class="danger" onclick={() => revoke(a)}>{t('admin.revoke')}</button>
                  {/if}
                </td>
              </tr>
            {:else}
              <tr class="agent"><td colspan="4" class="muted">↳ {t('admin.noAgents')}</td></tr>
            {/each}
            <tr class="agent">
              <td colspan="4">
                <form class="row" onsubmit={(e) => { e.preventDefault(); void addAgent(m); }}>
                  <input bind:value={agentNames[m.email]} placeholder={t('admin.agentName')} maxlength="60" required />
                  <button class="primary" type="submit">{t('admin.addAgent')}</button>
                </form>
              </td>
            </tr>
          </tbody>
        {/each}
      </table>
    </div>
    <form class="row" onsubmit={(e) => { e.preventDefault(); void saveMember(); }}>
      <input type="email" bind:value={memberEmail} placeholder={t('admin.email')} maxlength="254" required />
      <select bind:value={memberTeam} aria-label={t('admin.team')}>
        {#each teams as x (x.id)}<option value={x.id}>{x.name}</option>{/each}
      </select>
      <label class="check"><input type="checkbox" bind:checked={memberAdmin} /> {t('admin.isAdmin')}</label>
      <button class="primary" type="submit">{t('admin.saveMember')}</button>
    </form>
  </section>

  {#if unowned.length > 0}
    <section>
      <h2>{t('admin.unowned')}</h2>
      <p class="muted">{t('admin.unownedHelp')}</p>
      <div class="scroll">
        <table>
          <thead><tr><th>{t('admin.agentName')}</th><th>{t('admin.team')}</th><th>{t('admin.lastSeen')}</th><th></th></tr></thead>
          <tbody>
            {#each unowned as a (a.id)}
              <tr>
                <td>{a.name}</td><td>{teamName(a.team_id)}</td><td>{when(a.last_seen)}</td>
                <td class="actions">
                  <select bind:value={assignTo[a.id]} aria-label={t('admin.pickMember')}>
                    <option value="">{t('admin.pickMember')}</option>
                    {#each membersOfTeam(a.team_id) as m (m.email)}<option value={m.email}>{m.email}</option>{/each}
                  </select>
                  <button class="primary" disabled={!assignTo[a.id]} onclick={() => assign(a)}>{t('admin.assign')}</button>
                  <button class="danger" onclick={() => revoke(a)}>{t('admin.revoke')}</button>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </section>
  {/if}
{/if}

<style>
  section { margin-top: 24px; }
  ul { margin: 8px 0; padding-left: 20px; }
  .row { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 10px; }
  .check { display: inline-flex; align-items: center; gap: 6px; }
  .scroll { overflow-x: auto; margin-top: 12px; }
  table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
  th { text-align: left; color: var(--muted); font-weight: 500; font-size: 0.75rem; padding: 4px 10px 4px 0; white-space: nowrap; }
  td { padding: 6px 10px 6px 0; border-top: 1px solid var(--border); white-space: nowrap; }
  tr.member td { border-top: 2px solid var(--border); font-weight: 600; }
  tr.agent td { border-top: none; padding-top: 2px; font-weight: 400; }
  tr.gone { opacity: 0.6; }
  .actions { display: flex; flex-wrap: wrap; gap: 6px; }
  .token { border: 1px solid var(--accent); border-radius: 12px; padding: 12px 16px; background: var(--surface-2); }
  .token code { display: block; margin: 8px 0; padding: 8px; overflow-wrap: anywhere; user-select: all; background: var(--bg); border-radius: 8px; }
  .err { color: var(--bad, #e5534b); }
</style>
