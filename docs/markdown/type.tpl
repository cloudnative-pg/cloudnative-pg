{{ define "type" }}

## {{ .Name.Name }}     {#{{ .Anchor }}}

{{ if eq .Kind "Alias" -}}
(Alias of `{{ .Underlying }}`)
{{ end }}

{{- with .References }}
**Appears in:**
{{ range . }}
{{ if or .Referenced .IsExported -}}
- [{{ .DisplayName }}]({{ .Link }})
{{ end -}}
{{- end -}}
{{- end }}

{{ if .GetComment -}}
{{ .GetComment }}
{{ end }}
{{ if .GetMembers -}}
<table class="table">
<thead><tr><th width="30%">Field</th><th>Description</th></tr></thead>
<tbody>
    {{- /* . is a apiType */ -}}
    {{- if .IsExported -}}
{{- /* Add apiVersion and kind rows if deemed necessary */}}
<tr><td><code>apiVersion</code> <B>[Required]</B><br/>string</td><td><code>{{- .APIGroup -}}</code></td></tr>
<tr><td><code>kind</code> <B>[Required]</B><br/>string</td><td><code>{{- .Name.Name -}}</code></td></tr>
    {{- end -}}

{{/* The actual list of members is in the following template */}}
{{- template "members" . -}}
</tbody>
</table>
{{- end -}}
{{- end -}}
