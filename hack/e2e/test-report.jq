# jq returns an error code if there are any ginkgo failed tests
# or if the report.json is malformed and does not contain test reports (e.g. because ginkgo panicked)
(
    # does this file contain actual ginkgo SpecReports?
    [
        .[].SpecReports[]
    ] | length | . != 0
)
and
(
    # are any of the specs failing?
    # note that failing states, as of ginkgo2, include: panicked, aborted, interrupted
    # better to flag anything that is not `passed`, `skipped`, or `pending`
    # pending tests (PIt) are intentionally deferred and should not count as failures
    [
        .[].SpecReports[]
        | select(.State != "passed" and .State != "skipped" and .State != "pending")
    ] | length | . == 0
)
