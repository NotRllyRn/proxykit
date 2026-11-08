# Writing a ProxyKit TypeScript extension

This is the developer entry point for **installed HTTP v1 extensions**. It documents the implemented Node boundary, not proposed native Go APIs. Start with the complete example below, then consult the [SDK reference](../sdk/README.md), [TypeScript API declarations](../sdk/http.d.mts), [helper declarations](../sdk/index.d.mts) and [installer/runtime contract](implementation/distribution.md). The [requirement status table](REQUIREMENT_STATUS.md) distinguishes implemented capabilities from future work.

## 1. Mental model and conventions

- ProxyKit runs your prebuilt ESM module in a trusted Node process. Export one setup function; register hooks during setup. Go owns HTTP dispatch, body retention, ordering and result validation.
- TypeScript is an authoring language, **not an installer input**. Compile before publishing. There is no npm package download, dependency installation, build script or automatic TypeScript execution on the server.
- Handlers decide whether to act. Return `pass()` when not applicable; there is no separate check callback.
- Return detached full snapshots, not mutations or patches. Keep handlers small, choose explicit body/failure modes and avoid global per-request state. Never assume requests execute serially.
- Use stable descriptive hook IDs, generic example labels and documented configuration. Keep secrets, databases and runtime output out of Git. Document permissions, ports, persistence, limits and unsupported modes in your extension's README.
- Extensions are **not sandboxed**. They have the server user's filesystem/network privileges. `--trust` means consent to code execution, not verification of safety.

## 2. GitHub repository layout

A minimal TypeScript repository can look like this:

```text
example-extension/
├── proxykit-extension.json     # required at repository root
├── README.md                   # setup, trust, configuration, support limits
├── LICENSE                     # choose appropriate licensing; preserve attribution
├── package.json                # development/build scripts only
├── tsconfig.json
├── src/
│   └── extension.mts           # .mts compiles to .mjs
├── dist/
│   └── extension.mjs           # MUST exist in the published Git revision
├── sdk/
│   ├── index.mjs               # copy from a compatible ProxyKit checkout
│   ├── index.d.mts
│   └── http.d.mts
└── test/
    └── extension.test.mjs
```

Do not ignore `dist/` in the revision users install. A release attachment alone is insufficient: the installer fetches the resolved repository commit archive, not release assets. Commit prebuilt output on your installable branch/tag. Keep source alongside it for review. Copy the SDK with its source attribution; there is no published SDK npm package assumed here.

Use Node builtins and relative modules included in that archive. Bundle third-party runtime dependencies ahead of publication if needed, preserving licenses. Do not rely on bare imports being resolved by an installer-run `npm install`. Include all required static UI assets as regular files. Symlinks, hardlinks, devices and traversal paths are rejected.

### Manifest

```json
{
  "manifestVersion": 1,
  "id": "example-extension",
  "version": "1.0.0",
  "entry": "dist/extension.mjs"
}
```

These are the **only** manifest keys. Unknown/duplicate keys fail validation.

| Field | Contract |
|---|---|
| `manifestVersion` | Integer `1` |
| `id` | `[a-z][a-z0-9-]{0,63}`; stable installation identity |
| `version` | SemVer-shaped string, at most 128 bytes |
| `entry` | Safe relative path to an existing regular `.mjs` file; no absolute path or `..` |

There are no manifest permissions, ports, dependency installation, lifecycle scripts or arbitrary configuration fields. An ID collision refuses installation rather than overwriting an existing package.

### Build configuration

Example `package.json` (development dependencies are installed by the author, not ProxyKit):

```json
{
  "name": "example-extension",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "tsc -p tsconfig.json",
    "test": "npm run build && node --test test/*.test.mjs"
  },
  "devDependencies": { "typescript": "^5.9.3" }
}
```

Example `tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "strict": true,
    "rootDir": "src",
    "outDir": "dist"
  },
  "include": ["src/**/*.mts"]
}
```

### Complete first extension

