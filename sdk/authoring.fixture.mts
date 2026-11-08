// Compile-only fixture: tsc --noEmit --strict --module NodeNext --moduleResolution NodeNext --target ES2022 sdk/authoring.fixture.mts
import { defineExtension, defineHTTP, replace, respond, child, pass, streamUnit, streamBytes, type JSONValue, type StreamUnit } from './index.mjs';
const unit: StreamUnit = streamUnit('example-stream', 0, new Uint8Array([1, 2]), true);
const json: JSONValue = unit;
void json;
const httpSetup = defineHTTP(api => {
  api.on({ id: 'request', stage: 'request', bodyMode: 'buffered', failure: 'block', order: -2 }, i => {
    const hasTrailers: boolean = i.requestHasTrailers; void hasTrailers;
    // @ts-expect-error Original trailer presence is a read-only host fact.
    i.requestHasTrailers = false;
    return replace(i.request);
  });
  api.on({ id: 'resolve', stage: 'resolve', bodyMode: 'none', after: ['request'] }, () => ({ ...pass(), credentials: { Authorization: ['Bearer example'] } }));
  api.on({ id: 'upstream', stage: 'upstream', bodyMode: 'none' }, () => respond({ method: '', url: '', status: 200, header: {}, body: '' }));
  api.on({ id: 'observe', stage: 'observe', bodyMode: 'none' }, i => { const root: string = i.identity.root; void root; return pass(); });
  api.on({ id: 'child', stage: 'responseStart', bodyMode: 'none' }, () => child({ method: 'GET', url: 'http://localhost/example', status: 0, header: {}, body: '' }));
  api.on({ id: 'original', stage: 'originalBody', bodyMode: 'stream' }, i => { if (i.stream) streamBytes(i.stream); return pass(); });
  api.on({ id: 'body', stage: 'responseBody', bodyMode: 'buffered' }, async (i, ctx) => { const response = await ctx.http({ method: 'GET', url: 'http://localhost/example', status: 0, header: {}, body: '' }); void response; return replace(i.response); });
  api.on({ id: 'finish', stage: 'finish', bodyMode: 'none' }, () => pass());
  api.on({ id: 'delivery', stage: 'delivery', bodyMode: 'none' }, (i, ctx) => {
    // @ts-expect-error Delivery has no managed HTTP capability.
    void ctx.http;
    const bytes: number | undefined = i.delivery?.bytes; void bytes; return pass();
  });
});
void httpSetup;
streamBytes(unit);
export default defineExtension(api => {
  api.on({ id: 'stream', stage: 'observe', bodyMode: 'stream' }, () => streamUnit('example-stream', 0, new Uint8Array(), true));
  api.on({ id: 'asyncStream', stage: 'observe', bodyMode: 'stream' }, async () => unit);
});
