package handler

import (
	"net/http"

	"github.com/rudast/alpha-hack-2026/docs"
)

const apiDocsPage = `<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Документация API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.33.3/swagger-ui.css">
</head>
<body>
  <p>Если интерфейс не загрузился, откройте <a href="/openapi.yaml">OpenAPI YAML</a>.</p>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.33.3/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function () {
      SwaggerUIBundle({ url: "/openapi.yaml", dom_id: "#swagger-ui", deepLinking: true });
    };
  </script>
  <noscript>Для просмотра документации нужен JavaScript. <a href="/openapi.yaml">Открыть OpenAPI YAML</a>.</noscript>
</body>
</html>`

func openAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(docs.OpenAPI)
}

func apiDocs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(apiDocsPage))
}