Save this as `src/extension.mts`. It adds a response header without buffering the body:

```ts
import { defineHTTP, pass, replace } from '../sdk/index.mjs';

export default defineHTTP(api => {
  api.on({
    id: 'response-label', stage: 'responseStart', bodyMode: 'none',
    failure: 'block',
  }, input => {
    if (input.response.status !== 200) return pass();
    return replace({
      ...input.response,
      header: { ...input.response.header, 'X-Example-Extension': ['enabled'] },
    });
  });
});
```

Build with `npm install` then `npm run build` **in your development checkout**. Commit the lockfile, manifest, `dist/extension.mjs`, copied SDK and documentation. Test with the Node version your server uses; ProxyKit's process fixtures use Node 24.

## 3. Setup and lifecycle API

| API | Behavior |
|---|---|
| `defineHTTP(setup)` | Typed identity helper for the default HTTP setup function; not a dispatcher |
| `api.on(descriptor, handler)` | Register while setup runs; registrations freeze after setup completes |
| `api.config` | Host-supplied JSON; CLI supplies `gatewayOrigin`, not an arbitrary per-package configuration UI |
| `api.log(...args)` | Log to stderr; redact secrets and request content |
| `api.cleanup(fn)` | Register sync/async cleanup, called in reverse order after active handlers settle |
| `context.signal` | AbortSignal; cooperate with cancellation and avoid effects after it is aborted |
| `context.deadline` | Optional absolute Unix timestamp in milliseconds |
| `context.http(request)` | Bounded, invocation-scoped internal HTTP through this gateway; unavailable in delivery handlers |

Async setup is awaited. If setup cannot acquire a resource, clean up anything it already acquired before throwing. Register cleanup immediately after successful acquisition. Close listeners, timers and owned connections during cleanup. Bound shutdown; a hanging handler/cleanup can cause worker termination. Cancellation does not roll back external side effects.

Stdout is reserved for framed IPC. `api.log` and standard console methods go to stderr; do not use `process.stdout.write` or create a stdout logger.

An extension can implement its own dashboard/enrollment HTTP servers using Node builtins. There is **no** `api.router`, automatic port allocation, public URL provisioning, TLS, dashboard framework or built-in OAuth. Provision ports/configuration explicitly; start listeners in setup and close them in cleanup. Use separate authentication for administrative and public-facing flows, constrain request sizes, and keep secrets out of URLs/logs. Default to loopback until deployment access controls are configured. A public enrollment flow is application code, not an installer feature.

## 4. Hook stages and valid results

Stages execute in this order; original observations are distinct from effective presentation and final writer evidence.

| Stage | Body mode | Allowed actions / purpose |
|---|---|---|
| `request` | `none` or `buffered` | `pass`, `replace` request, `respond` locally |
| `resolve` | `none` or `buffered` | Same; may return separate upstream `credentials` |
| `upstream` | `none` or `buffered` | Same; may return separate upstream `credentials` |
| `observe` | `none` | `pass` only; original attempt response facts |
| `responseStart` | `none` | `pass`, `replace` response metadata, `child` request |
| `originalBody` | `stream` | `pass` only; original upstream bytes before presentation edits |
| `responseBody` | `none`, `buffered` or `stream` | `pass`, `replace`; body access/replacement requires a declared body mode |
| `finish` | `none` | `pass` only; reader completion/error, not delivery |
| `delivery` | `none` | `pass` only; external writer acceptance/error, not peer receipt |

Observer-only stages cannot return metadata or credentials. The host validates stage/action/body combinations; TypeScript's shared result union alone is not proof that an action is allowed at a particular stage. Local responses go through the response chain. Response-triggered child requests go through the request chain; child and parent each have response processing, so non-idempotent decorators must account for both.

### Descriptor fields and ordering

Required: `id`, `stage`, `bodyMode`. Optional:

