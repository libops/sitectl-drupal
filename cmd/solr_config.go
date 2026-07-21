package cmd

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/libops/sitectl/pkg/config"
	"github.com/libops/sitectl/pkg/docker"
	"github.com/spf13/cobra"
)

const (
	defaultSolrConfigServer = "default_solr_server"
	defaultSolrCore         = "default"
	solrContainerRoot       = "/opt/solr/server/solr"
	solrAPIBaseURL          = "http://127.0.0.1:8983/solr"

	maxSolrConfigZipBytes       int64 = 32 << 20
	maxSolrConfigFileBytes      int64 = 8 << 20
	maxSolrConfigExtractedBytes int64 = 64 << 20
	maxSolrConfigFiles                = 2048
	maxSolrConfigArchiveEntries       = 4096
)

var (
	solrConfigServerPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,127}$`)
	solrCorePattern         = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	solrVersionPattern      = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){1,2}(?:[-+][A-Za-z0-9.-]+)?$`)

	solrConfigCmd = newSolrConfigCommand()
)

type solrConfigOptions struct {
	Server      string
	Core        string
	SolrVersion string
	Output      string
	Reindex     bool
}

type solrConfigTree map[string][]byte

type solrConfigContainerRuntime interface {
	ExecCapture(ctx context.Context, container, workingDir string, argv []string) (string, error)
	CopyFrom(ctx context.Context, container, sourcePath string) (io.ReadCloser, error)
	CopyTo(ctx context.Context, container, destinationPath string, archive io.Reader) error
	AcquireLock(ctx context.Context, container, corePath string) (context.Context, func() error, error)
}

type solrConfigHostStore interface {
	ReadTree(ctx context.Context, root string) (solrConfigTree, error)
	PublishTree(ctx context.Context, root string, tree solrConfigTree) error
}

type solrConfigDependencies struct {
	Runtime          solrConfigContainerRuntime
	Host             solrConfigHostStore
	DrupalContainer  string
	SolrContainer    string
	DrupalWorkingDir string
	OutputPath       string
}

type solrConfigRefreshResult struct {
	Version        string
	Core           string
	OutputPath     string
	Skipped        bool
	SkipReason     string
	HostUpdated    bool
	RuntimeUpdated bool
	CoreAction     string
	Reindexed      bool
}

type containerArchiveAPI interface {
	CopyFromContainer(ctx context.Context, containerID, sourcePath string) (io.ReadCloser, dockercontainer.PathStat, error)
	CopyToContainer(ctx context.Context, containerID, destinationPath string, content io.Reader, options dockercontainer.CopyToContainerOptions) error
}

type dockerSolrConfigRuntime struct {
	client  *docker.DockerClient
	archive containerArchiveAPI
}

