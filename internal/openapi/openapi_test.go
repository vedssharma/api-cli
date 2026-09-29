package openapi

import (
	"reflect"
	"strings"
	"testing"
)

const petstoreYAML = `
openapi: 3.0.3
info: {title: Pets}
servers:
  - url: https://{env}.pets.test/v1/
    variables:
      env: {default: api}
security:
  - bearerAuth: []
paths:
  /pets/{petId}:
    parameters:
      - name: petId
        in: path
        required: true
        schema: {type: string}
    get:
      operationId: getPet
      parameters:
        - {name: verbose, in: query, required: true}
        - {name: optional, in: query}
        - {name: X-Trace, in: header, required: true}
      responses:
        "404": {description: nope}
        "200": {description: ok}
  /pets:
    post:
      summary: Create pet
      security:
        - keyAuth: []
      requestBody:
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/NewPet'
      responses:
        "201": {description: created}
    delete:
      responses: {}
components:
  securitySchemes:
    bearerAuth: {type: http, scheme: bearer}
    keyAuth: {type: apiKey, in: query, name: api_key}
  schemas:
    Tag:
      type: object
      properties:
        id: {type: integer}
        parent: {$ref: '#/components/schemas/Tag'}
    NewPet:
      allOf:
        - type: object
          properties:
            name: {type: string}
            born: {type: string, format: date}
            tags:
              type: array
              items: {$ref: '#/components/schemas/Tag'}
        - type: object
          properties:
            kind: {type: string, enum: [cat, dog]}
`

