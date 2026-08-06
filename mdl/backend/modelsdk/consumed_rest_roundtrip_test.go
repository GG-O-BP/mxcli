// SPDX-License-Identifier: Apache-2.0

package modelsdkbackend

import (
	"testing"

	"github.com/mendixlabs/mxcli/model"
)

// TestConsumedRestOperation_DetailRoundTrip covers the two halves of #843.
//
// Writing: an operation whose response is an implicit mapping must serialize to
// Rest$ImplicitMappingResponseHandling. It used to fall through to
// Rest$NoResponseHandling because the writer compared ResponseType against the
// uppercase "MAPPING" documented on model.RestClientOperation while the MDL
// executor stored the visitor's lowercase "mapping" — so every response mapping
// authored in MDL was dropped without a warning.
//
// Reading: restOperationFromGen only populated Name/HttpMethod/Path/Timeout, so
// `describe rest client` printed neither the query parameters nor the response
// even when both were stored correctly. The pre-existing round-trip test built
// an operation with a query parameter but never asserted it survived, which is
// why the gap went unnoticed.
func TestConsumedRestOperation_DetailRoundTrip(t *testing.T) {
	proj := copyFixture(t)
	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = b.Disconnect() })

	mod, err := b.GetModuleByName("MyFirstModule")
	if err != nil || mod == nil {
		t.Fatalf("GetModuleByName: %v", err)
	}

	svc := &model.ConsumedRestService{
		ContainerID: mod.ID,
		Name:        "ZzDetailClient",
		BaseUrl:     "https://api.example.com",
		Operations: []*model.RestClientOperation{{
			Name:       "GetRoute",
			HttpMethod: "GET",
			Path:       "/routes/{id}",
			Timeout:    300,
			Parameters: []*model.RestClientParameter{
				{Name: "id", DataType: "String"},
			},
			QueryParameters: []*model.RestClientParameter{
				{Name: "page", DataType: "Integer"},
				{Name: "pageSize", DataType: "Integer"},
			},
			Headers: []*model.RestClientHeader{
				{Name: "X-Tenant-Id", Value: "demo-tenant-01"},
			},
			ResponseType:   "MAPPING",
			ResponseEntity: "MyFirstModule.Routing",
			ResponseMappings: []*model.RestResponseMapping{
				{Attribute: "RoutingCode", ExposedName: "routing_code"},
			},
		}},
	}
	if err := b.CreateConsumedRestService(svc); err != nil {
		t.Fatalf("CreateConsumedRestService: %v", err)
	}

	b2 := New()
	if err := b2.Connect(proj); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	t.Cleanup(func() { _ = b2.Disconnect() })

	all, err := b2.ListConsumedRestServices()
	if err != nil {
		t.Fatalf("ListConsumedRestServices: %v", err)
	}
	var op *model.RestClientOperation
	for _, s := range all {
		if s.Name == "ZzDetailClient" && len(s.Operations) == 1 {
			op = s.Operations[0]
		}
	}
	if op == nil {
		t.Fatalf("ZzDetailClient/GetRoute not found after create")
	}

	// Path parameters: writer emits Rest$OperationParameter, so a reader that
	// type-asserts Rest$RestParameter silently drops every one of them.
	if len(op.Parameters) != 1 {
		t.Fatalf("Parameters = %d, want 1", len(op.Parameters))
	}
	if op.Parameters[0].Name != "id" {
		t.Errorf("Parameters[0].Name = %q, want %q", op.Parameters[0].Name, "id")
	}
	if op.Parameters[0].DataType != "String" {
		t.Errorf("Parameters[0].DataType = %q, want %q", op.Parameters[0].DataType, "String")
	}

	// Query parameters: writer emits Rest$QueryParameter — a different gen type
	// again. Mendix stores no DataType for these, so only the name round-trips.
	if len(op.QueryParameters) != 2 {
		t.Fatalf("QueryParameters = %d, want 2", len(op.QueryParameters))
	}
	if op.QueryParameters[0].Name != "page" || op.QueryParameters[1].Name != "pageSize" {
		t.Errorf("QueryParameters = %q/%q, want page/pageSize",
			op.QueryParameters[0].Name, op.QueryParameters[1].Name)
	}

	// Headers: the writer appends an Accept header of its own, so assert on the
	// authored one rather than the slice length.
	var tenant *model.RestClientHeader
	for _, h := range op.Headers {
		if h.Name == "X-Tenant-Id" {
			tenant = h
		}
	}
	if tenant == nil {
		t.Fatalf("X-Tenant-Id header not round-tripped (got %d headers)", len(op.Headers))
	}
	if tenant.Value != "demo-tenant-01" {
		t.Errorf("X-Tenant-Id = %q, want %q", tenant.Value, "demo-tenant-01")
	}

	// The response mapping itself — the silent data loss reported in #843.
	if op.ResponseType != "MAPPING" {
		t.Fatalf("ResponseType = %q, want MAPPING (response handling stored as NoResponseHandling?)", op.ResponseType)
	}
	if len(op.ResponseMappings) != 1 {
		t.Fatalf("ResponseMappings = %d, want 1", len(op.ResponseMappings))
	}
	if got := op.ResponseMappings[0]; got.Attribute != "RoutingCode" || got.ExposedName != "routing_code" {
		t.Errorf("ResponseMappings[0] = %+v, want RoutingCode = routing_code", got)
	}
}

// TestConsumedRestOperation_LowercaseResponseTypeStillMaps pins the specific
// regression: the MDL executor's lowercase "mapping" must produce the same
// implicit-mapping response handling as the documented uppercase "MAPPING".
// Without normalization the two disagree and the lowercase form loses the
// mapping — which is exactly what the reporter hit.
func TestConsumedRestOperation_LowercaseResponseTypeStillMaps(t *testing.T) {
	proj := copyFixture(t)
	b := New()
	if err := b.Connect(proj); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = b.Disconnect() })

	mod, err := b.GetModuleByName("MyFirstModule")
	if err != nil || mod == nil {
		t.Fatalf("GetModuleByName: %v", err)
	}

	svc := &model.ConsumedRestService{
		ContainerID: mod.ID,
		Name:        "ZzLowerClient",
		BaseUrl:     "https://api.example.com",
		Operations: []*model.RestClientOperation{{
			Name:           "GetRoute",
			HttpMethod:     "GET",
			Path:           "/routes",
			ResponseType:   "mapping",
			ResponseEntity: "MyFirstModule.Routing",
			ResponseMappings: []*model.RestResponseMapping{
				{Attribute: "RoutingCode", ExposedName: "routing_code"},
			},
		}},
	}
	if err := b.CreateConsumedRestService(svc); err != nil {
		t.Fatalf("CreateConsumedRestService: %v", err)
	}

	b2 := New()
	if err := b2.Connect(proj); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	t.Cleanup(func() { _ = b2.Disconnect() })

	all, err := b2.ListConsumedRestServices()
	if err != nil {
		t.Fatalf("ListConsumedRestServices: %v", err)
	}
	for _, s := range all {
		if s.Name != "ZzLowerClient" {
			continue
		}
		if len(s.Operations) != 1 {
			t.Fatalf("operations = %d, want 1", len(s.Operations))
		}
		if len(s.Operations[0].ResponseMappings) != 1 {
			t.Fatalf("lowercase %q lost the response mapping", "mapping")
		}
		return
	}
	t.Fatalf("ZzLowerClient not found after create")
}