- `failure`: `block` (default) or explicit `failOpen`. Required policy/authentication checks should not fail open.
- `formats`: canonical MIME types without parameters, for example `['application/json']`; not protocol parsing.
- `order`: numeric same-stage priority; lower runs first.
- `requires`: required hook IDs that must exist and precede this hook.
- `before` / `after`: ordering constraints; missing optional targets are ignored.

Global IDs are `installation-id/local-id`, at most 128 bytes altogether. Local IDs begin with a letter and use letters, digits, `_`, `.`, `-`. Unqualified dependencies are local; use `other-extension/hook-id` across packages. Equal priorities use dependency order then stable global ID. Cycles, missing required dependencies and contradictions with numeric ordering fail startup. Do not register after setup or depend on installation order.

## 5. Input snapshots and result helpers

`HTTPInput` contains `v: 1`, stage, identity, requested/request/response messages, owner metadata, ledger, truncation flag, provenance, trusted auth, error and optional stream/delivery evidence. See [exact fields](../sdk/http.d.mts).

- `identity`: root, exchange, parent, attempt and depth. Use these for correlation, not authorization from user headers.
- `requested`: initial snapshot without retained body; `request` / `response`: stage-specific snapshots.
- `metadata`: your installation's string-valued namespace. Return its complete replacement explicitly, e.g. `{ ...pass(), metadata: { ...input.metadata, decision: 'accepted' } }`, only at mutable stages. It is not a durable database.
- `ledger` / `provenance`: detached host evidence; inspect `truncated`. Cannot rewrite host history through your result.
- `auth`: host-established subject/claims, not proof supplied by model arguments or public identity headers. The stock CLI has no built-in caller authentication.
- `requestHasTrailers`: original ingress had declared trailer keys. Read-only, not a mutable trailer API or guarantee of detecting undeclared trailers. Older hosts omit it; absence is not confirmed false.
- `delivery`: writer status/bytes and errors; not proof the remote client consumed the response.

A `Message` is `{ method, url, header, status, body? }`. Headers are `Record<string, string[]>`. Request URL must be absolute HTTP(S), request status is `0`; response method/URL may be empty. Bodies are canonical base64, never plain text. Preserve supplied fields using spread when replacing. `bodyMode: 'none'` replacements must omit body; `respond` and `child` require body even when empty (`''`).

| Helper | Result |
|---|---|
| `pass()` | `{ action: 'pass' }` |
| `replace(message)` | `{ action: 'replace', message }` |
| `respond(message)` | `{ action: 'respond', message }` |
| `child(request)` | `{ action: 'child', request }` |
| `streamBytes(unit, maxBytes?)` | Validate/decode a bounded base64 stream unit to bytes |
| `streamUnit(id, sequence, bytes, end?, maxBytes?)` | Construct generic bounded stream data; **not itself an HTTP action** |

`defineExtension` is the lower-level generic worker setup helper. Prefer `defineHTTP` for installed HTTP hooks. Helpers construct objects; Go remains authoritative. Unknown fields, duplicate JSON keys, null fields, malformed bodies and illegal actions fail validation. Returning nothing is not a pass.

## 6. Recipes

These snippets go inside `defineHTTP`; import the helpers they use. They are independent recipes, not one combined registration set.

### Local response

```ts
api.on({ id: 'health', stage: 'request', bodyMode: 'none' }, input => {
  if (new URL(input.request.url).pathname !== '/extension-health') return pass();
  return respond({ method: '', url: '', status: 200,
    header: { 'Content-Type': ['text/plain'] }, body: btoa('ok\n') });
});
```

`btoa` here encodes ASCII only. For arbitrary UTF-8/binary in Node, use `Buffer.from(text, 'utf8').toString('base64')` (and install `@types/node` for TypeScript authoring).

### Routing and credentials

In a `resolve` or `upstream` handler, after your application authenticates the caller and authorizes an exact target:

```ts
return {
  ...replace({ ...input.request, url: authorizedEndpoint }),
  credentials: { Authorization: [`Bearer ${selectedCredential}`] },
};
```

