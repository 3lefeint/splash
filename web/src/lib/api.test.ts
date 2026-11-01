import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  adminCreateAgent, adminDeleteMember, adminRevokeAgent, adminSetAgentEmail, adminSetMember, ApiError, clearOverride,
  getMe, getParams, pitRequest, putPlan, setOverride, useAgent,
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
  it('creates an agent and returns the token of the reply', async () => {
    const f = respond(201, { id: 3, name: 'pc', team_id: 1, email: 'a@example.com', revoked: false, last_seen: null, token: 'tok' });
    vi.stubGlobal('fetch', f);
    expect((await adminCreateAgent('pc', 1, 'a@example.com')).token).toBe('tok');
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit];
    expect([url, init.method, init.body]).toEqual(['/api/admin/agents', 'POST', '{"name":"pc","team_id":1,"email":"a@example.com"}']);
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
