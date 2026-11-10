# Writing a ProxyKit TypeScript extension

This is the developer entry point for **installed HTTP v1 extensions**. It documents the implemented Node boundary, not proposed native Go APIs. Start with the complete example below, then consult the [SDK reference](../sdk/README.md), [TypeScript API declarations](../sdk/http.d.mts), [helper declarations](../sdk/index.d.mts) and [installer/runtime contract](implementation/distribution.md). The [requirement status table](REQUIREMENT_STATUS.md) distinguishes implemented capabilities from future work.

## 1. Mental model and conventions

- ProxyKit runs ESM in a trusted Node process. Prefer **TypeScript source installation** for small dependency-free extensions: publish a `.mts` or `.ts` entry and let the host typecheck and compile it. Export one setup function; register hooks during setup. Go owns HTTP dispatch, body retention, ordering and result validation.
- Prebuilt `.mjs` remains supported unchanged, including author-precompiled TypeScript and bundled dependencies. Installation never runs npm/npx, dependency installation, package build/lifecycle scripts or package code.
- Handlers decide whether to act. Return `pass()` when not applicable; there is no separate check callback.
- Return detached full snapshots, not mutations or patches. Keep handlers small, choose explicit body/failure modes and avoid global per-request state. Never assume requests execute serially.
- Use stable descriptive hook IDs, generic example labels and documented configuration. Keep secrets, databases and runtime output out of Git. Document permissions, ports, persistence, limits and unsupported modes in your extension's README.
- Extensions are **not sandboxed**. They have the server user's filesystem/network privileges. `--trust` means consent to code execution, not verification of safety.

## 2. Preferred: publish TypeScript source

Publish a root manifest plus source and regular-file assets. No copied SDK, `dist`, package.json or tsconfig is required:

```text
example-extension/
├── proxykit-extension.json
├── README.md
├── LICENSE
└── src/
    ├── extension.mts
    ├── label.ts
    └── asset.txt
```

```json
{
  "manifestVersion": 1,
  "apiVersion": 1,
  "id": "example-extension",
  "version": "1.0.0",
  "entry": "src/extension.mts"
}
```

```ts
// src/extension.mts
import { defineHTTP, pass, replace } from '@proxykit/sdk';
import { label } from './label.js'; // resolves to label.ts; emitted as label.mjs
export default defineHTTP(api => {
  api.on({ id: 'response-label', stage: 'responseStart', bodyMode: 'none' }, input => {
    if (input.response.status !== 200) return pass();
    return replace({ ...input.response,
      header: { ...input.response.header, 'X-Example-Extension': [label] } });
  });
});
// src/label.ts: export const label: string = 'enabled';
```

### Host compiler and source-mode limits

The Go binary embeds the **unmodified official TypeScript 5.9.3 npm archive**,
Apache-2.0 license and host-owned driver. Its SHA-256 is
`10e108c9cf7d5f2879053dff18515fb405abf2ccef63eaaf017d9c571687a1d3`.
The artifact was retrieved from the official npm registry and verified against
its SHA-512 SRI; see [license and provenance](../extensions/compiler/PROVENANCE.md).
Building the Go binary provisions the compiler automatically with `go:embed`;
there are no runtime downloads, npm calls, arbitrary local `tsc` lookup or
`TSC_BINARY` override (that variable is only for SDK authoring tests). Node must
be installed on PATH; a running embedded host can use its configured Node binary.

This is **real strict typechecking**, not transpile-only: all package `.ts`/`.mts`
files and the actual host-embedded SDK declarations are checked, including SDK
export names and callback types, before any output is published. The matching
host SDK runtime is copied into the generated tree. Fixed options: ES2022 ESM,
strict, ES2022 + DOM standard libraries (for SDK AbortSignal/URL types), no ambient
package types, no plugins or package config. TypeScript's explicit escape hatches
such as `any` and diagnostic suppression still work; typechecking is not runtime
validation or a sandbox, and DOM declarations do not imply browser APIs in Node.

Supported imports are `@proxykit/sdk` and **relative package source files only**.
Use explicit `.mts`/`.ts`, or runtime-style `.mjs` → `.mts` and `.js` → `.ts`.
Static imports/re-exports and literal dynamic imports are rewritten to the exact
emitted `.mjs` paths. Type-only imports use the same restriction. No extensionless,
directory, bare dependency, Node builtin, JSON, URL, query/fragment, escaping or
nonliteral imports. No JSX, CommonJS, namespaces, declaration files or triple-slash
directives. Source packages containing JavaScript or `tsconfig*.json` are rejected,
not silently built with different settings. An optional package.json may declare
`type: module`; its scripts are never executed. Use the prebuilt workflow below
for Node builtin typings, copied SDKs, third-party dependencies or custom compiler
configuration. This deliberately narrow first source mode is not a full Node
application build system.

