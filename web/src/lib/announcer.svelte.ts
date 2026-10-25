import type { Key } from './i18n';
import type { Announcement } from './announce';
import { settings, t } from './settings.svelte';
import type { Level } from './strategy';
import { pickVoice, speaksLanguage } from './voice';

const BEEPS: Record<Level, { count: number; hz: number }> = {
  info: { count: 1, hz: 660 },
  warn: { count: 2, hz: 880 },
  bad: { count: 3, hz: 1100 },
};

/** Plays announcements as they become true and keeps the list of those that hold, for the banner. */
class Announcer {
  active = $state<Announcement[]>([]);
  /** The voices of the browser, which load a moment after the page. */
  voices = $state<SpeechSynthesisVoice[]>([]);
  /** What went wrong with the last spoken message, empty when it started. */
  speechError = $state('');
  private seen = new Set<string>();
  private primed = false;
  private audio: AudioContext | null = null;

  constructor() {
    try {
      const load = () => (this.voices = speechSynthesis.getVoices());
      load();
      speechSynthesis.addEventListener('voiceschanged', load);
    } catch {
      // no speech synthesis in this browser
    }
  }

  /** The voice that is spoken with, or null when the browser has none. */
  get voice(): SpeechSynthesisVoice | null {
    return pickVoice(this.voices, settings.lang === 'de' ? 'de-DE' : 'en-US', settings.voiceName);
  }

  /** 'none': no speech at all; 'foreign': a voice of another language; 'ok'. */
  get speech(): 'none' | 'foreign' | 'ok' {
    const v = this.voice;
    if (typeof speechSynthesis === 'undefined' || !v) return 'none';
    return speaksLanguage(v, settings.lang) ? 'ok' : 'foreign';
  }

  /** Must be called from a click: browsers only allow sound after a gesture. */
  unlock(): void {
    try {
      this.audio ??= new AudioContext();
      void this.audio.resume();
    } catch {
      // no audio available: the banner still works
    }
  }

  /**
   * Takes what holds now. What already held when the first state arrived (e.g. after a reload) is not
   * announced; later a new id is spoken once and again only after it has ended.
   */
  update(list: Announcement[], ready: boolean): void {
    this.active = list;
    if (!ready) return;
    const ids = new Set(list.map((a) => a.id));
    if (this.primed) {
      for (const a of list) if (!this.seen.has(a.id)) this.fire(a);
    }
    this.primed = true;
    this.seen = ids;
  }

  fire(a: Announcement): void {
    if (settings.soundMode === 'off') return;
    this.beep(a.level);
    if (settings.soundMode === 'voice') this.speak(t(a.key as Key, a.params));
  }

  /** Plays an example so the volume and the voice can be checked. */
  test(): void {
    this.unlock();
    this.fire({ id: 'test', level: 'warn', key: 'say.test' });
  }

  private beep(level: Level): void {
    const ctx = this.audio;
    if (!ctx) return;
    const { count, hz } = BEEPS[level];
    const t0 = ctx.currentTime + 0.02;
    for (let i = 0; i < count; i++) {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.frequency.value = hz;
      gain.gain.setValueAtTime(0.0001, t0 + i * 0.26);
      gain.gain.exponentialRampToValueAtTime(0.35, t0 + i * 0.26 + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, t0 + i * 0.26 + 0.18);
      osc.connect(gain).connect(ctx.destination);
      osc.start(t0 + i * 0.26);
      osc.stop(t0 + i * 0.26 + 0.2);
    }
  }

  private speak(text: string): void {
    this.speechError = '';
    try {
      const voice = this.voice;
      if (!voice) {
        this.speechError = 'none';
        return;
      }
      const u = new SpeechSynthesisUtterance(text);
      u.voice = voice;
      u.lang = voice.lang;
      u.onerror = (e) => {
        this.speechError = e.error || 'error';
      };
      speechSynthesis.cancel(); // a queue that got stuck would swallow every later message
      speechSynthesis.resume();
      speechSynthesis.speak(u);
    } catch {
      this.speechError = 'error';
    }
  }
}

export const announcer = new Announcer();
