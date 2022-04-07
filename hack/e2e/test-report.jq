# jq returns an error code if there are any ginkgo failed test without the "ignore-fails" label.
# or if the report.json is malformed and does not contain test reports (e.g. because ginkgo panicked)
(
    # does this file contain actual ginkgo SpecReports?
    [
        .[].SpecReports[]
    ] | length | . != 0
)
and
(
    # are any of the specs failing without the ignore-fails flag?
    # note that failing states, as of ginkgo2, include: panicked, aborted, interrupted
    # better to flag anything that is not `passed` or `skipped`
    [
        .[].SpecReports[]
        | select(.State != "passed" and .State != "skipped")
        | select(.ContainerHierarchyLabels
            | flatten
            | any(. == "ignore-fails") 
            | not)
    ] | length | . == 0
)
