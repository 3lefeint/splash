import { describe, expect, it } from 'vitest';
import { pickVoice, speaksLanguage } from './voice';

const v = (name: string, lang: string, extra = {}) => ({ name, voiceURI: `uri:${name}:${lang}`, lang, ...extra });
const voices = [v('English', 'en-US', { default: true }), v('Hans', 'de-AT'), v('Katja', 'de-DE'), v('Marlene', 'de_DE')];

describe('pickVoice', () => {
  it('has no voice without voices', () => {
    expect(pickVoice([], 'de-DE')).toBeNull();
  });

  it('takes the voice the user chose', () => {
    expect(pickVoice(voices, 'de-DE', 'uri:Hans:de-AT')?.name).toBe('Hans');
  });

  it('falls back from a chosen voice that is gone', () => {
    expect(pickVoice(voices, 'de-DE', 'Gone')?.name).toBe('Katja');
  });

  it('prefers an exact language match, then any voice of the language', () => {
    expect(pickVoice(voices, 'de-DE')?.name).toBe('Katja');
    expect(pickVoice(voices.slice(0, 2), 'de-DE')?.name).toBe('Hans');
  });

  it('tells apart voices that share a name', () => {
    const twins = [v('Google', 'en-US', { default: true }), v('Google', 'de-DE')];
    expect(pickVoice(twins, 'de-DE')?.lang).toBe('de-DE');
    expect(pickVoice(twins, 'de-DE', 'uri:Google:en-US')?.lang).toBe('en-US');
  });

  it('uses the default voice when none speaks the language', () => {
    expect(pickVoice([v('A', 'fr-FR'), v('B', 'en-US', { default: true })], 'de-DE')?.name).toBe('B');
    expect(pickVoice([v('A', 'fr-FR')], 'de-DE')?.name).toBe('A');
  });

  it('tells whether a voice speaks the interface language', () => {
    expect(speaksLanguage(voices[0], 'de-DE')).toBe(false);
    expect(speaksLanguage(voices[3], 'de-DE')).toBe(true);
  });
});