`authorizedEndpoint` and `selectedCredential` are application-owned values, not SDK globals. Keep upstream credentials in the separate result field, not ordinary headers/metadata. The host applies them for dispatch without including that credential map in later Node snapshots. This is not a substitute for your destination authorization, caller auth, credential storage or refresh policy. Do not log the result. Trusted extensions can still make unmanaged network calls.

### Buffered body transformation

Declare `responseBody` / `buffered`, decode `input.response.body`, transform only formats you understand, then return `replace({ ...input.response, header: correctedHeaders, body: encodedBytes })`. Body-changing code must maintain representation headers: do not leave stale `Content-Length`, compression, digest or signature metadata. Reject unsupported encodings rather than interpreting compressed bytes as text. A retained-body limit failure does not silently switch to streaming.

### Opaque streaming pass-through or transformation

```ts
api.on({ id: 'units', stage: 'responseBody', bodyMode: 'stream' }, input => {
  if (!input.stream) throw new Error('missing stream unit');
  const bytes = streamBytes(input.stream, 32 * 1024);
  // Inspect bytes or transform with a bounded, framing-aware algorithm.
  return replace({ ...input.response, body: input.stream.data });
});
```

Units are arbitrary byte chunks, **not SSE events, JSON values or complete UTF-8 characters**. IDs distinguish hook/exchange/attempt/stage; sequence starts at zero. There is no fabricated final unit (`end` is false for native data callbacks); use `finish` for terminal evidence. Whole-response and Unit response transforms cannot be mixed. Before changing length/encoding in a stream, arrange corresponding response headers at `responseStart`.

### Response-triggered new request

At `responseStart`, return `child({ method: 'GET', url: authorizedEndpoint, status: 0, header: {}, body: '' })` to send a bounded full request through the native request pipeline. Authorize the destination and retry safety in your application. Do not replay POST/tool effects blindly; distinguish a new effect from retrying an uncertain one. Child calls share root limits and can reenter the same hook: check identity/depth and keep a bounded policy.

### Ordinary internal HTTP through this proxy

```ts
api.on({ id: 'internal-example', stage: 'request', bodyMode: 'none' },
  async (input, context) => {
    if (new URL(input.request.url).pathname !== '/ask-internal') return pass();
    const config = api.config;
    if (!config || typeof config !== 'object' || Array.isArray(config) ||
        typeof config.gatewayOrigin !== 'string') throw new Error('missing gateway origin');
    const response = await context.http({
      method: 'GET', url: config.gatewayOrigin + '/internal',
      status: 0, header: {}, body: '',
    });
    return respond(response);
  });
```

The `/internal` request passes this handler without recursion because of the path check. All ordinary chains run again. Only the exact configured gateway origin is allowed; redirects, reserved tracking headers, streaming/WebSocket options and alternative origins are rejected. One outstanding helper call per invocation; do not retain it or launch fire-and-forget calls. Parent cancellation expires it. Capacity exhaustion rejects immediately rather than queueing behind the parent.

