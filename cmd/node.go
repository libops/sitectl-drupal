package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/libops/sitectl-drupal/drupal"
	"github.com/spf13/cobra"
)

// clientKey is the context key for the Drupal client
type clientKey struct{}

var bundleConfig string

// nodeCmd represents the node command group
var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Drupal node operations",
	Long: `Commands for interacting with Drupal nodes via the JSON API.

Use --bundle-config to load bundle definitions from a Drupal config sync export
or generated YAML file. This enables field validation and type information.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var opts []drupal.ClientOption

		// Load bundle config if provided
		if bundleConfig != "" {
			opts = append(opts, drupal.WithBundlesFromPath(bundleConfig))
		}

		// Load auth from environment
		opts = append(opts, drupal.WithBasicAuthFromEnv("DRUPAL_USERNAME", "DRUPAL_PASSWORD"))

		client := drupal.NewClient(opts...)

		// Store client in context for subcommands
		ctx := context.WithValue(cmd.Context(), clientKey{}, client)
		cmd.SetContext(ctx)

		return nil
	},
}

func init() {
	// Register persistent flag for bundle configuration
	nodeCmd.PersistentFlags().StringVar(&bundleConfig, "bundle-config", "",
		"Path to bundle configuration YAML (from Drupal config sync or generated)")

	// Add subcommands
	nodeCmd.AddCommand(nodeFetchCmd)
	nodeCmd.AddCommand(nodeValidateCmd)
	nodeCmd.AddCommand(nodeBundlesCmd)
	nodeCmd.AddCommand(nodeCacheClearCmd)
}

// getClient retrieves the Drupal client from the command context
func getClient(cmd *cobra.Command) *drupal.Client {
	return cmd.Context().Value(clientKey{}).(*drupal.Client)
}

// nodeFetchCmd fetches and displays a node
var nodeFetchCmd = &cobra.Command{
	Use:   "fetch <url>",
	Short: "Fetch a node from the Drupal JSON API",
	Long: `Fetches a node from the Drupal JSON API and displays it as JSON.

The URL should be a full URL to the node JSON endpoint, e.g.:
  https://example.com/node/123?_format=json

If --bundle-config is provided, the node will be validated against its bundle schema.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		client := getClient(cmd)

		node, err := client.FetchNode(url)
		if err != nil {
			return fmt.Errorf("fetching node: %w", err)
		}

		// Validate if registry has bundles loaded
		if errors := node.Validate(); len(errors) > 0 {
			fmt.Fprintf(os.Stderr, "Warning: validation errors for bundle %q:\n", node.Bundle())
			for _, e := range errors {
				fmt.Fprintf(os.Stderr, "  - %s\n", e)
			}
		}

		// Output as JSON
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(node)
	},
}

// nodeValidateCmd validates a node against bundle schema
var nodeValidateCmd = &cobra.Command{
	Use:   "validate <url>",
	Short: "Validate a node against its bundle schema",
	Long: `Fetches a node and validates it against the bundle schema.

Requires --bundle-config to be set with bundle definitions.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		client := getClient(cmd)

		node, err := client.FetchNode(url)
		if err != nil {
			return fmt.Errorf("fetching node: %w", err)
		}

		registry := node.Registry()
		if registry == nil {
			return fmt.Errorf("no bundle definitions loaded - use --bundle-config")
		}

		def, ok := registry.GetNodeBundle(node.Bundle())
		if !ok {
			return fmt.Errorf("unknown bundle %q - not in registry", node.Bundle())
		}

		fmt.Printf("Validating node (nid: %s) against bundle: %s\n", node.Nid.String(), def.Name)
		if def.Description != "" {
			fmt.Printf("Bundle description: %s\n", def.Description)
		}
		fmt.Println()

		errors := node.Validate()
		if len(errors) == 0 {
			fmt.Println("Node is valid")
			return nil
		}

		fmt.Println("Validation errors:")
		for _, e := range errors {
			fmt.Printf("  - %s\n", e)
		}
		return fmt.Errorf("validation failed with %d error(s)", len(errors))
	},
}

// nodeBundlesCmd lists registered bundles
var nodeBundlesCmd = &cobra.Command{
	Use:   "bundles",
	Short: "List all registered bundle definitions",
	Long: `Lists all bundle definitions loaded from --bundle-config.

Shows bundle names, descriptions, field counts, and required fields.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := getClient(cmd)

		entityTypes := client.Registry.ListAllEntityTypes()
		if len(entityTypes) == 0 {
			fmt.Println("No bundles registered")
			fmt.Println("Use --bundle-config to load bundle definitions")
			return nil
		}

		fmt.Println("Registered bundles:")
		for _, entityType := range entityTypes {
			bundles := client.Registry.ListBundles(entityType)
			if len(bundles) == 0 {
				continue
			}

			fmt.Printf("\n%s:\n", entityType)
			for _, name := range bundles {
				def, _ := client.Registry.GetBundle(entityType, name)
				fmt.Printf("\n  %s (%s)\n", def.Name, def.MachineName)
				if def.Description != "" {
					fmt.Printf("    %s\n", def.Description)
				}
				fmt.Printf("    Fields: %d\n", len(def.Fields))

				// List required fields
				var required []string
				for _, f := range def.Fields {
					if f.Required {
						required = append(required, f.Name)
					}
				}
				if len(required) > 0 {
					fmt.Printf("    Required: %v\n", required)
				}
			}
		}

		return nil
	},
}

// nodeCacheClearCmd clears the bundle definition cache
var nodeCacheClearCmd = &cobra.Command{
	Use:   "cache-clear",
	Short: "Clear cached bundle definitions",
	Long:  `Clears the bundle definition cache from ~/.sitectl/cache`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := drupal.ClearCache(); err != nil {
			return fmt.Errorf("clearing cache: %w", err)
		}
		fmt.Println("Bundle definition cache cleared")
		return nil
	},
}
