// SPDX-License-Identifier: Apache-2.0

package openapi

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ============================================================================
// Minimal OpenAPI 3.0 types
// ============================================================================

// Spec is a minimal OpenAPI 3.0 representation covering fields needed for REST client generation.
type Spec struct {
	Info       Info                  `json:"info"       yaml:"info"`
	Servers    []Server              `json:"servers"    yaml:"servers"`
	Paths      map[string]PathItem   `json:"paths"      yaml:"paths"`
	Components Components            `json:"components" yaml:"components"`
	Security   []map[string][]string `json:"security"   yaml:"security"`
}

// Info holds the spec's title and description.
type Info struct {
	Title       string `json:"title"       yaml:"title"`
	Description string `json:"description" yaml:"description"`
	Version     string `json:"version"     yaml:"version"`
}

// Server represents one entry in the servers array.
type Server struct {
	URL         string `json:"url"         yaml:"url"`
	Description string `json:"description" yaml:"description"`
}

// PathItem holds the operations for a single path.
type PathItem struct {
	Get     *Operation `json:"get"     yaml:"get"`
	Post    *Operation `json:"post"    yaml:"post"`
	Put     *Operation `json:"put"     yaml:"put"`
	Patch   *Operation `json:"patch"   yaml:"patch"`
	Delete  *Operation `json:"delete"  yaml:"delete"`
	Head    *Operation `json:"head"    yaml:"head"`
	Options *Operation `json:"options" yaml:"options"`
	// Path-level parameters to be merged into each operation
	Parameters []Parameter `json:"parameters" yaml:"parameters"`
}

// Operation represents a single HTTP operation.
type Operation struct {
	OperationID string                `json:"operationId" yaml:"operationId"`
	Summary     string                `json:"summary"     yaml:"summary"`
	Description string                `json:"description" yaml:"description"`
	Parameters  []Parameter           `json:"parameters"  yaml:"parameters"`
	RequestBody *RequestBody          `json:"requestBody" yaml:"requestBody"`
	Responses   map[string]Response   `json:"responses"   yaml:"responses"`
	Security    []map[string][]string `json:"security"  yaml:"security"`
	Tags        []string              `json:"tags"        yaml:"tags"`
}

// Parameter represents an OpenAPI parameter (path, query, header, cookie).
type Parameter struct {
	Name     string  `json:"name"     yaml:"name"`
	In       string  `json:"in"       yaml:"in"` // "path", "query", "header", "cookie"
	Required bool    `json:"required" yaml:"required"`
	Schema   *Schema `json:"schema"   yaml:"schema"`
}

// RequestBody represents the request body of an operation.
type RequestBody struct {
	Required bool                 `json:"required" yaml:"required"`
	Content  map[string]MediaType `json:"content"  yaml:"content"`
}

// Response represents an API response.
type Response struct {
	Description string               `json:"description" yaml:"description"`
	Content     map[string]MediaType `json:"content"     yaml:"content"`
}

// MediaType represents a media type entry in content.
type MediaType struct {
	Schema *Schema `json:"schema" yaml:"schema"`
}

// Schema is a minimal JSON Schema representation.
type Schema struct {
	Type   string `json:"type"   yaml:"type"`
	Format string `json:"format" yaml:"format"`
}

// Components holds reusable components (security schemes, etc.).
type Components struct {
	SecuritySchemes map[string]SecurityScheme `json:"securitySchemes" yaml:"securitySchemes"`
}

// SecurityScheme describes an authentication mechanism.
type SecurityScheme struct {
	Type   string `json:"type"   yaml:"type"`   // "http", "apiKey", "oauth2", "openIdConnect"
	Scheme string `json:"scheme" yaml:"scheme"` // for type=http: "basic", "bearer"
	In     string `json:"in"     yaml:"in"`     // for apiKey: "header", "query", "cookie"
	Name   string `json:"name"   yaml:"name"`   // for apiKey: header/query parameter name
}

// ============================================================================
// Parsing
// ============================================================================

// ParseSpec parses an OpenAPI spec from bytes. The format is auto-detected by ext
// (".json" → JSON, anything else → YAML).
func ParseSpec(data []byte, ext string) (*Spec, error) {
	var spec Spec
	if strings.ToLower(ext) == ".json" {
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("JSON parse error: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("YAML parse error: %w", err)
		}
	}
	return &spec, nil
}

// ParseSpecFromURL parses an already-fetched spec. The URL is used only to
// determine the format (JSON vs YAML) from its extension.
func ParseSpecFromURL(data []byte, rawURL string) (*Spec, error) {
	// Strip query strings before extracting extension
	base := rawURL
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		base = rawURL[:i]
	}
	ext := strings.ToLower(filepath.Ext(base))
	if ext == "" {
		// Default to JSON for bare URLs (e.g., /openapi)
		ext = ".json"
	}
	return ParseSpec(data, ext)
}

// ============================================================================
// Conversion: Spec → model.ConsumedRestService
// ============================================================================

