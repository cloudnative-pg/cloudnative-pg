# jq returns an error code if there are any ginkgo failed test without the "ignore-fails" label.
[
    .[].SpecReports[]
    | select(.State == "failed")
    | select(.ContainerHierarchyLabels
        | flatten
        | any(. == "ignore-fails") 
        | not)
] | length | . == 0
