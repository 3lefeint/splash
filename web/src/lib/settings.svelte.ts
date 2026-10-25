import { translate, type Key, type Lang } from './i18n';
import { readJson, safeStorage, writeJson } from './storage';

interface Saved {
  lang: Lang;
  wearInverted: boolean;
  restDriver: string;
}

const KEY = 'splash.settings';

class Settings {
  lang = $state<Lang>('de');
  /** True when the tyre value 1.0 means "new"; the game header does not say which way mWear runs. */
  wearInverted = $state(false);
  restDriver = $state('');

  constructor() {
    const saved = readJson<Partial<Saved>>(safeStorage(), KEY);
    if (saved?.lang === 'de' || saved?.lang === 'en') this.lang = saved.lang;
    this.wearInverted = saved?.wearInverted === true;
    this.restDriver = typeof saved?.restDriver === 'string' ? saved.restDriver : '';
  }

  save(): void {
    writeJson(safeStorage(), KEY, { lang: this.lang, wearInverted: this.wearInverted, restDriver: this.restDriver });
  }
}

export const settings = new Settings();

/** Translates with the language chosen in the settings; reactive in templates. */
export function t(key: Key, params?: Record<string, string | number>): string {
  return translate(settings.lang, key, params);
}
