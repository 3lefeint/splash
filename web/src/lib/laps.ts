/**
 * The flag to store when the "counts" box of a lap is clicked. A lap has an own flag only when it differs from its
 * session: it is left out inside a session that counts (true), or taken in inside a session that is left out (false);
 * otherwise it follows the session (null).
 */
export function nextFlag(currentlyExcluded: boolean, sessionExcluded: boolean): boolean | null {
  const wantsToCount = currentlyExcluded;
  if (wantsToCount) return sessionExcluded ? false : null;
  return sessionExcluded ? null : true;
}