var nonAlphaNum = regexp.MustCompile(`[^A-Za-z0-9]+`)

// sanitizeIdent converts an arbitrary string to a valid MDL identifier.
// Non-alphanumeric runs are replaced with underscores; leading/trailing
// underscores are trimmed.
func sanitizeIdent(s string) string {
	s = nonAlphaNum.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}

// ConsumedRestService is a minimal view of the service returned by ToRestClientModel.
// It avoids importing the full model package into this package.
type ConsumedRestService struct {
	Name           string
	Documentation  string
	BaseUrl        string
	Authentication *RestAuthentication
	Operations     []*RestClientOperation
}

// RestAuthentication holds the scheme for service-level authentication.
type RestAuthentication struct {
	Scheme string // "Basic"
}

// RestClientOperation represents one generated operation.
type RestClientOperation struct {
	Name            string
	Documentation   string
	HttpMethod      string
	Path            string
	Tags            []string // from OpenAPI operation.tags; used as Studio Pro resource group labels
	Parameters      []*RestClientParameter
	QueryParameters []*RestClientParameter
	Headers         []*RestClientHeader
	BodyType        string
	BodyVariable    string
	ResponseType    string
}

// RestClientParameter represents a path or query parameter.
type RestClientParameter struct {
	Name     string
	DataType string
}

// RestClientHeader represents an HTTP header entry.
type RestClientHeader struct {
	Name  string
	Value string
}

// ToRestClientModel converts a parsed OpenAPI spec to a ConsumedRestService.
// serviceName is the name to assign (not module-qualified). baseUrlOverride replaces servers[0].url when non-empty.
// Returns the service, a list of warnings, and any fatal error.
func ToRestClientModel(spec *Spec, serviceName string, baseUrlOverride string) (*ConsumedRestService, []string, error) {
	svc := &ConsumedRestService{
		Name: serviceName,
	}
	var warnings []string

	// BaseUrl — only use absolute URLs from the spec; relative ones (e.g. "/api/v3")
	// are rejected by Studio Pro CE7247 and must be overridden by the caller.
	baseURL := baseUrlOverride
	if baseURL == "" && len(spec.Servers) > 0 {
		u := spec.Servers[0].URL
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			baseURL = u
		} else {
			warnings = append(warnings, fmt.Sprintf("server URL %q is relative and cannot be used as BaseUrl; set BaseUrl explicitly in CREATE REST CLIENT", u))
		}
	}
	svc.BaseUrl = baseURL

	// Authentication from top-level security + securitySchemes
	svc.Authentication, warnings = resolveAuthentication(spec, warnings)

	// Doc comment from info
	if spec.Info.Title != "" || spec.Info.Description != "" {
		parts := []string{}
		if spec.Info.Title != "" {
			parts = append(parts, spec.Info.Title)
		}
		if spec.Info.Description != "" {
			parts = append(parts, spec.Info.Description)
		}
		svc.Documentation = strings.Join(parts, "\n")
	}

	// Operations — iterate paths in sorted order for determinism
	paths := make([]string, 0, len(spec.Paths))
	for p := range spec.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Track used operation names to handle collisions
	usedNames := map[string]int{}

	methodOrder := []string{"get", "post", "put", "patch", "delete", "head", "options"}
	for _, path := range paths {
		item := spec.Paths[path]
		for _, method := range methodOrder {
			op := operationForMethod(item, method)
			if op == nil {
				continue
			}

			// Merge path-level parameters into operation-level (op-level wins)
			mergedParams := mergeParams(item.Parameters, op.Parameters)

			clientOp, opWarnings := convertOperation(path, strings.ToUpper(method), op, mergedParams, &usedNames)
			warnings = append(warnings, opWarnings...)
			svc.Operations = append(svc.Operations, clientOp)
		}
	}

	return svc, warnings, nil
}

// resolveAuthentication picks the first supported authentication scheme.
func resolveAuthentication(spec *Spec, warnings []string) (*RestAuthentication, []string) {
	if len(spec.Security) == 0 || len(spec.Components.SecuritySchemes) == 0 {
		return nil, warnings
	}
	for _, secReq := range spec.Security {
		for schemeName := range secReq {
			scheme, ok := spec.Components.SecuritySchemes[schemeName]
			if !ok {
				continue
			}
			switch scheme.Type {
			case "http":
				if strings.EqualFold(scheme.Scheme, "basic") {
					return &RestAuthentication{Scheme: "Basic"}, warnings
				}
				warnings = append(warnings, fmt.Sprintf("unsupported HTTP auth scheme '%s' (only basic is supported; set manually)", scheme.Scheme))
			case "apiKey":
				warnings = append(warnings, fmt.Sprintf("apiKey scheme '%s' (in: %s, name: %s) must be set dynamically in calling microflow", schemeName, scheme.In, scheme.Name))
			case "oauth2", "openIdConnect":
				warnings = append(warnings, fmt.Sprintf("OAuth2/OpenIdConnect scheme '%s' is not natively supported; set authentication manually", schemeName))
			}
		}
	}
	return nil, warnings
}

