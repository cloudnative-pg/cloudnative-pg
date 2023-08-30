{{ define "members" }}
  {{- /* . is a apiType */ -}}
  {{- range .GetMembers -}}
    {{- /* . is a apiMember */ -}}
    {{- if not .Hidden }}
<tr><td><code>{{ .FieldName }}</code>
      {{- if and (not .IsOptional) (not .IsInline) }} <B>[Required]</B>{{- end -}}
<br/>
{{/* Link for type reference */}}
      {{- with .GetType -}}
        {{- if .Link -}}
<a href="{{ .Link }}"><i>{{ .DisplayName }}</i></a>
        {{- else -}}
<i>{{ .DisplayName }}</i>
        {{- end -}}
      {{- end }}
</td>
<td>
   {{- if .IsInline -}}
(Members of <code>{{ .FieldName }}</code> are embedded into this type.)
   {{- end }}
   {{ if .GetComment -}}
   {{ .GetComment }}
   {{- else -}}
   <span class="text-muted">No description provided.</span>
   {{- end }}
   {{- if and (eq (.GetType.Name.Name) "ObjectMeta") -}}
Refer to the Kubernetes API documentation for the fields of the <code>metadata</code> field.
   {{- end -}}
</td>
</tr>
    {{- end }}
  {{- end }}
{{ end }}
