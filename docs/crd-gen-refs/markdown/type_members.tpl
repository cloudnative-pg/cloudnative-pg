{{- define "type_members" -}}
{{- $field := . -}}
{{- if eq $field.Name "metadata" -}}
Refer to Kubernetes API documentation for fields of `metadata`.
{{- else -}}
{{ markdownRenderFieldDoc $field.Doc }}
{{- end -}}
{{- end -}}
