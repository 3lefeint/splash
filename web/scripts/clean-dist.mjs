// Empties the embedded build directory but keeps .gitkeep, which makes the Go embed work on a fresh checkout.
import { readdirSync, rmSync, mkdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'backend', 'internal', 'webui', 'dist');
mkdirSync(dist, { recursive: true });
for (const name of readdirSync(dist)) {
  if (name !== '.gitkeep') rmSync(join(dist, name), { recursive: true, force: true });
}
