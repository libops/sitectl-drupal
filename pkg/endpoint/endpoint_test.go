package endpoint

import "testing"

func TestJSONAPIResolvesNamedDrupalRoute(t *testing.T) {
	t.Parallel()

	got, err := JSONAPI(nil, nil)
	if err != nil {
		t.Fatalf("JSONAPI() error = %v", err)
	}
	if got.Route.Name != JSONAPIRoute || got.Route.Path != "/jsonapi" || got.Route.Primary {
		t.Fatalf("JSONAPI() route = %+v", got.Route)
	}
	if got.URL != "http://localhost/jsonapi" || got.Resolution != ResolutionCatalog {
		t.Fatalf("JSONAPI() = %+v", got)
	}
}

func TestProviderReturnsPrimaryAppCatalog(t *testing.T) {
	t.Parallel()

	routes, err := Provider().Routes(nil, nil)
	if err != nil {
		t.Fatalf("Provider().Routes() error = %v", err)
	}
	if len(routes.Routes) != 2 {
		t.Fatalf("Provider().Routes() count = %d, want 2", len(routes.Routes))
	}
	route := routes.Routes[0]
	if route.Name != "app" || !route.Primary {
		t.Fatalf("Provider().Routes()[0] = %+v", route)
	}
	jsonapi := routes.Routes[1]
	if jsonapi.Name != JSONAPIRoute || jsonapi.Path != "/jsonapi" || jsonapi.Primary {
		t.Fatalf("Provider().Routes()[1] = %+v", jsonapi)
	}
}
