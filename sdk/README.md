# Trusted Node authoring SDK

Dependency-free ESM and TypeScript declarations. Bundle/copy `index.mjs` and any relative runtime modules into the prebuilt package, or use the runtime-supplied `api` directly as the example does. The installer does not fetch SDK dependencies or compile TypeScript. `defineExtension` remains a generic worker API; `defineHTTP` gives typed installed-HTTP inputs, results and contexts over the same registration API.

```ts
import { defineHTTP, replace, pass } from './sdk/index.mjs';
export default defineHTTP(api => {
  api.on({ id: 'decorate', stage: 'responseBody', bodyMode: 'buffered',
           order: 10, failure: 'block', formats: ['text/plain'] }, input => {
    return replace({ ...input.response, body: btoa('Example response') });
  });
  api.on({ id: 'delivered', stage: 'delivery', bodyMode: 'none' }, input => {
    api.log('proxy writer accepted bytes', input.delivery?.bytes);
    return pass();
  });
});
```

## Installed HTTP v1

- `request`, `resolve`, `upstream`: pass/replace the full request, or respond with a local full response. Credentials (header arrays) are a separate result field accepted only in resolve/upstream. `responseStart`: pass/replace response status/headers, or `child(fullBufferedRequest)` through the native chain. `responseBody`: pass/replace a declared whole body or opaque byte unit. `observe`, `originalBody`, `finish`, `delivery` are observation-only: return pass with no metadata/credentials. `observe` sees immutable original attempt response facts, not the effective response. `originalBody` sees bounded original source bytes. `finish` is reader completion/error, **not** peer delivery. `delivery` is proxy writer acceptance/error evidence, not peer receipt.
- Descriptors require id/stage/bodyMode. `none` omits bodies, `buffered` declares bounded Whole, `stream` declares bounded opaque Unit. Unit is valid only for originalBody/responseBody; originalBody requires Unit. Observe, responseStart, finish and delivery require none. Whole+Unit response transforms cannot mix. Canonical MIME formats have no parameters. No SSE or semantic-event inference.
- `failure` defaults to `block`; only explicit `failOpen` continues on worker/result failure using the native last accepted candidate. Returning `pass()` preserves it deliberately. Blocking stream failure after commitment aborts rather than replacing already-written bytes. A Whole retention-limit failure occurs before invoking a hook and does not silently downgrade to streaming.
- Registration IDs are `installation-id/local-id`, at most 128 bytes including owner. Dependencies without `/` are local; cross-package references use `other-installation/local-id`. Requires must exist and implies ordering; absent before/after targets are ignored. Numeric order is honored as a hard same-stage priority; conflicting dependency edges fail startup. Equal order honors explicit dependencies, then stable global ID. No hot swap; restart after installation/removal.
- Input `v:1` includes lower-camel-case identity, requested/request/response snapshots, owner metadata, copied ledger/truncated/provenance, trusted auth, error string, optional stream/delivery. Header values are string arrays. Requested has no retained body. No functions, Go errors or upstream credential map are serialized. Only the owner's metadata namespace can be replaced. Original/effective/delivery are distinct evidence.
- Messages are full snapshots `{method,url,header,status,body?}`, never patches; spread the supplied request/response when replacing. Request status is zero and URL absolute HTTP(S). Response method/URL may be empty. Bodies are canonical base64; no-body replacements must omit body. Respond/child always require body (empty string for empty bytes). Results require explicit `action`; unknown fields, duplicate keys, null fields, illegal actions, missing snapshot fields and noncanonical base64 fail. `replace`, `respond`, `child`, `pass` merely construct explicit result objects; native validation remains authoritative.
- Stream input uses `input.stream={streamId,sequence,data,end:false}` and response status/headers without a duplicated response.body. IDs distinguish hook/exchange/attempt/stage; sequences start at zero per stream and increase. Native callbacks supply data only, so there is **no fabricated final unit**. Use finish for terminal evidence. `streamBytes` / `streamUnit` preserve opaque bytes with explicit decoded-size bounds (helper default 64 KiB, bridge Unit default 32 KiB). Stream replacements carry base64 in `message.body`.

## Managed ordinary HTTP

Non-delivery HTTP handlers receive `context.http(fullBufferedRequest)`. It uses bounded correlated IPC to Go's `engine.Client` with the active invocation context and exact configured gateway origin, not direct `Input.Call` or a Node-visible capability. Example inside a handler:

```js
const origin = api.config.gatewayOrigin; // supplied by CLI; configure it when embedding
const inner = await context.http({
  method: 'GET', url: origin + '/internal', status: 0, header: {}, body: '',
});
```

The CLI supplies `config.gatewayOrigin` (actual bound listener by default; override with `-gateway-origin`). Embedded hosts choose their own worker configuration. Request URLs must match that exact origin. Reserved X-Proxykit headers are rejected. Requests do not inherit parent upstream credentials. Trusted ingress must call engine.Bind **before** authentication middleware; only a live Go-owned scope reuses trusted authentication, never public identity headers. The CLI has no built-in authentication middleware and does not claim to provide it.

Full ordinary request and response hooks execute for the internal request; root budgets and native/worker immediate-admission limits remain shared. One native slot rejects recursion without waiting; a required inner failure appears as an ordinary HTTP 502 response, not permission to skip authentication or hooks. Only one outstanding helper call per invocation; engine capability registry also limits a live exchange to 16 clients. Buffered request/response limits apply. Redirect responses, alternative origins, streaming/WebSocket/options and forged/expired scopes are rejected. Tokens remain Go-only. Helpers expire on settlement/cancellation; do not retain them or fire-and-forget calls. Delivery has no helper. Arbitrary trusted Node sockets remain unmanaged.

## Bounds and lifecycle

Bridge defaults: body/managed payload 256 KiB, Unit 32 KiB, IPC complete frame 1 MiB, worker concurrency 16, worker call timeout 30 s, native invocation admission 64. Complete JSON+base64 envelope must also fit the frame; body limits do **not** reserve enough space for arbitrary metadata/headers. Overflow is explicit, never truncated or silently passed. Embedded hosts may configure finite body (at most 16 MiB), unit and worker limits; CLI uses these defaults. At most 64 installed workers, 512 hooks and 1024 live stream bookkeeping entries; ledger/metadata/native attempt/depth bounds also apply. Finish retires stream state before user terminal observers; if native terminal admission itself fails, retained bounded entries can require restart rather than unbounded growth.

Workers are trusted arbitrary code, not sandboxed. Stdout is protocol-only; use api.log/console (stderr). Handlers receive AbortSignal; cancellation is cooperative and retains worker capacity until actual handler settlement or process kill. No automatic replay. Setup registers cleanup, awaited in reverse order after active calls settle. HTTP admission stops before engine drain/cancel and worker Stop/reap. Startup failures roll back all launched workers. Configure responsive stderr writers; forwarding is bounded by process shutdown deadlines, not a durable log service.

Run `node --test sdk/*.test.mjs`; set `TSC_BINARY` to an existing TypeScript compiler if it is not on PATH. The authoring fixture checks generic StreamUnit JSON compatibility and every typed HTTP stage, including absence of a delivery helper. See `examples/node-personal-usage/README.md` for install/restart and local compiled-process verification.
