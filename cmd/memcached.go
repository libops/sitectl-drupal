package cmd

import (
	_ "embed"
	"fmt"

	corecomponent "github.com/libops/sitectl/pkg/component"
	"github.com/libops/sitectl/pkg/plugin"
	coredevmode "github.com/libops/sitectl/pkg/services/devmode"
	memcachedcomponent "github.com/libops/sitectl/pkg/services/memcached"
	coretraefik "github.com/libops/sitectl/pkg/services/traefik"
)

const (
	memcachedSettingsStartMarker    = "// sitectl component memcached: begin"
	memcachedSettingsEndMarker      = "// sitectl component memcached: end"
	memcachedDockerfileStartMarker  = "# sitectl component memcached: begin"
	memcachedDockerfileEndMarker    = "# sitectl component memcached: end"
	memcachedComposerPackage        = "drupal/memcache"
	memcachedComposerPackageVersion = "^2.7"
)

//go:embed assets/memcached.Dockerfile.txt
var memcachedDockerfileBlock string

//go:embed assets/memcached.settings.php
var memcachedSettingsBlock string

func registerDrupalComponents(s *plugin.SDK) error {
	components, err := drupalServiceComponents()
	if err != nil {
		return fmt.Errorf("build Drupal service components: %w", err)
	}
	s.RegisterServiceComponents(plugin.ServiceComponentRegistryOptions{
		DisplayName: "Drupal",
		Components:  components,
	})
	return nil
}

func drupalServiceComponents() ([]corecomponent.ComposeServiceComponent, error) {
	memcached, err := memcachedcomponent.New(memcachedcomponent.TargetOptions{
		AppService: "drupal",
		AppDependencies: map[string]any{
			"memcached": map[string]any{"condition": "service_started"},
		},
		DefaultState:       corecomponent.StateOff,
		DefaultDisposition: corecomponent.DispositionDisabled,
		Dependencies: corecomponent.Dependencies{
			DrupalModules: []corecomponent.DrupalModuleDependency{{
				Module:          "memcache",
				ComposerPackage: memcachedComposerPackage,
				Mode:            corecomponent.DrupalModuleDependencyEnableOnly,
			}},
		},
		Behavior: corecomponent.Behavior{
			Idempotent: true,
			Enable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Adds the Memcached compose service, Drupal memcache Composer package, PHP extension package, and cache settings.",
			},
			Disable: corecomponent.TransitionBehavior{
				DataMigration: corecomponent.DataMigrationNone,
				Summary:       "Removes the local Memcached compose service and Drupal cache settings; build-time dependencies may remain installed.",
			},
		},
		FileOnRules: []corecomponent.FileRule{
			{
				Files: []string{"composer.json"},
				Op:    corecomponent.OpSet,
				Path:  ".require." + memcachedComposerPackage,
				Value: memcachedComposerPackageVersion,
			},
			{
				Files:       []string{"Dockerfile"},
				Op:          corecomponent.OpSet,
				StartMarker: memcachedDockerfileStartMarker,
				EndMarker:   memcachedDockerfileEndMarker,
				Content:     memcachedDockerfileBlock,
			},
			{
				Files:       []string{"assets/default_settings.txt"},
				Op:          corecomponent.OpSet,
				StartMarker: memcachedSettingsStartMarker,
				EndMarker:   memcachedSettingsEndMarker,
				Content:     memcachedSettingsBlock,
			},
		},
		FileOffRules: []corecomponent.FileRule{
			{
				Files:       []string{"assets/default_settings.txt"},
				Op:          corecomponent.OpDelete,
				StartMarker: memcachedSettingsStartMarker,
				EndMarker:   memcachedSettingsEndMarker,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	ingress, err := coretraefik.Ingress(coretraefik.IngressOptions{
		AppService:      "drupal",
		HTTPEntrypoint:  "http",
		HTTPSEntrypoint: "https",
		RouterHosts: map[string]string{
			"drupal":  "{domain}",
			"solr":    "solr.{domain}",
			"traefik": "traefik.{domain}",
		},
		ServiceEnvTemplates: map[string]map[string]string{
			"drupal": {
				"DRUPAL_DEFAULT_SITE_URL": "{base_url}",
				"DRUPAL_ENABLE_HTTPS":     "{https_enabled}",
				"DRUSH_OPTIONS_URI":       "{base_url}",
			},
		},
	})
	if err != nil {
		return nil, err
	}
	devMode, err := coredevmode.Component(coredevmode.Options{
		AppService: "drupal",
		Volumes: []string{
			"./assets:/var/www/drupal/assets:z,rw",
			"./composer.json:/var/www/drupal/composer.json:z,rw",
			"./composer.lock:/var/www/drupal/composer.lock:z,rw",
			"./config:/var/www/drupal/config:z,rw",
			"./web/modules/custom:/var/www/drupal/web/modules/custom:z,rw",
			"./web/themes/custom:/var/www/drupal/web/themes/custom:z,rw",
		},
	})
	if err != nil {
		return nil, err
	}
	return []corecomponent.ComposeServiceComponent{memcached, ingress, devMode}, nil
}