func TestConvert_OpenAPI3YAML(t *testing.T) {
	res, err := Convert([]byte(petstoreYAML), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "Pets" || res.BaseURL != "https://api.pets.test/v1" {
		t.Errorf("title=%q base=%q", res.Title, res.BaseURL)
	}
	if len(res.Requests) != 3 {
		t.Fatalf("got %d requests", len(res.Requests))
	}

	// Sorted by path: /pets (post, delete), then /pets/{petId}
	post, del, get := res.Requests[0], res.Requests[1], res.Requests[2]

	if get.Name != "getPet" || get.Method != "GET" ||
		get.URL != "https://api.pets.test/v1/pets/{{petId}}?verbose={{verbose}}" {
		t.Errorf("get: %+v", get)
	}
	if get.Headers["Authorization"] != "Bearer {{token}}" || get.Headers["X-Trace"] != "{{X-Trace}}" {
		t.Errorf("get headers: %v", get.Headers)
	}
	if !reflect.DeepEqual(get.Assertions, []string{"status=200"}) {
		t.Errorf("get assertions: %v", get.Assertions)
	}

	if post.Name != "Create pet" || post.Method != "POST" ||
		post.URL != "https://api.pets.test/v1/pets?api_key={{apiKey}}" {
		t.Errorf("post: %+v", post)
	}
	if _, has := post.Headers["Authorization"]; has {
		t.Error("operation-level security should replace the global requirement")
	}
	if post.Headers["Content-Type"] != "application/json" {
		t.Errorf("post headers: %v", post.Headers)
	}
	// allOf merged, $ref resolved, date format, enum first value, recursion bounded
	for _, want := range []string{`"name":"string"`, `"born":"2024-01-01"`, `"kind":"cat"`, `"tags":[{"id":0,"parent":`} {
		if !strings.Contains(post.Body, want) {
			t.Errorf("body %s missing %s", post.Body, want)
		}
	}
	if !reflect.DeepEqual(post.Assertions, []string{"status=201"}) {
		t.Errorf("post assertions: %v", post.Assertions)
	}

	if del.Name != "DELETE /pets" || len(del.Assertions) != 0 {
		t.Errorf("delete: %+v", del)
	}
}

func TestConvert_BaseURLOverrideAndRelativeServer(t *testing.T) {
	doc := `{"openapi":"3.0.0","servers":[{"url":"/api"}],"paths":{"/x":{"get":{}}}}`
	res, err := Convert([]byte(doc), Options{})
	if err != nil || res.Requests[0].URL != "{{baseUrl}}/api/x" {
		t.Errorf("relative server: %+v %v", res, err)
	}
	res, _ = Convert([]byte(doc), Options{BaseURL: "http://localhost:8080"})
	if res.Requests[0].URL != "http://localhost:8080/x" {
		t.Errorf("override: %s", res.Requests[0].URL)
	}
	res, _ = Convert([]byte(`{"openapi":"3.0.0","paths":{"/x":{"get":{}}}}`), Options{})
	if res.Requests[0].URL != "{{baseUrl}}/x" {
		t.Errorf("no servers: %s", res.Requests[0].URL)
	}
}

func TestConvert_Swagger2(t *testing.T) {
	doc := `{
	  "swagger": "2.0", "host": "h.test", "basePath": "/v2", "schemes": ["http", "https"],
	  "securityDefinitions": {"k": {"type": "apiKey", "in": "header", "name": "X-Key"}},
	  "paths": {"/items/{id}": {"put": {
	    "security": [{"k": []}],
	    "parameters": [
	      {"name": "id", "in": "path", "required": true, "type": "string"},
	      {"name": "body", "in": "body", "schema": {"$ref": "#/definitions/Item"}}
	    ],
	    "responses": {"200": {}}
	  }}},
	  "definitions": {"Item": {"type": "object", "properties": {"n": {"type": "integer", "example": 5}}}}
	}`
	res, err := Convert([]byte(doc), Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := res.Requests[0]
	if r.URL != "https://h.test/v2/items/{{id}}" || r.Method != "PUT" || r.Headers["X-Key"] != "{{apiKey}}" ||
		r.Body != `{"n":5}` || r.Headers["Content-Type"] != "application/json" {
		t.Errorf("%+v", r)
	}
}

func TestConvert_Errors(t *testing.T) {
	for name, doc := range map[string]string{
		"not openapi":   `{"paths":{}}`,
		"bad version":   `{"openapi":"2.5","paths":{"/x":{"get":{}}}}`,
		"no operations": `{"openapi":"3.0.0","paths":{}}`,
		"garbage":       "::: not [valid",
		"yaml scalar":   "just text",
	} {
		if _, err := Convert([]byte(doc), Options{}); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestConvert_BadRefsDoNotPanic(t *testing.T) {
	doc := `{"openapi":"3.0.0","paths":{"/x":{"post":{
	  "parameters":[{"$ref":"#/components/parameters/Nope"}, {"$ref":"http://elsewhere/x.json#/p"}],
	  "requestBody":{"content":{"application/json":{"schema":{"$ref":"#/paths/~1x/post/parameters/0/name"}}}}
	}}}}`
	res, err := Convert([]byte(doc), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 {
		t.Error("expected warnings for unresolvable references")
	}
}

func TestSample_SelfReferenceTerminates(t *testing.T) {
	doc := `{"openapi":"3.0.0","components":{"schemas":{"N":{"type":"object","properties":{"next":{"$ref":"#/components/schemas/N"}}}}},
	"paths":{"/x":{"post":{"requestBody":{"content":{"application/json":{"schema":{"$ref":"#/components/schemas/N"}}}}}}}}`
	res, err := Convert([]byte(doc), Options{})
	if err != nil || !strings.HasPrefix(res.Requests[0].Body, `{"next":`) {
		t.Errorf("%+v %v", res, err)
	}
}

func TestConvert_UndeclaredPathParameter(t *testing.T) {
	doc := `{"openapi":"3.0.0","servers":[{"url":"http://h.test"}],"paths":{"/a/{x}/b/{y}":{"get":{}}}}`
	res, err := Convert([]byte(doc), Options{})
	if err != nil || res.Requests[0].URL != "http://h.test/a/{{x}}/b/{{y}}" {
		t.Errorf("%+v %v", res, err)
	}
}