func newSolrConfigCommand() *cobra.Command {
	options := solrConfigOptions{}
	root := &cobra.Command{
		Use:   "solr-config",
		Short: "Manage Search API Solr configuration",
	}
	refresh := &cobra.Command{
		Use:   "refresh",
		Short: "Refresh the tracked and running Solr core configuration",
		Long: `Generate configuration with Drupal Search API Solr, compare it with both the
tracked seed and the running Solr core, and update only the copies that have drifted.
The running core's data directory is preserved. With --reindex, all Drupal Search API
index trackers are reset and all indexes are rebuilt after a live configuration change.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSolrConfigRefreshCommand(cmd, options)
		},
	}
	refresh.Flags().StringVar(&options.Server, "server", defaultSolrConfigServer, "Drupal Search API server machine name")
	refresh.Flags().StringVar(&options.Core, "core", defaultSolrCore, "Solr core name")
	refresh.Flags().StringVar(&options.SolrVersion, "solr-version", "", "Solr version for generated config (detected from the running server by default)")
	refresh.Flags().StringVar(&options.Output, "output", "", "Tracked Solr conf directory, relative to the context project")
	refresh.Flags().BoolVar(&options.Reindex, "reindex", false, "Reset trackers and rebuild all Drupal Search API indexes after a live configuration change")
	root.AddCommand(refresh)
	return root
}

func runSolrConfigRefreshCommand(cmd *cobra.Command, options solrConfigOptions) (returnErr error) {
	ctx, cli, drupalContainer, err := getDrupalContainerFromFlags(cmd)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := cli.Close(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close docker client: %w", closeErr))
		}
	}()
	if strings.TrimSpace(drupalContainer) == "" {
		return fmt.Errorf("drupal container is not running")
	}

	solrContainer, err := cli.GetContainerNameContext(cmd.Context(), ctx, "solr")
	if err != nil {
		return fmt.Errorf("resolve Solr container: %w", err)
	}
	if strings.TrimSpace(solrContainer) == "" {
		return fmt.Errorf("solr container is not running")
	}

	outputPath, err := resolveSolrConfigOutput(ctx, options.Output, options.Core)
	if err != nil {
		return err
	}
	runtime, err := newDockerSolrConfigRuntime(cli)
	if err != nil {
		return err
	}
	host, closeHost, err := newSolrConfigHostStore(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := closeHost(); closeErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("close context file client: %w", closeErr))
		}
	}()

	result, err := refreshSolrConfig(cmd.Context(), solrConfigDependencies{
		Runtime:          runtime,
		Host:             host,
		DrupalContainer:  drupalContainer,
		SolrContainer:    solrContainer,
		DrupalWorkingDir: ctx.EffectiveDrupalContainerRoot(),
		OutputPath:       outputPath,
	}, options)
	if err != nil {
		return err
	}
	if err := writeSolrConfigResult(cmd, result); err != nil {
		return err
	}
	return returnErr
}

func newDockerSolrConfigRuntime(cli *docker.DockerClient) (*dockerSolrConfigRuntime, error) {
	if cli == nil || cli.CLI == nil {
		return nil, fmt.Errorf("docker client is unavailable")
	}
	archive, ok := cli.CLI.(containerArchiveAPI)
	if !ok {
		return nil, fmt.Errorf("docker client does not support container archive operations")
	}
	return &dockerSolrConfigRuntime{client: cli, archive: archive}, nil
}

func (r *dockerSolrConfigRuntime) ExecCapture(ctx context.Context, container, workingDir string, argv []string) (string, error) {
	return docker.ExecCapture(ctx, r.client, container, workingDir, argv)
}

func (r *dockerSolrConfigRuntime) CopyFrom(ctx context.Context, container, sourcePath string) (io.ReadCloser, error) {
	reader, _, err := r.archive.CopyFromContainer(ctx, container, sourcePath)
	return reader, err
}

func (r *dockerSolrConfigRuntime) CopyTo(ctx context.Context, container, destinationPath string, archive io.Reader) error {
	return r.archive.CopyToContainer(ctx, container, destinationPath, archive, dockercontainer.CopyToContainerOptions{
		CopyUIDGID: true,
	})
}

func refreshSolrConfig(ctx context.Context, dependencies solrConfigDependencies, options solrConfigOptions) (result solrConfigRefreshResult, returnErr error) {
	result = solrConfigRefreshResult{Core: options.Core, OutputPath: dependencies.OutputPath}
	if err := validateSolrConfigInputs(dependencies, options); err != nil {
		return result, err
	}
	enabled, err := drupalSearchAPISolrEnabled(ctx, dependencies)
	if err != nil {
		return result, err
	}
	if !enabled {
		result.Skipped = true
		result.SkipReason = "Drupal module search_api_solr is not enabled"
		return result, nil
	}

	version := strings.TrimSpace(options.SolrVersion)
	if version == "" {
		version, err = detectSolrVersion(ctx, dependencies.Runtime, dependencies.SolrContainer)
		if err != nil {
			return result, err
		}
	}
	if err := validateSolrVersion(version); err != nil {
		return result, err
	}
	result.Version = version

	generated, err := generateSolrConfig(ctx, dependencies, options.Server, version)
	if err != nil {
		return result, err
	}
	lockedContext, releaseLock, err := dependencies.Runtime.AcquireLock(ctx, dependencies.SolrContainer, path.Join(solrContainerRoot, options.Core))
	if err != nil {
		return result, err
	}
	ctx = lockedContext
	defer func() {
		if releaseErr := releaseLock(); releaseErr != nil {
			returnErr = errors.Join(returnErr, fmt.Errorf("release target solr config lock: %w", releaseErr))
		}
	}()
	pending, err := resumePendingSolrConfig(ctx, dependencies, options.Core, generated, options.Reindex)
	if err != nil {
		return result, err
	}
	if pending.Completed {
		result.RuntimeUpdated = true
		result.CoreAction = pending.CoreAction
		result.Reindexed = pending.Reindexed
	}

	hostCurrent, hostExists, err := readHostSolrConfig(ctx, dependencies.Host, dependencies.OutputPath)
	if err != nil {
		return result, err
	}
	hostDrift := !hostExists || !solrConfigTreesEqual(generated, hostCurrent)

	runtimePath := path.Join(solrContainerRoot, options.Core, "conf")
	runtimeCurrent, runtimeExists, err := readRuntimeSolrConfig(ctx, dependencies.Runtime, dependencies.SolrContainer, runtimePath)
	if err != nil {
		return result, err
	}
	runtimeDrift := !runtimeExists || !solrConfigTreesEqual(generated, runtimeCurrent)

	if !hostDrift && !runtimeDrift {
		return result, nil
	}

	if hostDrift {
		if err := dependencies.Host.PublishTree(ctx, dependencies.OutputPath, generated); err != nil {
			return result, fmt.Errorf("publish tracked Solr config %q: %w", dependencies.OutputPath, err)
		}
		result.HostUpdated = true
	}

	if runtimeDrift {
		completed, err := startPendingSolrConfig(ctx, dependencies, options.Core, generated, options.Reindex)
		if err != nil {
			return result, err
		}
		result.RuntimeUpdated = true
		result.CoreAction = completed.CoreAction
		result.Reindexed = completed.Reindexed
	}

	return result, nil
}

func validateSolrConfigInputs(dependencies solrConfigDependencies, options solrConfigOptions) error {
	if dependencies.Runtime == nil {
		return fmt.Errorf("solr container runtime is unavailable")
	}
	if dependencies.Host == nil {
		return fmt.Errorf("solr host store is unavailable")
	}
	if strings.TrimSpace(dependencies.DrupalContainer) == "" {
		return fmt.Errorf("drupal container is required")
	}
	if strings.TrimSpace(dependencies.SolrContainer) == "" {
		return fmt.Errorf("solr container is required")
	}
	if strings.TrimSpace(dependencies.OutputPath) == "" {
		return fmt.Errorf("tracked Solr config output path is required")
	}
	if !solrConfigServerPattern.MatchString(options.Server) {
		return fmt.Errorf("invalid Search API server %q: use a Drupal machine name", options.Server)
	}
	if !validSolrCore(options.Core) {
		return fmt.Errorf("invalid Solr core %q", options.Core)
	}
	return nil
}

func validSolrCore(core string) bool {
	return core != "." && core != ".." && solrCorePattern.MatchString(core)
}

func validateSolrVersion(version string) error {
	if !solrVersionPattern.MatchString(version) {
		return fmt.Errorf("invalid Solr version %q", version)
	}
	return nil
}

func resolveSolrConfigOutput(ctx *config.Context, requested, core string) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("context is nil")
	}
	if !validSolrCore(core) {
		return "", fmt.Errorf("invalid Solr core %q", core)
	}
	if ctx.DockerHostType == config.ContextRemote {
		return resolveRemoteSolrConfigOutput(ctx, requested, core)
	}
	return resolveLocalSolrConfigOutput(ctx, requested, core)
}

func resolveLocalSolrConfigOutput(ctx *config.Context, requested, core string) (string, error) {
	projectDir := filepath.Clean(strings.TrimSpace(ctx.ProjectDir))
	if projectDir == "." || projectDir == "" {
		return "", fmt.Errorf("context project directory is required")
	}

	var output string
	if strings.TrimSpace(requested) != "" {
		output = strings.TrimSpace(requested)
		if !filepath.IsAbs(output) {
			output = filepath.Join(projectDir, output)
		}
	} else {
		rootfs := inferredLocalSolrSeedRootfs(ctx)
		if !filepath.IsAbs(rootfs) {
			rootfs = filepath.Join(projectDir, rootfs)
		}
		output = filepath.Join(rootfs, "opt", "solr", "server", "solr", core, "conf")
	}
	output = filepath.Clean(output)

	relative, err := filepath.Rel(projectDir, output)
	if err != nil {
		return "", fmt.Errorf("resolve Solr config output: %w", err)
	}
	if relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("solr config output %q must be a directory within context project %q", output, projectDir)
	}
	return output, nil
}

func resolveRemoteSolrConfigOutput(ctx *config.Context, requested, core string) (string, error) {
	projectDir := path.Clean(strings.TrimSpace(ctx.ProjectDir))
	if projectDir == "." || projectDir == "" {
		return "", fmt.Errorf("context project directory is required")
	}
	if strings.Contains(projectDir, "\\") || strings.Contains(requested, "\\") {
		return "", fmt.Errorf("remote Solr config paths must use POSIX separators")
	}

	var output string
	if strings.TrimSpace(requested) != "" {
		output = strings.TrimSpace(requested)
		if !path.IsAbs(output) {
			output = path.Join(projectDir, output)
		}
	} else {
		rootfs := inferredRemoteSolrSeedRootfs(ctx)
		if !path.IsAbs(rootfs) {
			rootfs = path.Join(projectDir, rootfs)
		}
		output = path.Join(rootfs, "opt", "solr", "server", "solr", core, "conf")
	}
	output = path.Clean(output)
	if !posixPathContained(projectDir, output) || output == projectDir {
		return "", fmt.Errorf("solr config output %q must be a directory within context project %q", output, projectDir)
	}
	return output, nil
}

func inferredLocalSolrSeedRootfs(ctx *config.Context) string {
	drupalRootfs := filepath.Clean(strings.TrimSpace(ctx.DrupalRootfs))
	if drupalRootfs != "" && drupalRootfs != "." {
		slashRootfs := filepath.ToSlash(drupalRootfs)
		const drupalContainerSuffix = "/var/www/drupal"
		if strings.HasSuffix(slashRootfs, drupalContainerSuffix) {
			rootfs := strings.TrimSuffix(slashRootfs, drupalContainerSuffix)
			if rootfs != "" && rootfs != "." {
				return filepath.FromSlash(rootfs)
			}
		}
	}
	if ctx.Plugin == "isle" {
		return filepath.Join("drupal", "rootfs")
	}
	return "rootfs"
}

func inferredRemoteSolrSeedRootfs(ctx *config.Context) string {
	drupalRootfs := path.Clean(strings.ReplaceAll(strings.TrimSpace(ctx.DrupalRootfs), "\\", "/"))
	if drupalRootfs != "" && drupalRootfs != "." {
		const drupalContainerSuffix = "/var/www/drupal"
		if strings.HasSuffix(drupalRootfs, drupalContainerSuffix) {
			rootfs := strings.TrimSuffix(drupalRootfs, drupalContainerSuffix)
			if rootfs != "" && rootfs != "." {
				return rootfs
			}
		}
	}
	if ctx.Plugin == "isle" {
		return "drupal/rootfs"
	}
	return "rootfs"
}

func posixPathContained(root, target string) bool {
	root = path.Clean(root)
	target = path.Clean(target)
	if root == "/" {
		return strings.HasPrefix(target, "/")
	}
	return strings.HasPrefix(target, strings.TrimSuffix(root, "/")+"/")
}

func detectSolrVersion(ctx context.Context, runtime solrConfigContainerRuntime, container string) (string, error) {
	output, err := runSolrAPI(ctx, runtime, container, "/admin/info/system", [][2]string{{"wt", "json"}})
	if err != nil {
		return "", fmt.Errorf("detect Solr version: %w", err)
	}
	var response struct {
		Lucene struct {
			SolrSpecVersion string `json:"solr-spec-version"`
		} `json:"lucene"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return "", fmt.Errorf("decode Solr system response: %w", err)
	}
	version := strings.TrimSpace(response.Lucene.SolrSpecVersion)
	if version == "" {
		return "", fmt.Errorf("solr system response did not include lucene.solr-spec-version")
	}
	if err := validateSolrVersion(version); err != nil {
		return "", fmt.Errorf("solr system response: %w", err)
	}
	return version, nil
}

