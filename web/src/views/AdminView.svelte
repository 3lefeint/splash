<script lang="ts">
  import { untrack } from 'svelte';
  import {
    adminAgents, adminCars, adminCreateCar, adminCreateTeam, adminDeleteCar, adminDeleteMember,
    adminMapCarName, adminUnmapCarName, adminMembers, adminRevokeAgent, adminSetAgentEmail,
    adminSetMember, adminTeams,
  } from '../lib/api';
  import MyAgentPanel from '../components/MyAgentPanel.svelte';
  import { identity } from '../lib/me.svelte';
  import { t } from '../lib/settings.svelte';
  import type { AdminAgent, AdminMember, AdminTeam, TeamCars } from '../lib/types';

  let teams = $state<AdminTeam[]>([]);
  let members = $state<AdminMember[]>([]);
  let agents = $state<AdminAgent[]>([]);
  let error = $state('');
  let carTeam = $state(0);
  let teamCars = $state<TeamCars>({ cars: [], unmapped: [] });
  let newCar = $state('');
  let nameFor = $state<Record<number, string>>({});

  let newTeam = $state('');
  let memberEmail = $state('');
  let memberTeam = $state(0);
  let memberAdmin = $state(false);
  let memberName = $state('');
  let memberShort = $state('');
  let memberCfId = $state('');
  // The Cloudflare secret is only ever written: the field stays empty after saving, and empty keeps the stored one.
  let memberCfSecret = $state('');
  let memberClearCf = $state(false);
  // The member picked for an agent that belongs to nobody.
  let assignTo = $state<Record<number, string>>({});

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
    if (!teams.some((x) => x.id === carTeam)) carTeam = teams[0]?.id ?? 0;
    if (carTeam) teamCars = await adminCars(carTeam);
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
      await adminSetMember({
        email: memberEmail.trim(), team_id: memberTeam, is_admin: memberAdmin, name: memberName.trim(), short: memberShort.trim(),
        cf_client_id: memberCfId.trim(), ...(memberCfSecret.trim() ? { cf_secret: memberCfSecret.trim() } : {}),
        ...(memberClearCf ? { clear_cf: true } : {}),
      });
      resetMemberForm();
    });
  function resetMemberForm(): void {
    memberEmail = memberName = memberShort = memberCfId = memberCfSecret = '';
    memberAdmin = memberClearCf = false;
  }
  // Fills the form with a member to change; the secret field stays empty.
  function editMember(m: AdminMember): void {
    memberEmail = m.email;
    memberTeam = m.team_id;
    memberAdmin = m.is_admin;
    memberName = m.name;
    memberShort = m.short;
    memberCfId = m.cf_client_id;
    memberCfSecret = '';
    memberClearCf = false;
  }
  const removeMember = (email: string) => {
    if (confirm(t('admin.confirmRemove', { email }))) void run(() => adminDeleteMember(email));
  };
  const addCar = () =>
    run(async () => {
      await adminCreateCar(carTeam, newCar.trim());
      newCar = '';
    });
  const removeCar = (c: { id: number; name: string }) => {
    if (confirm(t('admin.confirmDeleteCar', { name: c.name }))) void run(() => adminDeleteCar(carTeam, c.id));
  };
  const assignName = (id: number) =>
    run(async () => {
      await adminMapCarName(carTeam, id, (nameFor[id] ?? '').trim());
      nameFor[id] = '';
    });
  const release = (a: AdminAgent) => run(() => adminSetAgentEmail(a.id, ''));
  const assign = (a: AdminAgent) => run(async () => {
    await adminSetAgentEmail(a.id, assignTo[a.id] ?? '');
    delete assignTo[a.id];
  });
  const revoke = (a: AdminAgent) => {
    if (confirm(t('admin.confirmRevoke', { name: a.name }))) void run(() => adminRevokeAgent(a.id));
  };
</script>