This helper reuses **host-verified** authentication in an integrated authenticated host, never identity copied from public headers. It does not inherit upstream credential headers or add authentication to the bare CLI. Embedded ingress must bind before authentication middleware. See the [SDK's managed HTTP contract](../sdk/README.md#managed-ordinary-http).

## 7. Failure, bounds and safety

`pass` is an intentional no-op. `failOpen` recovers from hook/result failure using the last accepted input, discarding failed candidate output. `block` terminates processing. After bytes are committed, failure cannot retract them: a blocking stream error aborts. Whole-answer recovery requires bounded whole retention; unit recovery only restores that unit. No mode rolls back external effects.

Default bridge limits include 256 KiB buffered bodies/managed payloads, 32 KiB units, 1 MiB complete IPC frames, worker concurrency 16, native admission 64 and worker timeout 30 seconds. Base64/envelope/headers also consume frame space. Embedded hosts can configure limits; the CLI uses bridge defaults. See [full bounds](../sdk/README.md#bounds-and-lifecycle).

Do not count cancellation as released capacity until underlying work settles or the process is killed. Frozen registrations mean no hot reload. Worker calls are not automatically replayed after crashes. Keep application persistence outside the installation directory; removal/reinstallation replaces code, not a database migration mechanism. Choose atomic persistence, idempotency and recovery appropriate to your effects.

## 8. Install, activate and smoke-test

Build ProxyKit once with `go build -o /tmp/proxykit ./cmd/proxykit` in its checkout. Start a disposable local upstream on port 8090, then in the server environment:

```sh
export PROXYKIT_EXTENSIONS_DIR=/tmp/proxykit-example-extensions
# Substitute a reviewed repository containing the files described above:
/tmp/proxykit extension install https://github.com/OWNER/REPOSITORY/tree/v1.0.0 --trust
/tmp/proxykit extension list
/tmp/proxykit -listen 127.0.0.1:8080 -upstream http://127.0.0.1:8090
```

From another terminal, `curl -i http://127.0.0.1:8080/` should show `X-Example-Extension: enabled` for an upstream 200. Use a repository URL or `/tree/REVISION` with one unambiguous revision segment (a simple tag or commit is suitable). The installer resolves an immutable commit and records provenance. It does not install a subdirectory of a monorepo. No local-directory install or npx workflow is implied by these commands.

Installation alone does not run the code. Start/restart the server with the same store to activate it. Startup validates all records and registrations; failures are not silently skipped. Updates currently require stopping the server, explicit removal, installation and restart; there is no atomic live-update promise. Never edit host-owned `installation.json` or synthesize provenance to bypass the installer.

Before publishing, test:

- TypeScript compilation, built entry import, handler pass/change and invalid inputs.
- Real installer → compiled CLI → Node worker → local upstream → client output.
- Authentication denial, allowed destinations, credential non-disclosure and tenant isolation where applicable.
- Cancellation, bounded overflow, crash, required failure versus explicit fail-open, ordering/dependency startup failures and shutdown cleanup.
- Split reads, non-ASCII/binary bodies and unsupported encodings for body hooks.
- Persistence/restart and uncertain effects for stateful extensions.

Unit tests can invoke setup with a fake `on/config/log/cleanup` API to capture handlers, but that does not certify native action validation or installed behavior. Existing executable references: [minimal ESM extension](../examples/node-personal-usage/), [typed authoring fixture](../sdk/authoring.fixture.mts), [Node bridge tests](../nodehost/host_test.go), [CLI activation tests](../cmd/proxykit/activation_test.go) and [SDK tests](../sdk/authoring.test.mjs).

## 9. Troubleshooting / publishing checklist

| Symptom | Check |
|---|---|
| Install cannot find manifest/entry | Root manifest and prebuilt `.mjs` must be in the selected commit, not only your laptop/release attachment |
| Runtime cannot resolve import | Include relative modules/assets; no server-side npm install occurs |
| Startup fails ordering validation | Missing requires, cycles, global ID length, incompatible body modes or numeric-order contradictions |
| Hook output rejected | Explicit action, full snapshot fields, header arrays, body-mode rules and canonical base64 |
| Stream is aborted | Blocking failure after commitment cannot become a replacement response |
| Recursive call is 502/busy | Shared limits still apply; do not bypass hooks/auth to avoid capacity errors |
| Process framing fails | Direct stdout output is forbidden; complete message may exceed IPC bound |
| Installed changes do not appear | Restart with the same extension store; no hot swap |

Publish only when your README states configuration, required Node/ProxyKit compatibility, install URL, ports, data directory, trust implications, test commands and known limitations. Link back to this guide and pin the host/SDK revision you tested. The generic extension API does not provide provider OAuth, model parsing, account rotation, a secret vault, durable tool orchestration or a UI automatically: those are application responsibilities or separate native examples, not callable TypeScript SDK services.
