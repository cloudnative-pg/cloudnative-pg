{{ define "packages" -}}
# API Reference
<!-- SPDX-License-Identifier: CC-BY-4.0 -->

{{ $grpname := "" -}}
{{- range $idx, $val := .packages -}}
  {{- if and (ne .GroupName "") (eq $grpname "") -}}
{{ .GetComment -}}
{{- $grpname = .GroupName -}}
  {{- end -}}
{{- end }}

## Resource Types

{{ range .packages -}}
  {{- /* if ne .GroupName "" */ -}}
    {{- range .VisibleTypes -}}
      {{- if .IsExported }}
- [{{ .DisplayName }}]({{ .Link }})
      {{- end -}}
    {{- end -}}
  {{- /* end */ -}}
{{- end -}}

{{- range .packages -}}
  {{- if ne .GroupName "" -}}

    {{- /* For package with a group name, list all type definitions in it. */ -}}
    {{- range .VisibleTypes -}}
      {{- if or .Referenced .IsExported -}}
{{ template "type" . }}
      {{- end -}}
    {{- end }}
  {{- else -}}
    {{- /* For package w/o group name, list only types referenced. */ -}}
    {{- range .VisibleTypes -}}
      {{- if .Referenced -}}
{{ template "type" . }}
      {{- end -}}
    {{- end }}
  {{- end }}
{{- end }}
{{- end }}
