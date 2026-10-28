import type { Corner, MapProgress, TrackInfo, SessionSummary, OverrideKey, Params, PitSetup, PlanRow, Step, TrackMap } from './types';

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

/** Outline of a track, or null while none has been recorded. */
export async function getTrackMap(track: string): Promise<TrackMap | null> {
  try {
    return await request<TrackMap>('GET', `/api/track-map?track=${encodeURIComponent(track)}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

export const deleteTrackMap = (track: string) =>
  request<void>('DELETE', `/api/track-map?track=${encodeURIComponent(track)}`);

export const listTracks = () => request<TrackInfo[]>('GET', '/api/tracks');
export const getCorners = (track: string) => request<Corner[]>('GET', `/api/track-corners?track=${encodeURIComponent(track)}`);
export const putCorners = (track: string, corners: Corner[]) =>
  request<Corner[]>('PUT', `/api/track-corners?track=${encodeURIComponent(track)}`, corners);

export const getPitSetups = (car: string) => request<PitSetup[]>('GET', `/api/pit-setups?car=${encodeURIComponent(car)}`);
export const putPitSetup = (s: PitSetup) => request<PitSetup>('PUT', '/api/pit-setups', s);
export const deletePitSetup = (car: string, name: string) =>
  request<void>('DELETE', `/api/pit-setups?car=${encodeURIComponent(car)}&name=${encodeURIComponent(name)}`);
export const applyPitSetup = (name: string, steps: Step[]) =>
  request<{ id: string }>('POST', '/api/pit-setups/apply', { name, steps });

/** The outline recorded so far, or null while none is being recorded. */
export async function getMapProgress(track: string): Promise<MapProgress | null> {
  try {
    return await request<MapProgress>('GET', `/api/track-map/progress?track=${encodeURIComponent(track)}`);
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) return null;
    throw e;
  }
}

export const getSessions = () => request<SessionSummary[]>('GET', '/api/sessions');

/** Address of a CSV export of a session: laps of all cars, pit stops or the conditions of the driven car per lap. */
export const exportUrl = (sessionId: string, kind: 'laps' | 'stops' | 'conditions') =>
  `/api/sessions/${encodeURIComponent(sessionId)}/export?kind=${kind}`;

/** Toggles the pit request of the driven car through the driver agent. */
export const pitRequest = (name: string) =>
  request<{ id: string }>('POST', `/api/pit-request?name=${encodeURIComponent(name)}`);
