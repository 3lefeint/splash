import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, clearOverride, getParams, putPlan, setOverride } from './api';

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
});
