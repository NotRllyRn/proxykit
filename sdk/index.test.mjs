import test from 'node:test';
import assert from 'node:assert/strict';
import { defineExtension, pass, streamUnit, streamBytes } from './index.mjs';
test('setup and explicit pass', () => {
  const setup = api => api;
  assert.equal(defineExtension(setup), setup);
  assert.throws(() => defineExtension(null));
  assert.deepEqual(pass(), { action: 'pass' });
});
test('bounded stream units preserve opaque bytes', () => {
  const bytes = new Uint8Array([0, 255, 128, 10]);
  const unit = streamUnit('example-stream', 0, bytes, true, 4);
  assert.deepEqual([...streamBytes(unit, 4)], [...bytes]);
  assert.equal(unit.end, true);
  assert.throws(() => streamUnit('s', 1, bytes, false, 3));
  assert.throws(() => streamBytes(unit, 3));
  assert.throws(() => streamBytes({ ...unit, data: '!!!!' }));
  assert.throws(() => streamBytes({ ...unit, data: 'AB==' }));
  assert.throws(() => streamBytes({ ...unit, sequence: -1 }));
  assert.throws(() => streamUnit('', 0, bytes));
  assert.deepEqual([...streamBytes(streamUnit('s', 1, new Uint8Array(), true))], []);
});
