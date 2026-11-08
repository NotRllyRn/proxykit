# Personal usage: installed Node HTTP example

Prebuilt dependency-free ESM; no package manager, transpilation or setup script. This trusted worker logs original response observations and adds `X-Personal-Usage` before response commitment. Its counter is process-local, not durable accounting or a peer-delivery guarantee.

Publish this directory's two files (`proxykit-extension.json`, `extension.mjs`) at the root of your own GitHub repository. Replace the example URL below with that repository; it is not a live fixture. The installer pins its immutable commit. Review the code before accepting arbitrary server-side code execution:

```sh
go build -o /tmp/proxykit ./cmd/proxykit
/tmp/proxykit extension install https://github.com/example/personal-usage --trust
# Offline: persist explicit intent; alternatively enable after the server starts.
/tmp/proxykit extension enable personal-usage
# Start a local ordinary HTTP upstream in another terminal:
node --input-type=module -e 'import http from "node:http"; http.createServer((req,res)=>res.end("hello\n")).listen(9090,"127.0.0.1")'
/tmp/proxykit -listen 127.0.0.1:8080 -upstream http://127.0.0.1:9090
curl -i http://127.0.0.1:8080/example
# Expect HTTP 200, X-Personal-Usage: 1 and hello.
```

Installation defaults disabled. Online `extension enable personal-usage` / `disable personal-usage` changes pipelines without restarting the listener; `extension status` separates enabled intent from active/draining/failed state. Unchanged workers retain their counters and resources. Disable and wait for drain/reaping before removal/replacement. Workers clean up after HTTP drain; setup/descriptor/dependency failures preserve the prior pipeline. See [finite drain and legacy policy](../../docs/HOT_ACTIVATION.md). `PROXYKIT_EXTENSIONS_DIR` isolates the store; `-node` selects the Node binary. Node must be installed separately. Linux / Node 24 is the tested process environment; this is not a sandbox.

For an entirely local executable install/hot-enable-path check without publishing a repository or using GitHub credentials:

```sh
go test ./cmd/proxykit -run '^TestSystemInstalledNodeActivationAndShutdown$' -count=1
```

That test serves a fake GitHub commit/archive, runs the real installer, launches the compiled CLI with its temporary store, forwards HTTP through a real Node response hook, checks disabled pass-through, enables online and checks the changed body, then sends SIGTERM and checks worker cleanup plus process exit. See `sdk/README.md` for the full HTTP authoring contract.