The original archive remains under `package/`; generated ESM and copied assets
are under the reserved `package/.proxykit-build/`, preserving relative layout.
Both `.ts` and `.mts` emit `.mjs`. Regular non-source assets (except package.json)
are mirrored beside modules, so `new URL('./asset.txt', import.meta.url)` points
to the runtime copy; do not resolve assets against cwd or assume source filenames
exist beside emitted modules. Assets cannot be imported as modules. `.sdk` and
`.proxykit-build` at package root are reserved. Output-name collisions fail
installation. Build tools never write into `api.dataDir`.

Archive download/extraction bounds still apply. Generated-plus-source trees are
limited to 128 MiB and 8192 filesystem objects; emitted modules additionally to
64 MiB / 2048 files. Compilation has a 30-second default deadline, 256 MiB V8 old
heap plus 16 MiB semi-space setting, bounded 16 KiB retained diagnostics and pipe
completion, and context cancellation/forced termination (private process group on
Unix; direct child elsewhere). V8 settings are **not a hard RSS/OS memory sandbox**.
Embedded installers may choose a shorter timeout or at most two minutes. The
host driver spawns no subprocesses. Compilation precedes package publication,
persistent data creation and enabled-state intent changes. A failed compile
removes staging and leaves existing packages/running generations/intent intact.
Installation still defaults disabled; no source code executes until activation.

Host provenance records the compiler version/hash and a SHA-256 digest covering
original source, manifest, generated modules, SDK and assets. Inventory/activation
reject modified trees and links/special files. This detects changes relative to
host-owned provenance, not a signature, GitHub authenticity proof or protection
against trusted same-UID code changing provenance itself. Prebuilt `.mjs` retains
its existing behavior and does not acquire this new source-build integrity check.

### Optional: author-precompiled TypeScript / prebuilt ESM

