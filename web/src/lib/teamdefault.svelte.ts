import { adminSaveLayout, getTeamLayout } from './api';
import { dashboardLayout } from './layout.svelte';
import { settings } from './settings.svelte';

/** The dashboard default of the team: the tile layout and the tyre and brake ranges, saved by an administrator. */
class TeamDefault {
  /** Result of the last save, for the line next to the button. */
  status = $state<'' | 'done' | 'failed'>('');

  /** Reads the default from the server and applies it where this browser has nothing of its own. */
  async load(): Promise<void> {
    try {
      const doc = await getTeamLayout();
      dashboardLayout.setTeamDefault(doc.layout);
      settings.setTeamRanges(doc.ranges);
    } catch {
      // without a default the built-in one stays; the rest of the app shows its own errors
    }
  }

  /** Saves the layout and ranges as they are now as the default of the administrator's team. */
  async save(): Promise<void> {
    const doc = { layout: dashboardLayout.snapshot(), ranges: settings.rangesSnapshot() };
    try {
      await adminSaveLayout(doc);
      dashboardLayout.setTeamDefault(doc.layout);
      settings.setTeamRanges(doc.ranges);
      this.status = 'done';
    } catch {
      this.status = 'failed';
    }
  }
}

export const teamDefault = new TeamDefault();