// mergeParams merges path-level and operation-level parameters.
// Operation-level parameters override path-level ones with the same name+in.
func mergeParams(pathParams, opParams []Parameter) []Parameter {
	result := make([]Parameter, 0, len(pathParams)+len(opParams))
	// Start with path-level params
	result = append(result, pathParams...)
	// Override/add op-level params
	for _, op := range opParams {
		replaced := false
		for i, pp := range result {
			if pp.Name == op.Name && pp.In == op.In {
				result[i] = op
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, op)
		}
	}
	return result
}

// convertOperation converts a single OpenAPI operation to a RestClientOperation.
func convertOperation(path, method string, op *Operation, params []Parameter, usedNames *map[string]int) (*RestClientOperation, []string) {
	var warnings []string

	// Derive the MDL operation name
	name := op.OperationID
	if name == "" {
		name = strings.ToLower(method) + "_" + sanitizeIdent(path)
	} else {
		name = sanitizeIdent(name)
	}
	if name == "" {
		name = strings.ToLower(method) + "_operation"
	}

	// Collision resolution: append numeric suffix on duplicates
	if count, exists := (*usedNames)[name]; exists {
		(*usedNames)[name] = count + 1
		name = fmt.Sprintf("%s_%d", name, count+1)
	} else {
		(*usedNames)[name] = 1
	}

	clientOp := &RestClientOperation{
		Name:       name,
		HttpMethod: method,
		Path:       path,
		Tags:       op.Tags,
	}

	// Documentation
	docs := []string{}
	if op.Summary != "" {
		docs = append(docs, op.Summary)
	}
	if op.Description != "" && op.Description != op.Summary {
		docs = append(docs, op.Description)
	}
	if len(docs) > 0 {
		clientOp.Documentation = strings.Join(docs, "\n")
	}

	// Parameters (path and query)
	for _, p := range params {
		mdlType := schemaToMDLType(p.Schema)
		switch p.In {
		case "path":
			clientOp.Parameters = append(clientOp.Parameters, &RestClientParameter{
				Name:     p.Name,
				DataType: mdlType,
			})
		case "query":
			clientOp.QueryParameters = append(clientOp.QueryParameters, &RestClientParameter{
				Name:     p.Name,
				DataType: mdlType,
			})
		case "header":
			clientOp.Headers = append(clientOp.Headers, &RestClientHeader{
				Name:  p.Name,
				Value: "",
			})
		}
	}

	// Request body
	if op.RequestBody != nil {
		if _, hasJSON := op.RequestBody.Content["application/json"]; hasJSON {
			clientOp.BodyType = "JSON"
			clientOp.BodyVariable = "$body"
		} else {
			clientOp.BodyType = "TEMPLATE"
			clientOp.BodyVariable = ""
			warnings = append(warnings, fmt.Sprintf("operation %s: non-JSON request body; set body type manually", name))
		}
	}

	// Response type from 200/201/204
	clientOp.ResponseType = resolveResponseType(op, name, &warnings)

	return clientOp, warnings
}

// resolveResponseType picks the best ResponseType from an operation's responses.
func resolveResponseType(op *Operation, opName string, warnings *[]string) string {
	// Prefer 200, then 201, then 204, then first 2xx
	for _, code := range []string{"200", "201", "204"} {
		resp, ok := op.Responses[code]
		if !ok {
			continue
		}
		if code == "204" || len(resp.Content) == 0 {
			return "NONE"
		}
		if _, hasJSON := resp.Content["application/json"]; hasJSON {
			return "JSON"
		}
		if _, hasText := resp.Content["text/plain"]; hasText {
			return "STRING"
		}
		if _, hasOctet := resp.Content["application/octet-stream"]; hasOctet {
			return "FILE"
		}
		*warnings = append(*warnings, fmt.Sprintf("operation %s: unrecognized response content type; defaulting to STRING", opName))
		return "STRING"
	}
	// No recognised success response
	return "NONE"
}

// schemaToMDLType converts an OpenAPI schema type+format to an MDL data type name.
func schemaToMDLType(schema *Schema) string {
	if schema == nil {
		return "String"
	}
	switch schema.Type {
	case "integer":
		if schema.Format == "int64" {
			return "Long"
		}
		return "Integer"
	case "number":
		return "Decimal"
	case "boolean":
		return "Boolean"
	default:
		return "String"
	}
}

// operationForMethod returns the operation for the given lowercase HTTP method.
func operationForMethod(item PathItem, method string) *Operation {
	switch method {
	case "get":
		return item.Get
	case "post":
		return item.Post
	case "put":
		return item.Put
	case "patch":
		return item.Patch
	case "delete":
		return item.Delete
	case "head":
		return item.Head
	case "options":
		return item.Options
	}
	return nil
}
