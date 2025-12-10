{{- define "gvList" -}}
{{- $groupVersions := . -}}
---
id: cloudnative-pg.v1
sidebar_position: 550
title: API Reference
---

# API Reference

## Packages

{{- range $groupVersions }}
- {{ markdownRenderGVLink . }}
{{- end }}

{{ range $groupVersions }}
{{ template "gvDetails" . }}
{{ end }}

{{- end -}}
