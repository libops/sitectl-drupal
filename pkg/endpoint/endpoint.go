// Package endpoint resolves public Drupal service endpoints from sitectl
// ingress route catalogs.
package endpoint

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	corehealthcheck "github.com/libops/sitectl/pkg/healthcheck"
	"github.com/libops/sitectl/pkg/plugin"
	"github.com/spf13/cobra"
)

// Resolution identifies the route source selected for an endpoint.
type Resolution string

const (
	ResolutionTraefik Resolution = "traefik"
	ResolutionCatalog Resolution = "catalog"
)

// Resolved pairs a plugin-owned route with its public URL. It is deliberately
// local to the plugin until the generic resolver ships in a sitectl release.
type Resolved struct {
	Route      plugin.IngressRoute
	URL        string
	Resolution Resolution
}

const (
	// AppRoute is the public Drupal application route name.
	AppRoute = "app"
	// JSONAPIRoute is Drupal's public JSON:API endpoint route name.
	JSONAPIRoute = "jsonapi"
)

type drupalRouteProvider struct {
	app plugin.IngressRouteProvider
}

// Provider returns the Drupal ingress route provider shared by route discovery
// and callers that need the public Drupal application endpoint.
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

// App resolves the public Drupal application endpoint for a sitectl context.
func App(cmd *cobra.Command, ctx *config.Context) (Resolved, error) {
	return resolve(cmd, ctx, Provider(), AppRoute)
}

// JSONAPI resolves Drupal's public JSON:API root for a sitectl context.
func JSONAPI(cmd *cobra.Command, ctx *config.Context) (Resolved, error) {
	return resolve(cmd, ctx, Provider(), JSONAPIRoute)
}

func resolve(cmd *cobra.Command, ctx *config.Context, provider plugin.IngressRouteProvider, name string) (Resolved, error) {
	if cmd == nil {
		cmd = &cobra.Command{}
	}
	routes, err := provider.Routes(cmd, ctx)
	if err != nil {
		return Resolved{}, fmt.Errorf("list ingress routes: %w", err)
	}
	var selected *plugin.IngressRoute
	for index := range routes.Routes {
		if strings.TrimSpace(routes.Routes[index].Name) == name {
			selected = &routes.Routes[index]
			break
		}
	}
	if selected == nil {
		return Resolved{}, fmt.Errorf("ingress route %q not found", name)
	}
	route := *selected
	route.DefaultScheme = firstNonempty(route.DefaultScheme, routes.Scheme, "http")
	route.DefaultDomain = firstNonempty(route.DefaultDomain, routes.Domain)
	result := Resolved{Route: route, Resolution: ResolutionCatalog}
	if route.DefaultDomain != "" {
		result.URL, err = routeURL(route.DefaultScheme, route.DefaultDomain, route.Path)
		if err != nil {
			return Resolved{}, err
		}
	}
	if ctx != nil {
		resolvedURL, ok, resolveErr := corehealthcheck.PublicURLFromTraefik(ctx, corehealthcheck.TraefikRouteOptions{
			AppService: route.Service, Router: route.Router, TraefikService: route.TraefikService,
			DefaultScheme: route.DefaultScheme, DefaultDomain: route.DefaultDomain,
		})
		if resolveErr != nil {
			return Resolved{}, fmt.Errorf("resolve ingress route %q: %w", name, resolveErr)
		}
		if ok {
			parsed, parseErr := url.Parse(resolvedURL)
			if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
				return Resolved{}, fmt.Errorf("resolve ingress route %q: invalid public URL", name)
			}
			parsed.Path = joinRoutePath(parsed.Path, route.Path)
			result.URL = parsed.String()
			result.Resolution = ResolutionTraefik
		}
	}
	if result.URL == "" {
		return Resolved{}, fmt.Errorf("ingress route %q has no resolvable public URL", name)
	}
	return result, nil
}

func routeURL(scheme, domain, routePath string) (string, error) {
	parsed := &url.URL{Scheme: scheme, Host: domain, Path: cleanRoutePath(routePath)}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("ingress route has no scheme or domain")
	}
	return parsed.String(), nil
}

func joinRoutePath(base, suffix string) string {
	base, suffix = cleanRoutePath(base), cleanRoutePath(suffix)
	if suffix == "" || base == suffix || strings.HasSuffix(base, suffix) {
		return base
	}
	return path.Join("/", base, suffix)
}

func cleanRoutePath(value string) string {
	if value = strings.TrimSpace(value); value == "" || value == "/" {
		return ""
	}
	return path.Clean("/" + strings.TrimLeft(value, "/"))
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