func generateSolrConfig(ctx context.Context, dependencies solrConfigDependencies, server, version string) (tree solrConfigTree, err error) {
	suffix, err := secureSolrConfigSuffix()
	if err != nil {
		return nil, err
	}
	zipPath := path.Join("/tmp", "sitectl-solr-config-"+suffix+".zip")
	defer func() {
		_, cleanupErr := dependencies.Runtime.ExecCapture(ctx, dependencies.DrupalContainer, "", []string{"rm", "-f", zipPath})
		if cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove temporary Drupal Solr config %q: %w", zipPath, cleanupErr))
		}
	}()

	_, err = dependencies.Runtime.ExecCapture(ctx, dependencies.DrupalContainer, dependencies.DrupalWorkingDir, []string{
		"drush",
		"-y",
		"search-api-solr:get-server-config",
		server,
		zipPath,
		version,
	})
	if err != nil {
		return nil, fmt.Errorf("generate Solr config with Drupal server %q: %w", server, err)
	}

	archive, err := dependencies.Runtime.CopyFrom(ctx, dependencies.DrupalContainer, zipPath)
	if err != nil {
		return nil, fmt.Errorf("copy generated Solr config from Drupal: %w", err)
	}
	zipData, readErr := readSingleRegularFileTar(archive, path.Base(zipPath), maxSolrConfigZipBytes)
	closeErr := archive.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read generated Solr config archive: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close generated Solr config archive: %w", closeErr)
	}

	tree, err = readSolrConfigZip(zipData)
	if err != nil {
		return nil, fmt.Errorf("validate generated Solr config: %w", err)
	}
	return tree, nil
}

