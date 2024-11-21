# print concise information for every failing test,
# uses the JSON output format from ginkgo, outputs flattened JSON
#
.[].SpecReports[]
| select(.State != "passed" and .State != "skipped")
| { Error: .Failure.Message,
    Test: .LeafNodeText,
    File: .Failure.Location.FileName,
    Line: .Failure.Location.LineNumber | tostring,
    Stack: .Failure.Location.FullStackTrace
}
