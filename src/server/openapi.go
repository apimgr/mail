package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/apimgr/mail/src/common/api"
)

func (s *Server) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	paths := make(map[string]any)
	for _, route := range s.apiRouteRegistry() {
		operation := openAPIOperation(route)
		canonical := strings.Replace(route.Path, "{api_version}", api.Version, 1)
		addOpenAPIOperation(paths, canonical, route.Method, operation)
		for _, alias := range route.Aliases {
			aliasOperation := make(map[string]any, len(operation))
			for key, value := range operation {
				aliasOperation[key] = value
			}
			aliasOperation["operationId"] = route.OperationID + "Alias"
			addOpenAPIOperation(paths, alias, route.Method, aliasOperation)
		}
	}
	spec := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Mail API",
			"version":     api.Version,
			"description": "Mail management API. The specification contains only routes currently registered by this server.",
		},
		"servers": []any{map[string]any{"url": s.buildPublicURL(r, "")}},
		"paths":   paths,
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"BearerAuth": map[string]any{
					"type": "http", "scheme": "bearer",
					"bearerFormat": "operator or owner token",
					"description":  "A bearer token accepted by the route's declared authorization tier.",
				},
			},
		},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(spec)
}

func openAPIOperation(route Route) map[string]any {
	successStatus := route.SuccessStatus
	if successStatus == 0 {
		successStatus = http.StatusOK
	}
	successContentType := "application/json"
	successSchema := openAPISchema(route.Response)
	if strings.HasSuffix(route.Path, ".txt") {
		successContentType = "text/plain"
		successSchema = map[string]any{"type": "string"}
	}
	responses := map[string]any{
		strconv.Itoa(successStatus): map[string]any{
			"description": "Successful response",
			"content":     map[string]any{successContentType: map[string]any{"schema": successSchema}},
		},
	}
	for code, description := range map[int]string{
		http.StatusBadRequest:          "Invalid request",
		http.StatusUnauthorized:        "Invalid or missing credentials",
		http.StatusForbidden:           "The credential lacks permission for this operation",
		http.StatusNotFound:            "The requested resource was not found",
		http.StatusConflict:            "The request conflicts with current resource state",
		http.StatusTooManyRequests:     "The request limit was exceeded",
		http.StatusInternalServerError: "Internal server error",
		http.StatusServiceUnavailable:  "A required service is unavailable",
	} {
		responses[strconv.Itoa(code)] = map[string]any{"description": description}
	}
	operation := map[string]any{
		"operationId": route.OperationID,
		"summary":     route.Summary,
		"responses":   responses,
	}
	if route.Auth != "none" {
		operation["security"] = []any{map[string]any{"BearerAuth": []string{}}}
	}
	if route.Request != nil {
		requestContentType := route.RequestContentType
		if requestContentType == "" {
			requestContentType = "application/json"
		}
		operation["requestBody"] = map[string]any{
			"required": true,
			"content":  map[string]any{requestContentType: map[string]any{"schema": openAPISchema(route.Request)}},
		}
	}
	parameters := make([]any, 0, len(route.Parameters))
	if route.Path != "" && strings.Contains(route.Path, "{") {
		for _, segment := range strings.Split(route.Path, "/") {
			if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
				name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
				if name != "api_version" {
					parameters = append(parameters, map[string]any{
						"name": name, "in": "path", "required": true,
						"schema": map[string]any{"type": "string"},
					})
				}
			}
		}
	}
	for _, parameter := range route.Parameters {
		schema := map[string]any{"type": parameter.Type}
		if parameter.Default != nil {
			schema["default"] = parameter.Default
		}
		parameters = append(parameters, map[string]any{
			"name": parameter.Name, "in": parameter.In, "required": parameter.Required,
			"description": parameter.Description, "schema": schema,
		})
	}
	if len(parameters) > 0 {
		operation["parameters"] = parameters
	}
	return operation
}

func openAPISchema(value any) map[string]any {
	if value == nil {
		return map[string]any{"type": "object", "additionalProperties": true}
	}
	return schemaForType(reflect.TypeOf(value))
}

func schemaForType(value reflect.Type) map[string]any {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Array, reflect.Slice:
		return map[string]any{"type": "array", "items": schemaForType(value.Elem())}
	case reflect.Map, reflect.Interface:
		return map[string]any{"type": "object", "additionalProperties": true}
	case reflect.Struct:
		properties := make(map[string]any)
		required := make([]string, 0)
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.PkgPath != "" {
				continue
			}
			tag := field.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			properties[name] = schemaForType(field.Type)
			if !strings.Contains(tag, "omitempty") && field.Type.Kind() != reflect.Pointer && field.Type.Kind() != reflect.Interface {
				required = append(required, name)
			}
		}
		schema := map[string]any{"type": "object", "properties": properties}
		if len(required) > 0 {
			schema["required"] = required
		}
		return schema
	default:
		return map[string]any{}
	}
}

func addOpenAPIOperation(paths map[string]any, path, method string, operation map[string]any) {
	pathItem, ok := paths[path].(map[string]any)
	if !ok {
		pathItem = make(map[string]any)
		paths[path] = pathItem
	}
	pathItem[strings.ToLower(method)] = operation
}
