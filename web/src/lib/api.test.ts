import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  adminCars, adminDeleteMember, adminMapCarName, adminUnmapCarName, adminRevokeAgent, adminSetAgentEmail, adminSetMember, ApiError, clearOverride,
  getMe, getParams, issueAgentConfig, pitRequest, putPlan, setOverride, useAgent,
} from './api';

function respond(status: number, body?: unknown) {
  return vi.fn(async () => new Response(body === undefined ? null : JSON.stringify(body), { status }));
}

afterEach(() => vi.unstubAllGlobals());

describe('api', () => {
  it('sends JSON and returns the parsed body', async () => {
    const f = respond(200, [{ driver: 'a', duration: 60 }]);
    vi.stubGlobal('fetch', f);
    expect(await putPlan([{ driver: 'a', duration: 60 }])).toEqual([{ driver: 'a', duration: 60 }]);
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect(url).toBe('/api/stint-plan');
    expect(init.method).toBe('PUT');
    expect(init.body).toBe('[{"driver":"a","duration":60}]');
  });
  it('raises the error message of the backend', async () => {
    vi.stubGlobal('fetch', respond(400, { error: 'stint 1: driver required' }));
    await expect(putPlan([])).rejects.toMatchObject({ message: 'stint 1: driver required', status: 400 });
  });
  it('treats a missing parameter set as null and escapes the query', async () => {
    const f = respond(404, { error: 'none' });
    vi.stubGlobal('fetch', f);
    expect(await getParams('Spa & Co', 'GT3 #7')).toBeNull();
    expect((f.mock.calls[0] as unknown as [string])[0]).toBe('/api/params?track=Spa%20%26%20Co&car=GT3%20%237');
  });
  it('other errors are not swallowed', async () => {
    vi.stubGlobal('fetch', respond(500, { error: 'db down' }));
    await expect(getParams('t', 'c')).rejects.toBeInstanceOf(ApiError);
  });
  it('clears an override with a 204', async () => {
    const f = respond(204);
    vi.stubGlobal('fetch', f);
    await expect(clearOverride('fuel')).resolves.toBeUndefined();
    vi.stubGlobal('fetch', respond(200, { key: 'fuel', value: 40 }));
    expect(await setOverride('fuel', 40)).toEqual({ key: 'fuel', value: 40 });
  });
  it('reads who the user is', async () => {
    vi.stubGlobal('fetch', respond(200, { email: 'a@example.com', team_id: 1, team: 'default', is_admin: true }));
    expect(await getMe()).toMatchObject({ is_admin: true });
  });
  it('issues the configuration of the own agent as text and raises the message of a refusal', async () => {
    const f = vi.fn(async () => new Response('[server]\ntoken = "t"\n', { status: 200 }));
    vi.stubGlobal('fetch', f);
    expect(await issueAgentConfig()).toContain('token = "t"');
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect([url, init.method]).toEqual(['/api/me/agent/config', 'POST']);
    vi.stubGlobal('fetch', respond(409, { error: 'the configuration cannot be created yet: no_short' }));
    await expect(issueAgentConfig()).rejects.toMatchObject({ status: 409, message: 'the configuration cannot be created yet: no_short' });
  });
  it('sends name, short name and Cloudflare data of a member', async () => {
    const f = respond(200, {});
    vi.stubGlobal('fetch', f);
    await adminSetMember({ email: 'a@example.com', team_id: 1, is_admin: false, name: 'A', short: 'ab', cf_client_id: 'id', cf_secret: 's' });
    const [, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect(JSON.parse(init.body as string)).toEqual({ email: 'a@example.com', team_id: 1, is_admin: false, name: 'A', short: 'ab', cf_client_id: 'id', cf_secret: 's' });
  });
  it('addresses team cars by team and escapes vehicle names', async () => {
    const f = respond(200, { cars: [], unmapped: [] });
    vi.stubGlobal('fetch', f);
    await adminCars(2);
    await adminMapCarName(2, 5, 'BMW #1');
    await adminUnmapCarName(2, 'BMW #1 & red');
    const calls = f.mock.calls.map((c) => c as unknown as [string, RequestInit]);
    expect(calls.map(([u, i]) => [i.method, u])).toEqual([
      ['GET', '/api/admin/cars?team_id=2'],
      ['PUT', '/api/admin/cars/5/names'],
      ['DELETE', '/api/admin/car-names?team_id=2&name=BMW%20%231%20%26%20red'],
    ]);
    expect(calls[1][1].body).toBe('{"team_id":2,"vehicle_name":"BMW #1"}');
  });
  it('ties an agent to a member and releases it with an empty address', async () => {
    const f = respond(200, { id: 3, name: 'pc', team_id: 1, email: '', revoked: false, last_seen: null });
    vi.stubGlobal('fetch', f);
    await adminSetAgentEmail(3, '');
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect([url, init.method, init.body]).toEqual(['/api/admin/agents/3', 'PUT', '{"email":""}']);
  });
  it('adds the selected agent to overrides and pit commands, and nothing else', async () => {
    const f = respond(200, {});
    vi.stubGlobal('fetch', f);
    useAgent(7);
    try {
      await setOverride('fuel', 10);
      await clearOverride('fuel');
      await pitRequest('box');
      await putPlan([]);
      const urls = f.mock.calls.map((c) => (c as unknown as [string])[0]);
      expect(urls).toEqual(['/api/overrides/fuel?agent=7', '/api/overrides/fuel?agent=7', '/api/pit-request?name=box&agent=7', '/api/stint-plan']);
    } finally {
      useAgent(null);
    }
    await setOverride('fuel', 10);
    expect((f.mock.calls.at(-1) as unknown as [string])[0]).toBe('/api/overrides/fuel');
  });
  it('escapes the address when a member is removed and calls the revoke route', async () => {
    const f = respond(204);
    vi.stubGlobal('fetch', f);
    await adminDeleteMember('a+b@example.com');
    await adminRevokeAgent(7);
    await adminSetMember({ email: 'a@example.com', team_id: 2, is_admin: false });
    const urls = f.mock.calls.map((c) => (c as unknown as [string, RequestInit]).slice(0, 2).map((x) => (typeof x === 'string' ? x : x.method)));
    expect(urls).toEqual([
      ['/api/admin/members?email=a%2Bb%40example.com', 'DELETE'],
      ['/api/admin/agents/7/revoke', 'POST'],
      ['/api/admin/members', 'PUT'],
    ]);
  });
});
