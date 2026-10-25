import type { OverrideKey, Params, PlanRow } from './types';

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!res.ok) {
    let message = res.statusText;
    try {
      message = ((await res.json()) as { error?: string }).error ?? message;
    } catch {
      // not JSON
    }
    throw new ApiError(message || `HTTP ${res.status}`, res.status);
  }
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
}

export const getPlan = () => request<PlanRow[]>('GET', '/api/stint-plan');
export const putPlan = (rows: PlanRow[]) => request<PlanRow[]>('PUT', '/api/stint-plan', rows);

/** Parameters of a track and car, or null when none are stored yet. */
export async function getParams(track: string, car: string): Promise<Params | null> {
  try {
    return await request<Params>('GET', `/api/params?track=${encodeURIComponent(track)}&car=${encodeURIComponent(car)}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

export const putParams = (p: Params) =>
  request<Params>('PUT', `/api/params?track=${encodeURIComponent(p.track)}&car=${encodeURIComponent(p.car)}`, p);

export const setOverride = (key: OverrideKey, value: number) =>
  request<{ key: string; value: number }>('PUT', `/api/overrides/${key}`, { value });
export const clearOverride = (key: OverrideKey) => request<void>('DELETE', `/api/overrides/${key}`);
