package cmd

import (
	pluginjobs "github.com/libops/sitectl-drupal/pkg/jobs"
	"github.com/libops/sitectl/pkg/plugin"
)

var (
	drupalServiceName = func() *string { s := "drupal"; return &s }()
	sdk               *plugin.SDK
)

func init() {
	loginCmd.Flags().Uint("uid", 1, "Drupal user ID to generate the login link for.")
}

// RegisterCommands registers all drupal commands with the plugin SDK
func RegisterCommands(s *plugin.SDK) {
	sdk = s
	sdk.SetComposeProjectDiscovery(plugin.ComposeProjectDiscovery{
		RequiredServices:          []string{"drupal"},
		ForbiddenComposerPackages: []string{"drupal/islandora"},
		Reason:                    "drupal service without drupal/islandora in composer.json",
	})
	pluginjobs.Register(s)
	sdk.AddCommand(sdk.GetDiscoveryMetadataCommand())
	sdk.AddCommand(componentExtensionCmd)
	sdk.RegisterStandardComposeTemplate(createDefinition(), plugin.StandardComposeTemplateOptions{
		DefaultPath:         "./drupal",
		DefaultPlugin:       "drupal",
		DefaultDrupalRootfs: drupalCreateDrupalRoot,
		DrupalContainerRoot: drupalContainerRoot,
		ReadyMessage:        "Drupal is ready for use through sitectl.",
		DisplayName:         "Drupal",
	})
	sdk.RegisterDebugHandler(&drupalDebugRunner{})
	sdk.AddCommand(drushCmd)
	sdk.AddCommand(loginCmd)
	sdk.AddCommand(syncCmd)
}
