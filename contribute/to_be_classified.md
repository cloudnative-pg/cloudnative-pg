# Content to be classified

This is work in progress.

## How to upgrade the list of licenses

To generate or update the `licenses` folder run the following command:

```bash
make licenses
```

---

## Release procedure

### Initial verification

- Make sure release notes for the release have been updated
  in `docs/src/release_notes.md` and have been approved by
  the maintainers
- Make sure that the operator capability levels page has been
  updated in `docs/src/operator_capability_levels.md` and approved
  by the maintainers

### Release steps

The following steps assume version 1.15.0 as the one to be released. Alter the
instructions accordingly for your version.

1. Run `hack/release.sh 1.15.0`.
2. Approve the PR that is automatically generated.
3. Merge the PR.
4. Wait until all [Github Actions](https://github.com/cloudnative-pg/cloudnative-pg/actions) finish.

---

