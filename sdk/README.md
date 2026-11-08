# Trusted Node authoring SDK

`index.mjs` is dependency-free ESM; `index.d.mts` supplies TypeScript declarations for the `.mjs` import. This directory is not a published registry package. Bundle these files inside an extension if using the helpers: installation does not fetch dependencies, compile TypeScript, or run scripts. The runtime requires a prebuilt `.mjs` entry default-exporting an optionally async setup function.

```js
// In a package that bundles sdk/index.mjs alongside its entry:
import { defineExtension, pass } from './sdk/index.mjs';
export default defineExtension(api => {
  api.on({ id: 'example', stage: 'observe', bodyMode: 'none' }, (input, ctx) => {
    if (ctx.signal.aborted) throw new Error('cancelled');
    api.log('Example user observation');
    return pass();
  });
  api.cleanup(() => api.log('stopped'));
});
```

`on` and `cleanup` registrations close when setup finishes. Configuration is a JSON value. Handlers return JSON, including `null` if intended; `undefined`, cycles and BigInt are errors. An explicit `{action:'pass'}` is an authoring convention for the future bridge, not a second execution engine in the worker. Stage labels are also bridge-consumed; this SDK does not claim an attached HTTP pipeline.

`streamUnit`/`streamBytes` encode and validate explicit, canonical base64 units. Default decoded maximum is 64 KiB. The envelope must also fit the process frame bound; choose a smaller unit when negotiating small frames. The caller must serialize units per stream, retain/recover HTTP bodies when necessary and own commit behavior. The worker never discovers or copies an HTTP body on its own.

All standard global `console` methods and `api.log` write stderr. Direct stdout writes, including a separately constructed stdout `Console`, violate the protocol. Trusted code has full server-identity filesystem/network access and can bypass these conventions; none of this is a sandbox.

Run helper tests with `node --test sdk/*.test.mjs`. Actual Node process lifecycle and protocol tests live in `worker/` and run with `go test ./worker`. The implementation was tested with Node v24.16.0. TypeScript declarations have not been compiler-checked in the current environment (no `tsc` installed).
