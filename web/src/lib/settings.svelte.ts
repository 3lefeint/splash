import { translate, type Key, type Lang } from './i18n';
import { readJson, removeKey, safeStorage, writeJson } from './storage';
import { DEFAULT_BRAKE, DEFAULT_RANGES, FALLBACK_RANGE, type TyreRange } from './conditions';
import type { Target } from './overlays';
import type { Overlay, Wheel } from './wear';
import { migrateYellow } from './yellow';

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
  brakeRange: { low: number; high: number };
  soundMode: SoundMode;
  voiceName: string;
  mapTarget: Target;
  mapPace: boolean;
  mapPits: boolean;
  mapGhost: boolean;
  yellowAhead: boolean;
  yellowMarkers: boolean;
  /** The names before the yellow settings changed their meaning; read once so a saved choice survives. */
  yellowAlert?: boolean;
  yellowMap?: boolean;
}

export type SoundMode = 'off' | 'beep' | 'voice';

const KEY = 'splash.settings';
/** The working ranges are stored apart from the other settings, and only once this browser changes them: until then the default of the team applies. */
const RANGES_KEY = 'splash.ranges';

/** The tyre and brake ranges as a whole, the part of the settings an administrator saves for the team. */
export interface RangesDoc {
  tyreRanges: TyreRange[];
  tyreFallback: { low: number; high: number };
  brakeRange: { low: number; high: number };
}

const num = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);

const pair = (v: unknown): { low: number; high: number } | null => {
  const p = v as { low?: unknown; high?: unknown } | null;
  return p && num(p.low) && num(p.high) ? { low: p.low, high: p.high } : null;
};

const defaultRanges = (): RangesDoc => ({
  tyreRanges: DEFAULT_RANGES.map((r) => ({ ...r })),
  tyreFallback: { ...FALLBACK_RANGE },
  brakeRange: { ...DEFAULT_BRAKE },
});

/** Keeps what is valid; null when nothing of the three parts is there. */
function cleanRanges(raw: unknown): RangesDoc | null {
  if (!raw || typeof raw !== 'object') return null;
  const r = raw as Record<string, unknown>;
  const tyres = ranges(r.tyreRanges);
  const fallback = pair(r.tyreFallback);
  const brake = pair(r.brakeRange);
  if (!tyres && !fallback && !brake) return null;
  const d = defaultRanges();
  return { tyreRanges: tyres ?? d.tyreRanges, tyreFallback: fallback ?? d.tyreFallback, brakeRange: brake ?? d.brakeRange };
}

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
  /** Working temperature of the brake discs, the same for every car. */
  brakeRange = $state({ ...DEFAULT_BRAKE });
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
  /** Yellow flags: the alert line with distance and corner of the probable cause ahead (and the full course yellow); the markers of the probable causes on the map and in the rivals list. Independent of each other, per browser like the other settings. */
  yellowAhead = $state(true);
  yellowMarkers = $state(true);

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
    Object.assign(this, migrateYellow(saved));
    if (saved?.soundMode === 'beep' || saved?.soundMode === 'voice') this.soundMode = saved.soundMode;
    if (['all', '0', '1', '2', '3'].includes(saved?.mapWheel as string)) this.mapWheel = saved!.mapWheel as Wheel;
    this.loadRanges(saved);
  }

  /** True once this browser stores ranges of its own. */
  private rangesOwn = false;
  /** The ranges the administrator saved for the team, when the server has them. */
  private teamRanges: RangesDoc | null = null;

  private loadRanges(saved: Partial<Saved> | null): void {
    let own = cleanRanges(readJson(safeStorage(), RANGES_KEY));
    if (!own) {
      // Before the ranges had a key of their own they were part of the settings; keep them when they differ from the defaults.
      const legacy = cleanRanges(saved);
      if (legacy && JSON.stringify(legacy) !== JSON.stringify(defaultRanges())) {
        own = legacy;
        writeJson(safeStorage(), RANGES_KEY, own);
      }
    }
    this.rangesOwn = own !== null;
    this.adoptRanges(own);
  }

  private adoptRanges(doc: RangesDoc | null): void {
    const d = structuredClone(doc ?? defaultRanges());
    this.tyreRanges = d.tyreRanges;
    this.tyreFallback = d.tyreFallback;
    this.brakeRange = d.brakeRange;
  }

  /** The ranges as they are now, for saving them as the default of the team. */
  rangesSnapshot(): RangesDoc {
    return $state.snapshot({ tyreRanges: this.tyreRanges, tyreFallback: this.tyreFallback, brakeRange: this.brakeRange });
  }

  /** Stores the ranges in this browser after an edit; from then on they are its own. */
  saveRanges(): void {
    this.rangesOwn = true;
    writeJson(safeStorage(), RANGES_KEY, this.rangesSnapshot());
  }

  /** Takes the ranges the server holds for the team; a browser with ranges of its own keeps them. */
  setTeamRanges(raw: unknown): void {
    this.teamRanges = cleanRanges(raw);
    if (!this.rangesOwn) this.adoptRanges(this.teamRanges);
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
      soundMode: this.soundMode,
      voiceName: this.voiceName,
      mapTarget: this.mapTarget,
      mapPace: this.mapPace,
      mapPits: this.mapPits,
      mapGhost: this.mapGhost,
      yellowAhead: this.yellowAhead,
      yellowMarkers: this.yellowMarkers,
    });
  }

  /** Back to the ranges of the team, or the built-in ones; the browser stores none of its own afterwards. */
  resetTyreRanges(): void {
    this.rangesOwn = false;
    removeKey(safeStorage(), RANGES_KEY);
    this.adoptRanges(this.teamRanges);
    this.save(); // rewrites the settings without the ranges an older version kept there
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
