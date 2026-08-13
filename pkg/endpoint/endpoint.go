// Package endpoint resolves public Drupal service endpoints from sitectl
// ingress route catalogs.
package endpoint

import (
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

// Resolution identifies the route source selected for an endpoint.
type Resolution = plugin.IngressRouteResolution

const (
	ResolutionTraefik = plugin.IngressRouteResolutionTraefik
	ResolutionCatalog = plugin.IngressRouteResolutionCatalog
)

// Resolved pairs a plugin-owned route with its public URL.
type Resolved = plugin.ResolvedIngressRoute

const (
	// JSONAPIRoute is Drupal's public JSON:API endpoint route name.
	JSONAPIRoute = "jsonapi"
)

type drupalRouteProvider struct {
	app plugin.IngressRouteProvider
}

// Provider returns the Drupal ingress route provider shared by route discovery
// and JSON:API endpoint resolution.
func Provider() plugin.IngressRouteProvider {
	return drupalRouteProvider{app: plugin.StandardComposeWebIngressRoutesWithOptions(plugin.StandardComposeWebIngressOptions{
		AppService: "drupal",
		Router:     "drupal",
	})}
}

func (p drupalRouteProvider) BindFlags(cmd *cobra.Command) { p.app.BindFlags(cmd) }

func (p drupalRouteProvider) Routes(cmd *cobra.Command, ctx *config.Context) (plugin.IngressRoutes, error) {
	routes, err := p.app.Routes(cmd, ctx)
	if err != nil {
		return plugin.IngressRoutes{}, err
	}
	if len(routes.Routes) == 0 {
		return routes, nil
	}
	jsonapi := routes.Routes[0]
	jsonapi.Name = JSONAPIRoute
	jsonapi.Path = "/jsonapi"
	jsonapi.Primary = false
	routes.Routes = append(routes.Routes, jsonapi)
	return routes, nil
}

// JSONAPI resolves Drupal's public JSON:API root for a sitectl context.
func JSONAPI(cmd *cobra.Command, ctx *config.Context) (Resolved, error) {
	return plugin.ResolveIngressRouteFromProvider(cmd, ctx, Provider(), JSONAPIRoute)
}