<h1>{t('admin.title')}</h1>
<MyAgentPanel />
{#if identity.isAdmin}
  {#if error}<p class="err">{error}</p>{/if}

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
        <thead>
          <tr>
            <th>{t('admin.email')}</th><th>{t('admin.memberName')}</th><th>{t('admin.short')}</th><th>{t('admin.team')}</th>
            <th>{t('admin.isAdmin')}</th><th>{t('admin.cloudflare')}</th><th></th>
          </tr>
        </thead>
        {#each members as m (m.email)}
          <tbody>
            <tr class="member">
              <td>{m.email}</td><td>{m.name || '–'}</td><td>{m.short || '–'}</td><td>{teamName(m.team_id)}</td><td>{m.is_admin ? '✓' : ''}</td>
              <td title={m.cf_client_id}>{m.cf_client_id ? (m.has_cf_secret ? '✓' : t('admin.cfNoSecret')) : '–'}</td>
              <td class="actions">
                <button onclick={() => editMember(m)}>{t('admin.edit')}</button>
                <button class="danger" onclick={() => removeMember(m.email)}>{t('admin.remove')}</button>
              </td>
            </tr>
            {#each agentsOf(m.email) as a (a.id)}
              <tr class="agent" class:gone={a.revoked}>
                <td colspan="5">↳ {a.name} <span class="muted">· {a.revoked ? t('admin.revoked') : t('admin.active')} · {t('admin.lastSeen')}: {when(a.last_seen)}</span></td>
                <td></td>
                <td class="actions">
                  {#if !a.revoked}
                    <button onclick={() => release(a)}>{t('admin.release')}</button>
                    <button class="danger" onclick={() => revoke(a)}>{t('admin.revoke')}</button>
                  {/if}
                </td>
              </tr>
            {:else}
              <tr class="agent"><td colspan="7" class="muted">↳ {t('admin.noAgents')}</td></tr>
            {/each}
          </tbody>
        {/each}
      </table>
    </div>
    <form class="form" onsubmit={(e) => { e.preventDefault(); void saveMember(); }}>
      <div class="row">
        <input type="email" bind:value={memberEmail} placeholder={t('admin.email')} maxlength="254" required />
        <select bind:value={memberTeam} aria-label={t('admin.team')}>
          {#each teams as x (x.id)}<option value={x.id}>{x.name}</option>{/each}
        </select>
        <label class="check"><input type="checkbox" bind:checked={memberAdmin} /> {t('admin.isAdmin')}</label>
      </div>
      <div class="row">
        <input bind:value={memberName} placeholder={t('admin.memberName')} maxlength="60" />
        <input bind:value={memberShort} placeholder={t('admin.short')} maxlength="8" pattern={'[A-Za-z0-9]{2,8}'} title={t('admin.shortHelp')} />
      </div>
      <div class="row">
        <input bind:value={memberCfId} placeholder={t('admin.cfId')} maxlength="200" autocomplete="off" />
        <input type="password" bind:value={memberCfSecret} placeholder={t('admin.cfSecret')} maxlength="500" autocomplete="new-password" />
        <label class="check"><input type="checkbox" bind:checked={memberClearCf} /> {t('admin.cfClear')}</label>
      </div>
      <p class="muted small">{t('admin.cfHelp')}</p>
      <div class="row">
        <button class="primary" type="submit">{t('admin.saveMember')}</button>
        <button type="button" onclick={resetMemberForm}>{t('admin.clearForm')}</button>
      </div>
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

  <section>
    <h2>{t('admin.cars')}</h2>
    <p class="muted">{t('admin.carsHelp')}</p>
    <div class="row">
      <select bind:value={carTeam} onchange={() => void run(async () => {})} aria-label={t('admin.team')}>
        {#each teams as x (x.id)}<option value={x.id}>{x.name}</option>{/each}
      </select>
    </div>
    {#if teamCars.unmapped.length > 0}
      <p class="muted">{t('admin.unmappedNames', { names: teamCars.unmapped.join(', ') })}</p>
    {/if}
    <datalist id="unmapped-names">
      {#each teamCars.unmapped as n (n)}<option value={n}></option>{/each}
    </datalist>
    {#each teamCars.cars as c (c.id)}
      <div class="car">
        <div class="row">
          <strong>{c.name}</strong>
          <button class="danger" onclick={() => removeCar(c)}>{t('admin.deleteCar')}</button>
        </div>
        <ul class="names">
          {#each c.names as n (n)}
            <li>{n} <button class="x" aria-label={t('admin.remove')} onclick={() => void run(() => adminUnmapCarName(carTeam, n))}>×</button></li>
          {:else}
            <li class="muted">{t('admin.noNames')}</li>
          {/each}
        </ul>
        <form class="row" onsubmit={(e) => { e.preventDefault(); void assignName(c.id); }}>
          <input list="unmapped-names" bind:value={nameFor[c.id]} placeholder={t('admin.vehicleName')} maxlength="120" required />
          <button type="submit">{t('admin.assignName')}</button>
        </form>
      </div>
    {:else}
      <p class="muted">{t('admin.noCars')}</p>
    {/each}
    <form class="row" onsubmit={(e) => { e.preventDefault(); void addCar(); }}>
      <input bind:value={newCar} placeholder={t('admin.carName')} maxlength="120" required />
      <button class="primary" type="submit">{t('admin.addCar')}</button>
    </form>
  </section>
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
  .form { margin-top: 14px; }
  .small { font-size: 0.8rem; }
  .actions { display: flex; flex-wrap: wrap; gap: 6px; }
  .car { margin-top: 14px; padding: 10px 12px; border: 1px solid var(--border); border-radius: 10px; }
  .names { display: flex; flex-wrap: wrap; gap: 6px; list-style: none; margin: 8px 0; padding: 0; }
  .names li { padding: 2px 8px; border-radius: 999px; background: var(--surface-2); }
  .x { min-height: 0; padding: 0 4px; border: none; background: transparent; color: var(--muted); }
  .err { color: var(--bad, #e5534b); }
</style>
