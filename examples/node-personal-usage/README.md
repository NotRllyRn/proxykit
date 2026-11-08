# Personal usage worker example

A prebuilt dependency-free ESM extension. `extension.mjs` default-exports setup, registers a metadata-only observation and registers cleanup. Its `observe` stage is illustrative: the parent host bridge must map stage labels to its actual core contract before HTTP activation. Today this example can be run through `worker.New`/`Start`/`Invoke`, not attached to a serving proxy.

The root manifest is ready for inclusion in a GitHub repository archive. Installation requires explicit `--trust`, does not execute the entry and does not run any build/package scripts. Code executes only on worker startup. See `sdk/README.md` for typed authoring helpers and `docs/implementation/distribution.md` for the public process protocol and remaining integration work.
