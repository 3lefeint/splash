import { getMe } from './api';
import type { Me } from './types';

/** Who the backend says the user is. Without authentication on the backend nobody is an administrator. */
class Identity {
  me = $state<Me | null>(null);

  get isAdmin(): boolean {
    return this.me?.is_admin ?? false;
  }

  async load(): Promise<void> {
    try {
      this.me = await getMe();
    } catch {
      this.me = null; // the rest of the app shows its own errors
    }
  }
}

export const identity = new Identity();
