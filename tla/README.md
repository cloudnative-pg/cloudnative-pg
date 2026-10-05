# TLA+ Specifications

Formal models of CloudNativePG mechanisms, checked with TLC.
Layout: one folder per model holding `<Model>.tla` (PlusCal source plus
its translation), `<Model>.cfg`, and a `README.md` documenting that
model; the `Makefile` and `.gitignore` at this level apply to every
model.

## Models

* [`primary-lease`](primary-lease/) — safe primary election: what happens
  after a CloudNativePG primary loses its lease when fail-safe mode is
  active (non-cooperative old primary).

## Files

* `Makefile` — `check` / `translate` / `clean` targets covering every model.
* `.gitignore` — TLC output (`states/`) and translator backups (`*.old`).

## How to check

Preferred entry point, from `tla/` (ensures a Java runtime is present,
fetches `tla2tools.jar` from the pinned TLA+ release when missing,
translates PlusCal if needed, then checks every model with
`-workers auto`):

```sh
make check
```

Changing a PlusCal algorithm requires re-translation before
re-checking. See the model folders for per-model details and how to
run a single spec manually or from the VS Code TLA+ extension.

## Adding a new model

Add a folder holding `<Model>.tla` and `<Model>.cfg` (module name must
match the file name) plus a `README.md` documenting it. The `Makefile`
discovers it automatically — no registration needed; link it under
`## Models` above.