func secureSolrConfigSuffix() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate temporary Solr config name: %w", err)
	}
	return hex.EncodeToString(random), nil
}

func readHostSolrConfig(ctx context.Context, host solrConfigHostStore, root string) (solrConfigTree, bool, error) {
	tree, err := host.ReadTree(ctx, root)
	if err == nil {
		return tree, true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read tracked Solr config %q: %w", root, err)
}

func readRuntimeSolrConfig(ctx context.Context, runtime solrConfigContainerRuntime, container, sourcePath string) (solrConfigTree, bool, error) {
	archive, err := runtime.CopyFrom(ctx, container, sourcePath)
	if err != nil {
		if containerPathMissing(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("copy running Solr config %q: %w", sourcePath, err)
	}
	tree, readErr := readRegularTreeTar(archive, path.Base(sourcePath))
	closeErr := archive.Close()
	if readErr != nil {
		return nil, false, fmt.Errorf("validate running Solr config %q: %w", sourcePath, readErr)
	}
	if closeErr != nil {
		return nil, false, fmt.Errorf("close running Solr config archive: %w", closeErr)
	}
	return tree, true, nil
}

func containerPathMissing(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "could not find the file") ||
		strings.Contains(message, "no such file or directory")
}

func reconcileSolrCore(ctx context.Context, runtime solrConfigContainerRuntime, container, core string) (string, error) {
	exists, err := solrCoreExists(ctx, runtime, container, core)
	if err != nil {
		return "", err
	}
	action := "CREATE"
	parameters := [][2]string{
		{"action", action},
		{"name", core},
		{"instanceDir", core},
		{"config", "solrconfig.xml"},
		{"dataDir", "data"},
		{"wt", "json"},
	}
	result := "created"
	if exists {
		action = "RELOAD"
		parameters = [][2]string{{"action", action}, {"core", core}, {"wt", "json"}}
		result = "reloaded"
	}
	output, err := runSolrAPI(ctx, runtime, container, "/admin/cores", parameters)
	if err != nil {
		return "", fmt.Errorf("%s Solr core %q: %w", strings.ToLower(action), core, err)
	}
	if err := validateSolrAPIResponse(output); err != nil {
		return "", fmt.Errorf("%s Solr core %q: %w", strings.ToLower(action), core, err)
	}
	return result, nil
}

func solrCoreExists(ctx context.Context, runtime solrConfigContainerRuntime, container, core string) (bool, error) {
	output, err := runSolrAPI(ctx, runtime, container, "/admin/cores", [][2]string{
		{"action", "STATUS"},
		{"core", core},
		{"indexInfo", "false"},
		{"wt", "json"},
	})
	if err != nil {
		return false, fmt.Errorf("query Solr core %q: %w", core, err)
	}
	var response struct {
		ResponseHeader *struct {
			Status int `json:"status"`
		} `json:"responseHeader"`
		Status map[string]json.RawMessage `json:"status"`
		Error  json.RawMessage            `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return false, fmt.Errorf("decode Solr core status response: %w", err)
	}
	if err := validateParsedSolrAPIResponse(response.ResponseHeader, response.Error); err != nil {
		return false, err
	}
	if response.Status == nil {
		return false, fmt.Errorf("solr core status response did not include status")
	}
	value, ok := response.Status[core]
	if !ok || len(value) == 0 || string(value) == "null" {
		return false, nil
	}
	var coreStatus map[string]json.RawMessage
	if err := json.Unmarshal(value, &coreStatus); err != nil {
		return false, fmt.Errorf("decode Solr core %q status: %w", core, err)
	}
	return len(coreStatus) > 0, nil
}

func runSolrAPI(ctx context.Context, runtime solrConfigContainerRuntime, container, endpoint string, parameters [][2]string) (string, error) {
	if !strings.HasPrefix(endpoint, "/") || strings.Contains(endpoint, "..") {
		return "", fmt.Errorf("invalid Solr API endpoint %q", endpoint)
	}
	argv := []string{"curl", "--fail", "--silent", "--show-error", "--max-time", "30", "--get"}
	for _, parameter := range parameters {
		if parameter[0] == "" || strings.ContainsAny(parameter[0], "=&") {
			return "", fmt.Errorf("invalid Solr API parameter name %q", parameter[0])
		}
		argv = append(argv, "--data-urlencode", parameter[0]+"="+parameter[1])
	}
	argv = append(argv, solrAPIBaseURL+endpoint)
	return runtime.ExecCapture(ctx, container, "", argv)
}

func validateSolrAPIResponse(output string) error {
	var response struct {
		ResponseHeader *struct {
			Status int `json:"status"`
		} `json:"responseHeader"`
		Error json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		return fmt.Errorf("decode Solr API response: %w", err)
	}
	return validateParsedSolrAPIResponse(response.ResponseHeader, response.Error)
}

func validateParsedSolrAPIResponse(responseHeader *struct {
	Status int `json:"status"`
}, apiError json.RawMessage) error {
	if len(apiError) > 0 && string(apiError) != "null" && string(apiError) != "{}" {
		var detail struct {
			Message string `json:"msg"`
			Code    int    `json:"code"`
		}
		if err := json.Unmarshal(apiError, &detail); err == nil && detail.Message != "" {
			return fmt.Errorf("solr API error %d: %s", detail.Code, detail.Message)
		}
		return fmt.Errorf("solr API returned an error")
	}
	if responseHeader == nil {
		return fmt.Errorf("solr API response did not include responseHeader")
	}
	if responseHeader.Status != 0 {
		return fmt.Errorf("solr API returned status %d", responseHeader.Status)
	}
	return nil
}

func runDrupalSearchAPIIndexCommand(ctx context.Context, dependencies solrConfigDependencies, command string) error {
	if command != "search-api:reset-tracker" && command != "search-api:index" {
		return fmt.Errorf("unsupported Drupal Search API index command %q", command)
	}
	if _, err := dependencies.Runtime.ExecCapture(ctx, dependencies.DrupalContainer, dependencies.DrupalWorkingDir, []string{"drush", "-y", command}); err != nil {
		return fmt.Errorf("run drush %s: %w", command, err)
	}
	return nil
}

func drupalSearchAPISolrEnabled(ctx context.Context, dependencies solrConfigDependencies) (bool, error) {
	output, err := dependencies.Runtime.ExecCapture(ctx, dependencies.DrupalContainer, dependencies.DrupalWorkingDir, []string{
		"drush",
		"pm:list",
		"--type=module",
		"--status=enabled",
		"--no-core",
		"--format=json",
	})
	if err != nil {
		return false, fmt.Errorf("check whether Drupal module search_api_solr is enabled: %w", err)
	}
	var modules any
	decoder := json.NewDecoder(strings.NewReader(output))
	decoder.UseNumber()
	if err := decoder.Decode(&modules); err != nil {
		return false, fmt.Errorf("decode enabled Drupal modules: %w", err)
	}
	return structuredValueContainsString(modules, "search_api_solr"), nil
}

func structuredValueContainsString(value any, wanted string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == wanted || structuredValueContainsString(child, wanted) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if structuredValueContainsString(child, wanted) {
				return true
			}
		}
	case string:
		return typed == wanted
	}
	return false
}

func writeSolrConfigResult(cmd *cobra.Command, result solrConfigRefreshResult) error {
	if result.Skipped {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s; no changes made.\n", result.SkipReason)
		return err
	}
	if !result.HostUpdated && !result.RuntimeUpdated {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Solr config is already current for version %s; no changes made.\n", result.Version)
		return err
	}
	if result.HostUpdated {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Updated tracked Solr config at %s.\n", result.OutputPath); err != nil {
			return err
		}
	}
	if result.RuntimeUpdated {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Updated running Solr config and %s core %s.\n", result.CoreAction, result.Core); err != nil {
			return err
		}
	}
	if result.Reindexed {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Reindexed all Drupal Search API indexes."); err != nil {
			return err
		}
	}
	return nil
}

func solrConfigTreesEqual(left, right solrConfigTree) bool {
	if len(left) != len(right) {
		return false
	}
	for name, leftData := range left {
		rightData, ok := right[name]
		if !ok || !bytes.Equal(leftData, rightData) {
			return false
		}
	}
	return true
}

func readSingleRegularFileTar(reader io.Reader, expectedBase string, maximum int64) ([]byte, error) {
	tarReader := tar.NewReader(reader)
	var result []byte
	entries := 0
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		entries++
		if entries > maxSolrConfigArchiveEntries {
			return nil, fmt.Errorf("archive contains more than %d entries", maxSolrConfigArchiveEntries)
		}
		name, err := cleanArchivePath(header.Name)
		if err != nil {
			return nil, err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			if name != expectedBase || result != nil {
				return nil, fmt.Errorf("archive contains unexpected regular file %q", name)
			}
			if header.Size < 0 || header.Size > maximum {
				return nil, fmt.Errorf("archive file %q exceeds %d bytes", name, maximum)
			}
			data, err := readLimitedExact(tarReader, header.Size, maximum)
			if err != nil {
				return nil, fmt.Errorf("read archive file %q: %w", name, err)
			}
			result = data
		default:
			return nil, fmt.Errorf("archive entry %q is not a regular file or directory", name)
		}
	}
	if result == nil {
		return nil, fmt.Errorf("archive did not contain %q", expectedBase)
	}
	return result, nil
}

func readRegularTreeTar(reader io.Reader, expectedRoot string) (solrConfigTree, error) {
	tarReader := tar.NewReader(reader)
	tree := solrConfigTree{}
	entries := 0
	var total int64
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		entries++
		if entries > maxSolrConfigArchiveEntries {
			return nil, fmt.Errorf("archive contains more than %d entries", maxSolrConfigArchiveEntries)
		}
		name, err := cleanArchivePath(header.Name)
		if err != nil {
			return nil, err
		}
		parts := strings.Split(name, "/")
		if len(parts) == 0 || parts[0] != expectedRoot {
			return nil, fmt.Errorf("archive entry %q is outside expected root %q", name, expectedRoot)
		}
		relative := strings.Join(parts[1:], "/")
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg:
			if relative == "" {
				return nil, fmt.Errorf("archive root %q is not a directory", expectedRoot)
			}
			if len(tree) >= maxSolrConfigFiles {
				return nil, fmt.Errorf("archive contains more than %d files", maxSolrConfigFiles)
			}
			if header.Size < 0 || header.Size > maxSolrConfigFileBytes || total > maxSolrConfigExtractedBytes-header.Size {
				return nil, fmt.Errorf("archive file %q exceeds Solr config size limits", name)
			}
			if _, duplicate := tree[relative]; duplicate {
				return nil, fmt.Errorf("archive contains duplicate file %q", relative)
			}
			data, err := readLimitedExact(tarReader, header.Size, maxSolrConfigFileBytes)
			if err != nil {
				return nil, fmt.Errorf("read archive file %q: %w", name, err)
			}
			tree[relative] = data
			total += int64(len(data))
		default:
			return nil, fmt.Errorf("archive entry %q is not a regular file or directory", name)
		}
	}
	if err := validateSolrConfigTreeShape(tree); err != nil {
		return nil, err
	}
	return tree, nil
}

func readSolrConfigZip(data []byte) (solrConfigTree, error) {
	if int64(len(data)) > maxSolrConfigZipBytes {
		return nil, fmt.Errorf("zip exceeds %d bytes", maxSolrConfigZipBytes)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(reader.File) > maxSolrConfigArchiveEntries {
		return nil, fmt.Errorf("zip contains more than %d entries", maxSolrConfigArchiveEntries)
	}
	tree := solrConfigTree{}
	directories := map[string]struct{}{}
	var total int64
	for _, file := range reader.File {
		name, err := cleanArchivePath(file.Name)
		if err != nil {
			return nil, err
		}
		mode := file.Mode()
		if mode&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("zip entry %q is a symbolic link", name)
		}
		if file.FileInfo().IsDir() || strings.HasSuffix(file.Name, "/") {
			if file.UncompressedSize64 != 0 {
				return nil, fmt.Errorf("zip directory %q contains data", name)
			}
			directories[name] = struct{}{}
			continue
		}
		if mode.Type() != 0 {
			return nil, fmt.Errorf("zip entry %q is not a regular file", name)
		}
		if len(tree) >= maxSolrConfigFiles {
			return nil, fmt.Errorf("zip contains more than %d files", maxSolrConfigFiles)
		}
		if file.UncompressedSize64 > uint64(maxSolrConfigFileBytes) || uint64(total) > uint64(maxSolrConfigExtractedBytes)-file.UncompressedSize64 {
			return nil, fmt.Errorf("zip file %q exceeds Solr config size limits", name)
		}
		if _, duplicate := tree[name]; duplicate {
			return nil, fmt.Errorf("zip contains duplicate file %q", name)
		}
		entry, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("open zip file %q: %w", name, err)
		}
		fileData, readErr := readLimitedExact(entry, int64(file.UncompressedSize64), maxSolrConfigFileBytes)
		closeErr := entry.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read zip file %q: %w", name, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close zip file %q: %w", name, closeErr)
		}
		tree[name] = fileData
		total += int64(len(fileData))
	}
	for directory := range directories {
		if _, collision := tree[directory]; collision {
			return nil, fmt.Errorf("zip path %q is both a file and directory", directory)
		}
	}
	if err := validateSolrConfigTreeShape(tree); err != nil {
		return nil, err
	}
	if _, ok := tree["solrconfig.xml"]; !ok {
		return nil, fmt.Errorf("zip does not contain solrconfig.xml at its root")
	}
	if _, schemaXML := tree["schema.xml"]; !schemaXML {
		if _, managedSchema := tree["managed-schema"]; !managedSchema {
			return nil, fmt.Errorf("zip does not contain schema.xml or managed-schema at its root")
		}
	}
	return tree, nil
}

func cleanArchivePath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, '\x00') || strings.Contains(name, "\\") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return "", fmt.Errorf("unsafe archive path %q", name)
		}
	}
	trimmed := strings.TrimSuffix(name, "/")
	firstComponent := strings.SplitN(trimmed, "/", 2)[0]
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || strings.Contains(firstComponent, ":") {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || cleaned != trimmed {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return cleaned, nil
}

func validateSolrConfigTreeShape(tree solrConfigTree) error {
	for name := range tree {
		cleaned, err := cleanArchivePath(name)
		if err != nil {
			return err
		}
		if cleaned != name {
			return fmt.Errorf("non-canonical Solr config path %q", name)
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, collision := tree[parent]; collision {
				return fmt.Errorf("solr config path %q is nested beneath file %q", name, parent)
			}
		}
	}
	return nil
}

func readLimitedExact(reader io.Reader, expected, maximum int64) ([]byte, error) {
	if expected < 0 || expected > maximum {
		return nil, fmt.Errorf("entry size %d exceeds %d bytes", expected, maximum)
	}
	data, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maximum {
		return nil, fmt.Errorf("entry exceeds %d bytes", maximum)
	}
	if int64(len(data)) != expected {
		return nil, fmt.Errorf("entry contains %d bytes, expected %d", len(data), expected)
	}
	return data, nil
}

func buildSolrConfigTreeTar(root string, tree solrConfigTree) ([]byte, error) {
	if _, err := cleanArchivePath(root); err != nil || strings.Contains(root, "/") {
		return nil, fmt.Errorf("invalid Solr config staging root %q", root)
	}
	if err := validateSolrConfigTreeShape(tree); err != nil {
		return nil, err
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	writeHeader := func(header *tar.Header) error {
		header.Uid = 100
		header.Gid = 1000
		return writer.WriteHeader(header)
	}
	if err := writeHeader(&tar.Header{Name: root + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		return nil, err
	}

	directories := map[string]struct{}{}
	files := make([]string, 0, len(tree))
	for name := range tree {
		files = append(files, name)
		for directory := path.Dir(name); directory != "."; directory = path.Dir(directory) {
			directories[directory] = struct{}{}
		}
	}
	sortedDirectories := make([]string, 0, len(directories))
	for directory := range directories {
		sortedDirectories = append(sortedDirectories, directory)
	}
	sort.Strings(sortedDirectories)
	for _, directory := range sortedDirectories {
		if err := writeHeader(&tar.Header{Name: path.Join(root, directory) + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	for _, name := range files {
		data := tree[name]
		if err := writeHeader(&tar.Header{Name: path.Join(root, name), Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))}); err != nil {
			return nil, err
		}
		if _, err := writer.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}
