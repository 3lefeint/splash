/** The part of a speech synthesis voice that the choice needs. */
export interface VoiceInfo {
  name: string;
  /** Identifies the voice; names are not unique, browsers list several voices under one name. */
  voiceURI: string;
  lang: string;
  default?: boolean;
}

/**
 * Picks the voice to speak with: the one the user chose (by its voiceURI), else a voice of the interface language (an exact
 * match like de-DE first), else the default voice, else any. Null when the browser has no voice at all.
 */
export function pickVoice<T extends VoiceInfo>(voices: T[], lang: string, preferred = ''): T | null {
  if (voices.length === 0) return null;
  const chosen = preferred ? voices.find((v) => v.voiceURI === preferred) : undefined;
  if (chosen) return chosen;
  const base = lang.toLowerCase().slice(0, 2);
  const mine = voices.filter((v) => v.lang.toLowerCase().replace('_', '-').startsWith(base));
  return mine.find((v) => v.lang.toLowerCase().replace('_', '-') === lang.toLowerCase()) ?? mine[0] ?? voices.find((v) => v.default) ?? voices[0];
}

/** True when the voice speaks the language of the interface. */
export const speaksLanguage = (v: VoiceInfo, lang: string): boolean =>
  v.lang.toLowerCase().replace('_', '-').startsWith(lang.toLowerCase().slice(0, 2));
