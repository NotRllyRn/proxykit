import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

test('TypeScript stream helpers are JSON hook return values', t => {
  // Never bootstrap/download a compiler. CI may supply an existing compiler.
  const compiler = process.env.TSC_BINARY || 'tsc';
  const result = spawnSync(compiler, [
    '--noEmit', '--strict', '--module', 'NodeNext', '--moduleResolution', 'NodeNext',
    '--target', 'ES2022', fileURLToPath(new URL('./authoring.fixture.mts', import.meta.url)),
  ], { encoding: 'utf8' });
  if (result.error?.code === 'ENOENT' && !process.env.TSC_BINARY) {
    t.skip('tsc unavailable; set TSC_BINARY to an existing compiler');
    return;
  }
  assert.ifError(result.error);
  assert.equal(result.status, 0, `${result.stdout}\n${result.stderr}`);
});
