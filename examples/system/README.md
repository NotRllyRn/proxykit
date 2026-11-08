# Optional combined system

`system.New` returns the existing `brokerapp.Host` (`http.Handler` plus engine and
bounded shutdown), with the broker listeners, AI coordinator, durable recall and
independent original recorder composed explicitly. It adds no dispatcher, CLI
mode, installation mechanism or default proxy dependency.

Deployment supplies:

- an exclusively owned `brokerapp.Store` and `Broker`, with trusted caller tokens,
  exact allowed endpoints and activated account ACLs/credentials;
- a private local recall directory and `aihost.App` with explicit protocols,
  finite limits, tools/validators and an optional trusted `Select` callback;
- `Config.Routes`: complete, trusted `ai.Route` tuples (account, provider, model,
  endpoint, protocol, version). Several accounts may share an endpoint: the
  exact requested tuple is carried in a private context, not inferred from URL;
- `Config.Identity(ctx, subject)`: current authorization and stable trusted
  tenant/conversation ownership; never copy tenant/account headers into identity;
- `App.Recorder = &ai.HostRecorder{Sink: system.FileSink(privatePath)}`;
- broker catalog/enrollment listeners and service token in `Config.Host`.

For example, after constructing those deployment-owned values:

```go
host, err := system.New(broker, system.Config{
    Host: brokerapp.HostConfig{
        Catalog: catalogListener, Enrollment: enrollmentListener,
        ServiceToken: serviceToken,
    },
    App: app, Routes: trustedRoutes, Identity: currentIdentity,
})
if err != nil { return err }
// Serve host with an ordinary net/http.Server with deployment-specific read,
// header and idle timeouts. Stop that server before host.Close(shutdownContext).
```

Bind and bearer validation occur before `Identity` and AI ingress parsing. Every
managed call binds its exact encoding target, rechecks `Identity`, and invokes the
broker's current account/endpoint/ACL/refresh checks through `Input.Call`. The
broker's final route hook must see the same endpoint; it cannot silently replace
an encoded AI target with an unrelated account. Requested model and actual
response model remain distinct. The recorder uses request-local **actual broker
selection**, original host request/response snapshots and original body bytes.
The five-second required sink acknowledgement precedes logical success, tool
effects and final selected delivery.

Root request policies in `Host.Registrations` must be blocking `Prepare` hooks
with explicit `Before: []string{"ai-app.coordinate"}`. Other request stages are
rejected: they would silently miss the root's local-response short circuit.
The engine additionally validates IDs/dependencies and stage ordering. This is a
small deliberate composition contract, not a universal policy-order resolver.

Explicit selection disables broker automatic fallback. Use separate logical
managed calls for AI candidates/reroutes; no POST replay, private HTTP client or
transport-error retry is introduced. Default `brokerapp.NewHost` semantics are
unchanged when these opt-ins are absent. General nested multi-source selection
still fails closed; see [implementation evidence and limits](../../docs/implementation/system.md).

`FileSink` is a synchronous fsynced JSON-lines example for one process, not a
multi-host log, outbox, retention service or exactly-once effect system. The
operator owns its private directory, capacity and recovery. Keep configuration
immutable after construction. The real-socket/fresh-process fixtures are local
compatibility evidence, not live-provider/client certification.
