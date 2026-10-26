package docs

import _ "embed"

// OpenAPI is served by the API so the browser documentation matches this build.
//go:embed openapi.yaml
var OpenAPI []byte
