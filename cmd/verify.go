package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/libops/sitectl/pkg/plugin"
	sitevalidate "github.com/libops/sitectl/pkg/validate"
	"github.com/spf13/cobra"
)

const drupalQueueProbe = `$cron = \Drupal::service('cron'); $workers = \Drupal::service('plugin.manager.queue_worker')->getDefinitions(); print json_encode(['cron' => $cron !== NULL, 'queue_workers' => count($workers)]);`

const drupalSolrProbe = `$server = \Drupal::entityTypeManager()->getStorage('search_api_server')->load('default_solr_server'); print json_encode(['exists' => $server !== NULL, 'enabled' => $server ? (bool) $server->status() : false, 'available' => $server ? (bool) $server->isAvailable() : false]);`

const drupalConfigDriftProbe = `$active = \Drupal::service('config.storage'); $directory = \Drupal\Core\Site\Settings::get('config_sync_directory'); $result = []; if (is_string($directory) && $directory !== '') { $sync = new \Drupal\Core\Config\FileStorage($directory); $names = array_unique(array_merge($active->listAll(), $sync->listAll())); sort($names); foreach ($names as $name) { $active_data = $active->read($name); $sync_data = $sync->read($name); if (!is_array($active_data) || !is_array($sync_data) || $active_data === $sync_data) { continue; } $keys = array_unique(array_merge(array_keys($active_data), array_keys($sync_data))); sort($keys); foreach ($keys as $key) { if (!array_key_exists($key, $active_data) || !array_key_exists($key, $sync_data) || $active_data[$key] !== $sync_data[$key]) { $result[$name][] = $key; } } } } print json_encode($result);`

type drupalVerifyRuntime interface {
	ExecCapture(context.Context, string, string, []string) (string, error)
}

type dockerDrupalVerifyRuntime struct {
	client *docker.DockerClient
}

func (r dockerDrupalVerifyRuntime) ExecCapture(ctx context.Context, container, workingDir string, argv []string) (string, error) {
	return docker.ExecCapture(ctx, r.client, container, workingDir, argv)
}

type drupalVerifyRunner struct{}

func (r *drupalVerifyRunner) BindFlags(_ *cobra.Command) {}

func (r *drupalVerifyRunner) Run(cmd *cobra.Command, _ *config.Context) ([]sitevalidate.Result, error) {
	ctx, cli, container, err := getDrupalContainerForSDK(cmd.Context())
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	return runDrupalVerifyChecks(cmd.Context(), dockerDrupalVerifyRuntime{client: cli}, container, ctx.EffectiveDrupalContainerRoot()), nil
}

var _ plugin.VerifyRunner = (*drupalVerifyRunner)(nil)

func runDrupalVerifyChecks(ctx context.Context, runtime drupalVerifyRuntime, container, workingDir string) []sitevalidate.Result {
	type check struct {
		name string
		argv []string
		read func(string) sitevalidate.Result
		fix  string
	}

	checks := []check{
		{
			name: "verify:drupal:bootstrap",
			argv: []string{drushExecutable, "status", "--field=bootstrap"},
			read: func(output string) sitevalidate.Result {
				if strings.EqualFold(strings.TrimSpace(output), "Successful") {
					return verifyOK("verify:drupal:bootstrap", "Drupal bootstrapped successfully")
				}
				return verifyFailed("verify:drupal:bootstrap", fmt.Sprintf("unexpected bootstrap status %q", strings.TrimSpace(output)), "inspect Drupal logs and settings.php")
			},
			fix: "inspect Drupal logs and settings.php",
		},
		{
			name: "verify:drupal:database-identity",
			argv: []string{drushExecutable, "sql:query", "SELECT CURRENT_USER();", "--extra=--batch", "--extra=--skip-column-names"},
			read: verifyDrupalDatabaseIdentity,
			fix:  "mount only the scoped Drupal database password and recreate the application database user",
		},
		{
			name: "verify:drupal:config-drift",
			argv: []string{drushExecutable, "config:status", "--format=json"},
			read: verifyDrupalConfigDrift,
			fix:  "export intentional configuration changes or import the committed configuration",
		},
		{
			name: "verify:drupal:cron-queue",
			argv: []string{drushExecutable, "php:eval", drupalQueueProbe},
			read: verifyDrupalCronQueue,
			fix:  "repair Drupal cron and queue worker service discovery",
		},
		{
			name: "verify:drupal:solr",
			argv: []string{drushExecutable, "php:eval", drupalSolrProbe},
			read: verifyDrupalSolr,
			fix:  "check the default_solr_server configuration and Solr connectivity",
		},
	}

	results := make([]sitevalidate.Result, 0, len(checks))
	for _, check := range checks {
		output, err := runtime.ExecCapture(ctx, container, workingDir, check.argv)
		if err != nil {
			results = append(results, verifyFailed(check.name, err.Error(), check.fix))
			continue
		}
		result := check.read(output)
		if check.name == "verify:drupal:config-drift" && result.Status == sitevalidate.StatusFailed {
			probeOutput, probeErr := runtime.ExecCapture(ctx, container, workingDir, []string{drushExecutable, "php:eval", drupalConfigDriftProbe})
			if probeErr == nil {
				if differences := describeDrupalConfigDriftKeys(probeOutput); len(differences) > 0 {
					result.Detail += "; differing top-level keys: " + strings.Join(differences, ", ")
				}
			}
		}
		results = append(results, result)
	}
	return results
}

