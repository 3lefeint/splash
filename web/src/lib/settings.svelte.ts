import { translate, type Key, type Lang } from './i18n';
import { readJson, safeStorage, writeJson } from './storage';
import { DEFAULT_RANGES, FALLBACK_RANGE, type TyreRange } from './conditions';
import type { Target } from './overlays';
import type { Overlay, Wheel } from './wear';

interface Saved {
  lang: Lang;
  wearInverted: boolean;
  restDriver: string;
  mapOpponents: boolean;
  mapHiddenClasses: string[];
  mapHiddenCars: string[];
  mapOverlay: Overlay;
  mapWheel: Wheel;
  tyreRanges: TyreRange[];
  tyreFallback: { low: number; high: number };
  soundMode: SoundMode;
  voiceName: string;
  mapTarget: Target;
  mapPace: boolean;
  mapPits: boolean;
  mapGhost: boolean;
}

export type SoundMode = 'off' | 'beep' | 'voice';

const KEY = 'splash.settings';

const num = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);

const ranges = (v: unknown): TyreRange[] | null =>
  Array.isArray(v)
    ? v
        .filter((r): r is TyreRange => !!r && typeof r.name === 'string' && num(r.low) && num(r.high))
        .map((r) => ({ name: r.name, low: r.low, high: r.high }))
    : null;

const strings = (v: unknown): string[] => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []);

class Settings {
  lang = $state<Lang>('de');
  /** True when the tyre value 1.0 means "new"; the game header does not say which way mWear runs. */
  wearInverted = $state(false);
  restDriver = $state('');
  /** Track map: show other cars, and which classes and cars (see hideKey) are hidden. */
  mapOpponents = $state(true);
  mapHiddenClasses = $state<string[]>([]);
  mapHiddenCars = $state<string[]>([]);
  /** What is painted on the track: nothing, tyre wear per section, or its change since the set was new. */
  mapOverlay = $state<Overlay>('off');
  mapWheel = $state<Wheel>('all');
  /** Working temperature ranges of the tyres by compound name, and the range for any other compound. */
  tyreRanges = $state<TyreRange[]>(DEFAULT_RANGES.map((r) => ({ ...r })));
  tyreFallback = $state({ ...FALLBACK_RANGE });
  /** How announcements sound: not at all, a beep pattern by urgency, or a beep and the text spoken. */
  soundMode = $state<SoundMode>('off');
  /** The speech voice chosen by name; empty picks one that speaks the interface language. */
  voiceName = $state('');
  /** Time view of the map: which car or lap we compare with. */
  mapTarget = $state<Target>('ahead');
  /** Colour the cars by their pace against ours; show the pit lane; show where we would rejoin after a stop. */
  mapPace = $state(true);
  mapPits = $state(true);
  mapGhost = $state(true);

  constructor() {
    const saved = readJson<Partial<Saved>>(safeStorage(), KEY);
    if (saved?.lang === 'de' || saved?.lang === 'en') this.lang = saved.lang;
    this.wearInverted = saved?.wearInverted === true;
    this.restDriver = typeof saved?.restDriver === 'string' ? saved.restDriver : '';
    this.mapOpponents = saved?.mapOpponents !== false;
    this.mapHiddenClasses = strings(saved?.mapHiddenClasses);
    this.mapHiddenCars = strings(saved?.mapHiddenCars);
    if (['wear', 'delta', 'time', 'fuel', 'speed', 'brake'].includes(saved?.mapOverlay as string)) this.mapOverlay = saved!.mapOverlay as Overlay;
    if (['ahead', 'behind', 'best'].includes(saved?.mapTarget as string)) this.mapTarget = saved!.mapTarget as Target;
    this.voiceName = typeof saved?.voiceName === 'string' ? saved.voiceName : '';
    this.mapPace = saved?.mapPace !== false;
    this.mapPits = saved?.mapPits !== false;
    this.mapGhost = saved?.mapGhost !== false;
    if (saved?.soundMode === 'beep' || saved?.soundMode === 'voice') this.soundMode = saved.soundMode;
    this.tyreRanges = ranges(saved?.tyreRanges) ?? this.tyreRanges;
    const fb = saved?.tyreFallback;
    if (fb && num(fb.low) && num(fb.high)) this.tyreFallback = { low: fb.low, high: fb.high };
    if (['all', '0', '1', '2', '3'].includes(saved?.mapWheel as string)) this.mapWheel = saved!.mapWheel as Wheel;
  }

  save(): void {
    writeJson(safeStorage(), KEY, {
      lang: this.lang,
      wearInverted: this.wearInverted,
      restDriver: this.restDriver,
      mapOpponents: this.mapOpponents,
      mapHiddenClasses: [...this.mapHiddenClasses],
      mapHiddenCars: [...this.mapHiddenCars],
      mapOverlay: this.mapOverlay,
      mapWheel: this.mapWheel,
      tyreRanges: this.tyreRanges.map((r) => ({ ...r })),
      tyreFallback: { ...this.tyreFallback },
      soundMode: this.soundMode,
      voiceName: this.voiceName,
      mapTarget: this.mapTarget,
      mapPace: this.mapPace,
      mapPits: this.mapPits,
      mapGhost: this.mapGhost,
    });
  }

  resetTyreRanges(): void {
    this.tyreRanges = DEFAULT_RANGES.map((r) => ({ ...r }));
    this.tyreFallback = { ...FALLBACK_RANGE };
    this.save();
  }

  /** Adds or removes a value from a hidden list and saves. */
  toggleHidden(list: 'mapHiddenClasses' | 'mapHiddenCars', value: string): void {
    const cur = this[list];
    this[list] = cur.includes(value) ? cur.filter((v) => v !== value) : [...cur, value];
    this.save();
  }
}

export const settings = new Settings();

/** Translates with the language chosen in the settings; reactive in templates. */
export function t(key: Key, params?: Record<string, string | number>): string {
  return translate(settings.lang, key, params);
}