For the broader prebuilt workflow, a repository can instead look like this:

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
  "apiVersion": 1,
  "id": "example-extension",
  "version": "1.0.0",
  "entry": "dist/extension.mjs"
}
```

These are the **only** manifest keys. Unknown/duplicate keys fail validation.

| Field | Contract |
|---|---|
| `manifestVersion` | Manifest schema integer `1` |
| `apiVersion` | Required host/author HTTP API major integer `1`; independent of package version and IPC `v:1`. Missing/unsupported majors are rejected, never inferred from copied SDK files |
| `id` | `[a-z][a-z0-9-]{0,63}`; stable installation identity |
| `version` | SemVer-shaped string, at most 128 bytes |
| `entry` | Safe relative existing regular `.mts`/`.ts` (source mode) or `.mjs` (prebuilt); no absolute path or `..` |

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
| `api.dataDir` | Read-only absolute host-created private directory for this manifest ID; persists independently of installed code |
| `api.config` | Host-supplied JSON; CLI supplies `gatewayOrigin`, not an arbitrary per-package configuration UI |
| `api.log(...args)` | Log to stderr; redact secrets and request content |
| `api.cleanup(fn)` | Register sync/async cleanup, called in reverse order after active handlers settle |
| `context.signal` | AbortSignal; cooperate with cancellation and avoid effects after it is aborted |
| `context.deadline` | Optional absolute Unix timestamp in milliseconds |
| `context.http(request)` | Bounded, invocation-scoped internal HTTP through this gateway; unavailable in delivery handlers |

Async setup is awaited. If setup cannot acquire a resource, clean up anything it already acquired before throwing. Register cleanup immediately after successful acquisition. Close listeners, timers and owned connections during cleanup. Bound shutdown; a hanging handler/cleanup can cause worker termination. Cancellation does not roll back external side effects.

On Unix (AIX, Darwin, DragonFly, FreeBSD, Linux, NetBSD, OpenBSD and Solaris), each worker runs in a private process group. Terminal Ctrl-C reaches the CLI (including when launched with `go run`), not Node or its inherited subprocesses. The host requests protocol shutdown and awaits setup before running cleanup; cancellation during setup receives a cleanup opportunity bounded by the cancellation grace and shutdown timeout too. Startup timeout, cancellation grace and shutdown deadlines still forcibly terminate uncooperative workers. On Linux, when a worker exits or is forcibly terminated, the host kills its remaining process-group descendants before reaping the leader: a non-reaping exit observation keeps the numeric group ID reserved until signaling finishes. If that observation is unavailable (for example, blocked by seccomp), supervision fails with an error and falls back to direct-child termination. Other Unix platforms retain terminal group isolation but only direct-child forced termination; other platforms retain direct-child termination without Unix group isolation. Extensions must still close/wait for their own subprocesses during cleanup. Subprocesses that detach into another group/session, and descendant cleanup on platforms without the Linux observation mechanism, are outside this guarantee.

If host protocol input closes (including abrupt host death), the runtime attempts the same setup-then-cleanup sequence and enforces its shutdown timeout locally before ending the worker group. This fallback requires a responsive Node event loop and closed protocol descriptors: blocking native/JavaScript work or a process retaining the host's pipe can prevent it. Normal host-owned shutdown still enforces deadlines externally. Direct worker SIGKILL, power loss, setup/cleanup failure, or forced deadline termination cannot guarantee cleanup or durable lock release. ProxyKit does not delete extension-owned ambiguous/live locks; extensions must define safe resource recovery rather than assuming cleanup always runs.

Stdout is reserved for framed IPC. `api.log` and standard console methods go to stderr; do not use `process.stdout.write` or create a stdout logger.

An extension can implement its own dashboard/enrollment HTTP servers using Node builtins. There is **no** `api.router`, automatic port allocation, public URL provisioning, TLS, dashboard framework or built-in OAuth. Provision ports/configuration explicitly; start listeners in setup and close them in cleanup. Use separate authentication for administrative and public-facing flows, constrain request sizes, and keep secrets out of URLs/logs. Default to loopback until deployment access controls are configured. A public enrollment flow is application code, not an installer feature.

### Persistent data and application configuration

Installation automatically creates or validates `<store>/.data/<manifest-id>`.
The host validates it again and supplies its absolute path as `api.dataDir` before
calling setup, including for packages installed by older hosts. No user directory
initialization step is needed. Use normal Node filesystem APIs to initialize your
application's files during setup, not a host storage service. For example:

```js
import { open, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { defineHTTP } from '../sdk/index.mjs';

export default defineHTTP(async api => {
  if (!api.dataDir) throw new Error('This extension requires a host with api.dataDir');
  const path = join(api.dataDir, 'config.json');
  try {
    const file = await open(path, 'wx', 0o600); // Do not overwrite existing config.
    try { await file.writeFile(JSON.stringify({ label: 'Personal usage' })); }
    finally { await file.close(); }
  } catch (error) {
    if (error.code !== 'EEXIST') throw error;
  }
  const config = JSON.parse(await readFile(path, 'utf8'));
  // Validate config and register your hooks/listeners here.
});
```

The host creates a directory, not application configuration. If your extension
requires user input or supports an optional standalone configuration tool, document
that workflow, the selected store/manifest ID, and when configuration must be
ready. Do not use `--activate` until any required application configuration is
ready. The host does not discover arbitrary configuration or copy an old external
store. Stop old owners before an application-specific migration.

Manifest IDs, not display labels or package versions, determine storage identity.
The manifest has no separate display-name field; identical application/hook labels
under distinct IDs still receive different directories. One directory per ID,
no multi-instance framework. Disable, cleanup, restart, remove/reinstall and version
replacement keep data; removal never silently deletes it. Keep databases/secrets
out of package files. Authors own schema validation/migration, backups, atomic
writes and concurrent access; use private file modes (0600) for secrets. No CRUD,
database, quota, secret vault or sandbox API is implied. The host refuses symlinks,
non-directory/wrong-owner/non-0700 data paths instead of repairing them; trusted
store ancestors and the server identity remain the security boundary. Trusted
same-UID code can access other data. `apiVersion` remains **1**; document a minimum
tested host revision when requiring this additive field (older hosts omit it).

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

Do not count cancellation as released capacity until underlying work settles or the process is killed. Registrations are frozen per worker. Activation publishes a new immutable pipeline while reusing unchanged healthy workers; existing roots and all descendants keep their prior pipeline. Worker calls are not automatically replayed after crashes. Keep application persistence outside the installation directory; removal/reinstallation replaces code, not a database migration mechanism. Choose atomic persistence, idempotency and recovery appropriate to your effects.

## 8. Install, activate and smoke-test

Build ProxyKit once with `go build -o /tmp/proxykit ./cmd/proxykit` in its checkout. Start a disposable local upstream on port 8090, then in the server environment:

```sh
export PROXYKIT_EXTENSIONS_DIR=/tmp/proxykit-example-extensions
# Substitute a reviewed repository containing the files described above:
/tmp/proxykit extension install https://github.com/OWNER/REPOSITORY/tree/v1.0.0 --trust
/tmp/proxykit extension status
# Either persist enable intent while offline, then start:
/tmp/proxykit extension enable example-extension
/tmp/proxykit -listen 127.0.0.1:8080 -upstream http://127.0.0.1:8090
# Or start disabled, then enable in another terminal without listener restart.
```

From another terminal, `curl -i http://127.0.0.1:8080/` should show `X-Example-Extension: enabled` for an upstream 200. Use a repository URL or `/tree/REVISION` with one unambiguous revision segment (a simple tag or commit is suitable). The installer resolves an immutable commit and records provenance. It does not install a subdirectory of a monorepo. No local-directory install or npx workflow is implied by these commands.

Installation alone does not run code: new packages are **disabled**. On a running server, `extension enable ID` validates and publishes the entire candidate pipeline, and `extension disable ID` retires the old one. `install URL --trust --activate` requires online activation; offline it leaves the installation disabled and returns an error. `extension status [ID]` shows persisted intent separately from active/draining/failed/incompatible state; offline runtime fields are unknown. Offline enable/disable persists intent only, with full dependency validation deferred to startup. An unresolved legacy record blocks startup until explicitly disabled or reinstalled; incompatible manifests never execute. Removal requires no active/draining/reaping owner. For replacement: disable, wait for retirement/reaping, remove, install new version, enable. No overlapping stateful owners or transactional migration promise. Never edit host-owned installation/state files to bypass the installer. See [activation contracts and finite drain limits](HOT_ACTIVATION.md).

Before publishing, test:

- TypeScript compilation, built entry import, handler pass/change and invalid inputs.
- Real installer → compiled CLI → Node worker → local upstream → client output.
- Authentication denial, allowed destinations, credential non-disclosure and tenant isolation where applicable.
- Cancellation, bounded overflow, crash, required failure versus explicit fail-open, ordering/dependency startup failures and shutdown cleanup.
- Split reads, non-ASCII/binary bodies and unsupported encodings for body hooks.
- Persistence/restart and uncertain effects for stateful extensions.

Unit tests can invoke setup with a fake `on/config/dataDir/log/cleanup` API to capture handlers, but that does not certify native action validation or installed behavior. Existing executable references: [minimal ESM extension](../examples/node-personal-usage/), [typed authoring fixture](../sdk/authoring.fixture.mts), [Node bridge tests](../nodehost/host_test.go), [CLI activation tests](../cmd/proxykit/activation_test.go) and [SDK tests](../sdk/authoring.test.mjs).

## 9. Troubleshooting / publishing checklist

| Symptom | Check |
|---|---|
| Install cannot find manifest/entry | Root manifest and selected `.mts`/`.ts` source or prebuilt `.mjs` must be in the selected commit, not only your laptop/release attachment |
| Import/config rejected during source install | Follow source-mode limits above, or publish prebuilt `.mjs` for custom tooling/Node builtins; no server-side npm install occurs |
| Startup fails ordering validation | Missing requires, cycles, global ID length, incompatible body modes or numeric-order contradictions |
| Hook output rejected | Explicit action, full snapshot fields, header arrays, body-mode rules and canonical base64 |
| Stream is aborted | Blocking failure after commitment cannot become a replacement response |
| Recursive call is 502/busy | Shared limits still apply; do not bypass hooks/auth to avoid capacity errors |
| Process framing fails | Direct stdout output is forbidden; complete message may exceed IPC bound |
| Installed changes do not appear | Install defaults disabled; inspect `extension status`, then explicitly enable using the same store |
| Manifest API rejected / legacy startup blocked | Publish `apiVersion: 1`; inspect status and explicitly disable incompatible legacy records before restarting |

Publish only when your README states configuration, required Node/ProxyKit compatibility, install URL, ports, data directory, trust implications, test commands and known limitations. Link back to this guide and pin the host/SDK revision you tested. The generic extension API does not provide provider OAuth, model parsing, account rotation, a secret vault, durable tool orchestration or a UI automatically: those are application responsibilities or separate native examples, not callable TypeScript SDK services.
