package cmd

import (
	"github.com/libops/sitectl-drupal/pkg/endpoint"
	pluginjobs "github.com/libops/sitectl-drupal/pkg/jobs"
	"github.com/libops/sitectl/pkg/plugin"
)

var (
	drupalServiceName                  = func() *string { s := "drupal"; return &s }()
	drupalForbiddenISLEProjectServices = []string{"activemq", "alpaca", "blazegraph", "cantaloupe", "fcrepo", "milliner", "triplet"}
	sdk                                *plugin.SDK
)

func init() {
	loginCmd.Flags().Uint("uid", 1, "Drupal user ID to generate the login link for.")
}

// RegisterCommands registers all Drupal commands with the plugin SDK.
func RegisterCommands(s *plugin.SDK) error {
	sdk = s
	sdk.SetComposeProjectDiscovery(plugin.ComposeProjectDiscovery{
		RequiredServices:          []string{"drupal"},
		ForbiddenServices:         drupalForbiddenISLEProjectServices,
		ForbiddenComposerPackages: []string{"drupal/islandora"},
		Reason:                    "drupal service without drupal/islandora in composer.json or ISLE services",
	})
	pluginjobs.Register(s)
	if err := registerDrupalComponents(s); err != nil {
		return err
	}
	sdk.RegisterComposeTemplateCreateRunner(createDefinition(), plugin.ComposeTemplateCreateOptions{
		DefaultPath:         "./drupal",
		DefaultPlugin:       "drupal",
		DefaultDrupalRootfs: drupalCreateDrupalRoot,
		DrupalContainerRoot: drupalContainerRoot,
		ReadyMessage:        "Drupal is ready for use through sitectl.",
	})
	sdk.RegisterDebugRunner(&drupalDebugRunner{})
	sdk.RegisterDeployRunner(drupalDeployDefinition(), drupalDeployRunner{})
	sdk.RegisterHealthcheckRunner(drupalHealthcheckRunner)
	sdk.RegisterVerifyRunner(&drupalVerifyRunner{})
	sdk.RegisterIngressRouteProvider(endpoint.Provider())
	sdk.AddCommand(composerCmd)
	sdk.AddCommand(newCrosswalkCmd(defaultCrosswalkRuntime()))
	sdk.AddCommand(drushCmd)
	sdk.AddCommand(loginCmd)
	sdk.AddCommand(solrConfigCmd)
	sdk.AddCommand(syncCmd)
	return nil
}
