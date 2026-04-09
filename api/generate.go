package thunderstormmock

//go:generate curl -sfL -o openapi.yaml https://github.com/NextronSystems/thunderstorm-openapi/releases/download/v1.0.0/openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config cfg.yaml openapi.yaml