func describeDrupalConfigDriftKeys(output string) []string {
	var values map[string][]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &values); err != nil {
		return nil
	}

	differences := make([]string, 0, len(values))
	for name, keys := range values {
		name = strings.TrimSpace(name)
		if name == "" || len(keys) == 0 {
			continue
		}
		cleanKeys := make([]string, 0, len(keys))
		for _, key := range keys {
			if key = strings.TrimSpace(key); key != "" {
				cleanKeys = append(cleanKeys, key)
			}
		}
		if len(cleanKeys) == 0 {
			continue
		}
		sort.Strings(cleanKeys)
		if len(cleanKeys) > 10 {
			cleanKeys = append(cleanKeys[:10], fmt.Sprintf("and %d more", len(cleanKeys)-10))
		}
		differences = append(differences, fmt.Sprintf("%s [%s]", name, strings.Join(cleanKeys, ", ")))
	}
	sort.Strings(differences)
	if len(differences) > 10 {
		differences = append(differences[:10], fmt.Sprintf("and %d more configurations", len(differences)-10))
	}
	return differences
}

func verifyDrupalDatabaseIdentity(output string) sitevalidate.Result {
	identity := strings.TrimSpace(output)
	if identity == "" {
		return verifyFailed("verify:drupal:database-identity", "database returned an empty current user", "check the scoped Drupal database secret")
	}
	user, _, _ := strings.Cut(identity, "@")
	if strings.EqualFold(strings.TrimSpace(user), "root") {
		return verifyFailed("verify:drupal:database-identity", "Drupal is connected as the MariaDB root user", "configure Drupal with its scoped application database user")
	}
	return verifyOK("verify:drupal:database-identity", identity)
}

func verifyDrupalConfigDrift(output string) sitevalidate.Result {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return verifyFailed("verify:drupal:config-drift", "config:status returned no JSON", "confirm Drush can read the active and sync configuration")
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return verifyFailed("verify:drupal:config-drift", fmt.Sprintf("invalid config:status JSON: %v", err), "run drush config:status and resolve the error")
	}
	clean := false
	switch typed := value.(type) {
	case []any:
		clean = len(typed) == 0
	case map[string]any:
		clean = len(typed) == 0
	}
	if !clean {
		detail := "active Drupal configuration differs from the sync directory"
		if differences := describeDrupalConfigDrift(value); len(differences) > 0 {
			detail += ": " + strings.Join(differences, ", ")
		}
		return verifyFailed("verify:drupal:config-drift", detail, "export intentional changes or import the committed configuration")
	}
	return verifyOK("verify:drupal:config-drift", "active configuration matches the sync directory")
}

func describeDrupalConfigDrift(value any) []string {
	differences := make([]string, 0)
	appendDifference := func(name, state string) {
		name = strings.TrimSpace(name)
		state = strings.TrimSpace(state)
		if name == "" {
			return
		}
		if state != "" {
			name += " (" + state + ")"
		}
		differences = append(differences, name)
	}

	readRow := func(fallback string, row any) {
		fields, ok := row.(map[string]any)
		if !ok {
			appendDifference(fallback, "")
			return
		}
		name, _ := fields["name"].(string)
		state, _ := fields["state"].(string)
		if name == "" {
			name = fallback
		}
		appendDifference(name, state)
	}

	switch typed := value.(type) {
	case map[string]any:
		for name, row := range typed {
			readRow(name, row)
		}
	case []any:
		for _, row := range typed {
			readRow("", row)
		}
	}
	sort.Strings(differences)
	if len(differences) > 10 {
		remaining := len(differences) - 10
		differences = append(differences[:10], fmt.Sprintf("and %d more", remaining))
	}
	return differences
}

func verifyDrupalCronQueue(output string) sitevalidate.Result {
	var probe struct {
		Cron         bool `json:"cron"`
		QueueWorkers *int `json:"queue_workers"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &probe); err != nil {
		return verifyFailed("verify:drupal:cron-queue", fmt.Sprintf("invalid cron/queue probe JSON: %v", err), "repair Drupal cron and queue worker service discovery")
	}
	if probe.QueueWorkers == nil {
		return verifyFailed("verify:drupal:cron-queue", "queue worker discovery result is missing", "repair Drupal queue worker service discovery")
	}
	if !probe.Cron || *probe.QueueWorkers < 0 {
		return verifyFailed("verify:drupal:cron-queue", fmt.Sprintf("cron=%t queue_workers=%d", probe.Cron, *probe.QueueWorkers), "repair Drupal cron and queue worker service discovery")
	}
	return verifyOK("verify:drupal:cron-queue", fmt.Sprintf("cron available; %d configured queue workers discovered", *probe.QueueWorkers))
}

func verifyDrupalSolr(output string) sitevalidate.Result {
	var probe struct {
		Exists    bool `json:"exists"`
		Enabled   bool `json:"enabled"`
		Available bool `json:"available"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &probe); err != nil {
		return verifyFailed("verify:drupal:solr", fmt.Sprintf("invalid Search API server probe JSON: %v", err), "check the default_solr_server configuration and Search API Solr compatibility")
	}
	if !probe.Exists {
		return verifyFailed("verify:drupal:solr", "default_solr_server does not exist", "import or recreate the default_solr_server configuration")
	}
	if !probe.Enabled {
		return verifyFailed("verify:drupal:solr", "default_solr_server is disabled", "enable the default_solr_server configuration")
	}
	if !probe.Available {
		return verifyFailed("verify:drupal:solr", "default_solr_server cannot reach Solr", "check the default_solr_server host, port, core, and Solr health")
	}
	return verifyOK("verify:drupal:solr", "default_solr_server reached Solr")
}

func verifyOK(name, detail string) sitevalidate.Result {
	return sitevalidate.Result{Name: name, Status: sitevalidate.StatusOK, Detail: detail}
}

func verifyFailed(name, detail, fix string) sitevalidate.Result {
	return sitevalidate.Result{Name: name, Status: sitevalidate.StatusFailed, Detail: detail, FixHint: fix}
}
