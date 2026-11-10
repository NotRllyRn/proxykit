# Set up ProxyKit and install extensions

This guide runs one local ProxyKit server, then installs and activates a trusted GitHub extension without restarting it. Commands use a POSIX shell on Linux, macOS or FreeBSD; extension management requires a local filesystem supporting Unix sockets and file locks.

## 1. Build ProxyKit

Install Git and **Go 1.26+**. For Node extensions, install **Node.js** (tested with Node 24) and ensure `node` is on PATH.

```sh
git clone https://github.com/NotRllyRn/proxykit.git
cd proxykit
go build -o ./proxykit ./cmd/proxykit
./proxykit -help
node --version
```

There is no npm/npx installation step for ProxyKit. The Go binary runs the server; Node runs JavaScript/TypeScript extensions.

## 2. Choose the extension store

Run this in **every terminal** used below, under the same OS user:

```sh
export PROXYKIT_EXTENSIONS_DIR="$HOME/.local/share/proxykit/extensions"
```

This directory holds installed packages, enabled state and the private management socket. Keep it on a local disk, private to the server account. Do not edit its files manually. Only one server can own a store at a time.

If unset, ProxyKit uses your OS user configuration directory under `proxykit/extensions`. Choosing an explicit path avoids accidentally managing a different store.

## 3. Start the server — terminal A

From the cloned repository:

```sh
./proxykit -listen 127.0.0.1:8080 -upstream https://example.com
```

Leave it running. Replace `https://example.com` with your actual upstream when ready.

ProxyKit is a **reverse proxy**: clients use `http://127.0.0.1:8080` as their base URL. Do not set it as a CONNECT proxy through `HTTP_PROXY` or `HTTPS_PROXY`.

## 4. Check forwarding — terminal B

Open another terminal, enter the same repository directory and set the store variable from step 2. Then:

```sh
curl -i http://127.0.0.1:8080/
./proxykit extension status
```

The HTTP response comes from the upstream. Status shows the extension inventory; a fresh store has none. For a real API, use its normal path and authentication. An upstream authentication error does not necessarily mean the proxy failed.

## 5. Install an extension

Review the repository first: **extensions execute arbitrary code with the server account's privileges**. `--trust` acknowledges this; it does not sandbox or audit the code.

Replace `OWNER/REPOSITORY` below with a real compatible repository:

```sh
./proxykit extension install https://github.com/OWNER/REPOSITORY --trust
./proxykit extension status
```

**Installation leaves the extension disabled**, even after restarting the server. Its ID comes from the manifest, not necessarily the GitHub repository name.

Before activation, follow the extension's own configuration instructions. Set any required environment variables in **terminal A before starting the server**; exporting them only in terminal B does not change the running server's environment. Missing configuration may require restarting the server once with the right environment. Hot activation itself does not require a restart.

### Persistent extension data

Installation automatically creates or validates a private directory at `<store>/.data/<manifest-id>`, keyed by the unique manifest ID—not its display name. No directory initialization command is needed. The host validates it again before setup and supplies its absolute path as `api.dataDir`. Extensions can initialize their own files during setup, or document optional application-specific configuration. Creating the directory does not create application configuration.

Installed Node extensions receive this path as `api.dataDir` during setup. Data survives disable/enable, server restart, package removal and reinstall; removal does **not** erase credentials or state. Different IDs have separate directories. Back up sensitive state appropriately; delete it manually only when you intentionally want to discard it and the extension has fully stopped.

### Required package format

Prefer a repository containing a root manifest and TypeScript source entry:

```text
proxykit-extension.json
src/extension.mts
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

The entry must implement ProxyKit's extension API; an arbitrary script is not enough. `.mts`/`.ts` selects source mode: the host's embedded, pinned **TypeScript 5.9.3** compiler strictly typechecks against the embedded SDK and compiles in temporary staging, without executing package code. No compiler download, npm/npx, package build/lifecycle scripts or local `tsc` is used. Node is required for source installation as well as activation. A compiler failure publishes nothing and does not change enabled intent.

Source mode is deliberately narrow: relative source imports and `@proxykit/sdk` only, fixed ES2022 ESM/strict options, no package tsconfig, copied JS, Node builtin imports or third-party dependency resolution. Assets retain their relative layout beside generated modules; use `new URL('./asset', import.meta.url)`. See [source limits, compiler hash/license/provisioning and integrity](EXTENSION_AUTHORING.md#host-compiler-and-source-mode-limits).

Prebuilt `.mjs` remains fully supported, including author-precompiled TypeScript: use `"entry": "dist/extension.mjs"` and commit that file and its dependencies/assets. It is not rebuilt or typechecked by the installer. Missing or unsupported `apiVersion` is rejected in either mode. Authors should use the [developer guide](EXTENSION_AUTHORING.md).

## 6. Activate and verify

Replace `example-extension` with the installed ID:

```sh
./proxykit extension enable example-extension
./proxykit extension status example-extension
```

Confirm that status reports it enabled and active, then run the extension's documented smoke test—for example, check its response header or authenticated service endpoint. Installation alone is not proof that an extension works.

For future installations, combine steps 5 and 6 while the server is running:

```sh
./proxykit extension install https://github.com/OWNER/REPOSITORY --trust --activate
```

If activation fails, the new package remains installed but disabled; the existing pipeline stays active. Inspect the command error and status. Offline `--activate` installs disabled and returns an error. To prepare an installation before starting a server, use separate `install` and `enable` commands: offline enable saves intent, and full pipeline validation happens at startup.

## 7. Disable, remove or update

```sh
./proxykit extension disable example-extension
./proxykit extension status example-extension
```

New requests stop using it. Existing requests retain their original pipeline while draining. Wait until status shows **no active, draining or cleanup owner** before removal:

```sh
./proxykit extension remove example-extension
```

To update, disable → wait for cleanup → remove → install the new revision → enable. There is no in-place update command or overlapping stateful replacement. Unrelated extension changes reuse unchanged workers, preserving their listeners and state.

The default retired-pipeline drain deadline is **30 seconds**; long-lived streams can be interrupted at that deadline. See [hot activation and lifecycle limits](HOT_ACTIVATION.md) for configuration and failure behavior.

## Troubleshooting

| Symptom | Check / action |
| --- | --- |
| Installed but not active | Run `extension enable ID`; inspect `extension status ID`. |
| API version missing or unsupported | Ask the author for a compatible package; do not merely relabel incompatible code. |
| Node/setup failure | Check Node on the server's PATH, required configuration and the server's stderr. |
| Cannot contact running server | Use the same store and OS user. A busy store never falls back to offline mutation. |
| Store already owned | Another server uses it. Use that server or stop it; do not delete the lock file. |
| Old installation blocks startup | Inspect offline status; explicitly disable incompatible/legacy records, then reinstall compatible packages. Do not bypass required security extensions. |
| Port or state-lock conflict | Wait for the old extension to finish cleanup. Consult the extension's recovery instructions; do not delete its locks blindly. |

## Stop safely and deploy deliberately

Press **Ctrl+C** in terminal A to shut down. Enabled intent persists for the next startup; activation is validated again.

The example binds to loopback. ProxyKit has **no built-in client authentication or dashboard**. Do not expose an unauthenticated listener publicly. Public deployment needs access controls and TLS (a trusted frontend, or `-tls-cert` plus `-tls-key`). Credentials and sensitive runtime files must stay out of Git repositories. Extension-owned dashboards and enrollment services have their own setup and security requirements.

Next: [developer guide](EXTENSION_AUTHORING.md) · [hot activation contracts](HOT_ACTIVATION.md) · [README](../README.md).
