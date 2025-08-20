apiVersion: v1
kind: Secret
metadata:
  name: {{ .Name }}
  namespace: default
type: Opaque
stringData:
  {{- $root := . }}
  {{- range $key, $value := .Data }}
  {{ $key }}: |
{{ call $root.Indent 4 $value }}
  {{- end }}
