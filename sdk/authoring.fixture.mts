// Compile-only fixture: tsc --noEmit --strict --module NodeNext --moduleResolution NodeNext --target ES2022 sdk/authoring.fixture.mts
import { defineExtension, streamUnit, streamBytes, type JSONValue, type StreamUnit } from './index.mjs';
const unit: StreamUnit = streamUnit('example-stream', 0, new Uint8Array([1, 2]), true);
const json: JSONValue = unit;
void json;
streamBytes(unit);
export default defineExtension(api => {
  api.on({ id: 'stream', stage: 'observe', bodyMode: 'stream' }, () => streamUnit('example-stream', 0, new Uint8Array(), true));
  api.on({ id: 'asyncStream', stage: 'observe', bodyMode: 'stream' }, async () => unit);
});
