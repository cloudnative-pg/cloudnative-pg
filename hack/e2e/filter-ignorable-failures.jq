# print concise information for every failing test on "ignore-fails"
# uses the JSON output format from ginkgo, outputs flattened JSON
#
.[].SpecReports[]
| select(.State == "failed")
# skip failed tests with an IgnoreFails label
| select(.ContainerHierarchyLabels
        | flatten
        | any(. == "ignore-fails"))
| { Error: .Failure.Message,
    Test: .LeafNodeText,
    File: .Failure.Location.FileName,
    Line: .Failure.Location.LineNumber | tostring,
    Stack: .Failure.Location.FullStackTrace
}
