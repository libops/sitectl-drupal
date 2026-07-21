package cmd

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
)

const (
	pendingSolrConfigVersion   = 1
	pendingSolrConfigDirectory = ".sitectl-solr-config-pending"
	pendingSolrConfigStateFile = "state.json"
	maxPendingSolrStateBytes   = 16 << 10
)

type pendingSolrConfigState struct {
	Version         int    `json:"version"`
	DesiredSHA256   string `json:"desired_sha256"`
	Reindex         bool   `json:"reindex"`
	CoreReconciled  bool   `json:"core_reconciled"`
	CoreAction      string `json:"core_action,omitempty"`
	TrackersReset   bool   `json:"trackers_reset,omitempty"`
	ReindexComplete bool   `json:"reindex_complete,omitempty"`
}

type pendingSolrConfigOutcome struct {
	Completed  bool
	CoreAction string
	Reindexed  bool
}

type pendingSolrConfigPaths struct {
	Core      string
	Conf      string
	Pending   string
	State     string
	New       string
	Previous  string
	Abandoned string
}

func solrPendingPaths(core string) pendingSolrConfigPaths {
	corePath := path.Join(solrContainerRoot, core)
	pending := path.Join(corePath, pendingSolrConfigDirectory)
	return pendingSolrConfigPaths{
		Core:      corePath,
		Conf:      path.Join(corePath, "conf"),
		Pending:   pending,
		State:     path.Join(pending, pendingSolrConfigStateFile),
		New:       path.Join(pending, "new"),
		Previous:  path.Join(pending, "previous"),
		Abandoned: path.Join(pending, "abandoned"),
	}
}

func resumePendingSolrConfig(ctx context.Context, dependencies solrConfigDependencies, core string, generated solrConfigTree, requestedReindex bool) (pendingSolrConfigOutcome, error) {
	paths := solrPendingPaths(core)
	state, found, err := readPendingSolrConfigState(ctx, dependencies.Runtime, dependencies.SolrContainer, paths.State)
	if err != nil {
		return pendingSolrConfigOutcome{}, err
	}
	if !found {
		pendingExists, err := runtimePathExists(ctx, dependencies.Runtime, dependencies.SolrContainer, paths.Pending)
		if err != nil {
			return pendingSolrConfigOutcome{}, err
		}
		if !pendingExists {
			return pendingSolrConfigOutcome{}, nil
		}
		return pendingSolrConfigOutcome{}, fmt.Errorf("pending Solr config at %q is missing its durable state marker; verify no refresh is running before removing it", paths.Pending)
	}

	desiredSHA256 := solrConfigTreeSHA256(generated)
	if state.CoreReconciled {
		conf, exists, err := readRuntimeSolrConfig(ctx, dependencies.Runtime, dependencies.SolrContainer, paths.Conf)
		if err != nil {
			return pendingSolrConfigOutcome{}, err
		}
		activeMatchesState := exists && solrConfigTreeSHA256(conf) == state.DesiredSHA256
		if state.DesiredSHA256 != desiredSHA256 || !activeMatchesState {
			return restartPendingSolrConfig(ctx, dependencies, paths, state, generated, requestedReindex, activeMatchesState)
		}
		if requestedReindex && !state.Reindex {
			state.Reindex = true
			if err := writePendingSolrConfigState(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, state); err != nil {
				return pendingSolrConfigOutcome{}, fmt.Errorf("record requested Drupal Search API reindex: %w", err)
			}
		}
		return finishReconciledPendingSolrConfig(ctx, dependencies, paths, state)
	}
	if state.DesiredSHA256 != desiredSHA256 {
		return restartPendingSolrConfig(ctx, dependencies, paths, state, generated, requestedReindex, false)
	}
	if requestedReindex && !state.Reindex {
		state.Reindex = true
		if err := writePendingSolrConfigState(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, state); err != nil {
			return pendingSolrConfigOutcome{}, fmt.Errorf("record requested Drupal Search API reindex: %w", err)
		}
	}
	return completePendingSolrConfig(ctx, dependencies, paths, state, generated)
}

func restartPendingSolrConfig(ctx context.Context, dependencies solrConfigDependencies, paths pendingSolrConfigPaths, previousState pendingSolrConfigState, generated solrConfigTree, requestedReindex, activeMatchesState bool) (pendingSolrConfigOutcome, error) {
	if !previousState.CoreReconciled {
		if err := rollbackPendingSolrConfig(ctx, dependencies.Runtime, dependencies.SolrContainer, paths); err != nil {
			return pendingSolrConfigOutcome{}, fmt.Errorf("restore previous Solr config before superseding pending config: %w", err)
		}
	} else if activeMatchesState {
		if err := removeRuntimePath(ctx, dependencies.Runtime, dependencies.SolrContainer, paths.Previous); err != nil {
			return pendingSolrConfigOutcome{}, fmt.Errorf("remove superseded Solr config rollback copy: %w", err)
		}
	}
	for _, stalePath := range []string{paths.New, paths.Abandoned} {
		if err := removeRuntimePath(ctx, dependencies.Runtime, dependencies.SolrContainer, stalePath); err != nil {
			return pendingSolrConfigOutcome{}, fmt.Errorf("remove superseded pending Solr config path %q: %w", stalePath, err)
		}
	}

	state := pendingSolrConfigState{
		Version:       pendingSolrConfigVersion,
		DesiredSHA256: solrConfigTreeSHA256(generated),
		Reindex:       previousState.Reindex || requestedReindex,
	}
	if err := writePendingSolrConfigState(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, state); err != nil {
		return pendingSolrConfigOutcome{}, fmt.Errorf("record superseding pending Solr config: %w", err)
	}
	if err := stagePendingSolrConfig(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, generated); err != nil {
		return pendingSolrConfigOutcome{}, err
	}
	return completePendingSolrConfig(ctx, dependencies, paths, state, generated)
}

func startPendingSolrConfig(ctx context.Context, dependencies solrConfigDependencies, core string, generated solrConfigTree, reindex bool) (pendingSolrConfigOutcome, error) {
	paths := solrPendingPaths(core)
	pendingExists, err := runtimePathExists(ctx, dependencies.Runtime, dependencies.SolrContainer, paths.Pending)
	if err != nil {
		return pendingSolrConfigOutcome{}, err
	}
	if pendingExists {
		return pendingSolrConfigOutcome{}, fmt.Errorf("pending Solr config already exists at %q", paths.Pending)
	}
	if _, err := dependencies.Runtime.ExecCapture(ctx, dependencies.SolrContainer, "", []string{"mkdir", "-p", paths.Core}); err != nil {
		return pendingSolrConfigOutcome{}, fmt.Errorf("create Solr core directory %q: %w", paths.Core, err)
	}
	if _, err := dependencies.Runtime.ExecCapture(ctx, dependencies.SolrContainer, "", []string{"mkdir", paths.Pending}); err != nil {
		return pendingSolrConfigOutcome{}, fmt.Errorf("acquire pending Solr config transaction at %q: %w", paths.Pending, err)
	}
	state := pendingSolrConfigState{
		Version:       pendingSolrConfigVersion,
		DesiredSHA256: solrConfigTreeSHA256(generated),
		Reindex:       reindex,
	}
	if err := writePendingSolrConfigState(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, state); err != nil {
		return pendingSolrConfigOutcome{}, err
	}
	if err := stagePendingSolrConfig(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, generated); err != nil {
		return pendingSolrConfigOutcome{}, err
	}
	return completePendingSolrConfig(ctx, dependencies, paths, state, generated)
}

func completePendingSolrConfig(ctx context.Context, dependencies solrConfigDependencies, paths pendingSolrConfigPaths, state pendingSolrConfigState, generated solrConfigTree) (pendingSolrConfigOutcome, error) {
	hadPrevious, err := activatePendingSolrConfig(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, state.DesiredSHA256, generated)
	if err != nil {
		if hadPrevious {
			recoveryErr := rollbackAndReconcilePreviousSolrConfig(ctx, dependencies, paths)
			return pendingSolrConfigOutcome{}, errors.Join(err, recoveryErr)
		}
		return pendingSolrConfigOutcome{}, err
	}
	action, reconcileErr := reconcileSolrCore(ctx, dependencies.Runtime, dependencies.SolrContainer, path.Base(paths.Core))
	if reconcileErr != nil {
		if hadPrevious {
			recoveryErr := rollbackAndReconcilePreviousSolrConfig(ctx, dependencies, paths)
			return pendingSolrConfigOutcome{}, errors.Join(reconcileErr, recoveryErr)
		}
		return pendingSolrConfigOutcome{}, reconcileErr
	}

	state.CoreReconciled = true
	state.CoreAction = action
	if err := writePendingSolrConfigState(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, state); err != nil {
		if hadPrevious {
			recoveryErr := rollbackAndReconcilePreviousSolrConfig(ctx, dependencies, paths)
			return pendingSolrConfigOutcome{}, errors.Join(fmt.Errorf("record accepted Solr config: %w", err), recoveryErr)
		}
		return pendingSolrConfigOutcome{}, fmt.Errorf("record accepted Solr config: %w", err)
	}
	return finishReconciledPendingSolrConfig(ctx, dependencies, paths, state)
}

func finishReconciledPendingSolrConfig(ctx context.Context, dependencies solrConfigDependencies, paths pendingSolrConfigPaths, state pendingSolrConfigState) (pendingSolrConfigOutcome, error) {
	conf, exists, err := readRuntimeSolrConfig(ctx, dependencies.Runtime, dependencies.SolrContainer, paths.Conf)
	if err != nil {
		return pendingSolrConfigOutcome{}, err
	}
	if !exists || solrConfigTreeSHA256(conf) != state.DesiredSHA256 {
		return pendingSolrConfigOutcome{}, fmt.Errorf("accepted pending Solr config no longer matches conf at %q", paths.Conf)
	}
	if err := removeRuntimePath(ctx, dependencies.Runtime, dependencies.SolrContainer, paths.Previous); err != nil {
		return pendingSolrConfigOutcome{}, fmt.Errorf("remove accepted previous Solr conf: %w", err)
	}
	outcome := pendingSolrConfigOutcome{Completed: true, CoreAction: state.CoreAction}
	if state.Reindex {
		if !state.TrackersReset {
			if err := runDrupalSearchAPIIndexCommand(ctx, dependencies, "search-api:reset-tracker"); err != nil {
				return pendingSolrConfigOutcome{}, err
			}
			state.TrackersReset = true
			if err := writePendingSolrConfigState(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, state); err != nil {
				return pendingSolrConfigOutcome{}, fmt.Errorf("record reset Drupal Search API trackers: %w", err)
			}
		}
		if !state.ReindexComplete {
			if err := runDrupalSearchAPIIndexCommand(ctx, dependencies, "search-api:index"); err != nil {
				return pendingSolrConfigOutcome{}, err
			}
			state.ReindexComplete = true
			if err := writePendingSolrConfigState(ctx, dependencies.Runtime, dependencies.SolrContainer, paths, state); err != nil {
				return pendingSolrConfigOutcome{}, fmt.Errorf("record completed Drupal Search API reindex: %w", err)
			}
		}
		outcome.Reindexed = true
	}
	if err := removeRuntimePath(ctx, dependencies.Runtime, dependencies.SolrContainer, paths.Pending); err != nil {
		return pendingSolrConfigOutcome{}, fmt.Errorf("remove completed pending Solr config marker: %w", err)
	}
	return outcome, nil
}

func activatePendingSolrConfig(ctx context.Context, runtime solrConfigContainerRuntime, container string, paths pendingSolrConfigPaths, desiredSHA256 string, generated solrConfigTree) (bool, error) {
	conf, confExists, err := readRuntimeSolrConfig(ctx, runtime, container, paths.Conf)
	if err != nil {
		return false, err
	}
	previousExists, err := runtimeTreeExists(ctx, runtime, container, paths.Previous)
	if err != nil {
		return false, err
	}
	if confExists && solrConfigTreeSHA256(conf) == desiredSHA256 {
		return previousExists, nil
	}

	staged, stagedExists, err := readRuntimeSolrConfig(ctx, runtime, container, paths.New)
	if err != nil {
		return false, err
	}
	if !stagedExists || solrConfigTreeSHA256(staged) != desiredSHA256 {
		if solrConfigTreeSHA256(generated) != desiredSHA256 {
			return false, fmt.Errorf("cannot recreate pending Solr config because generated config changed")
		}
		if err := stagePendingSolrConfig(ctx, runtime, container, paths, generated); err != nil {
			return false, err
		}
	}

	if !previousExists && confExists {
		if _, err := runtime.ExecCapture(ctx, container, "", []string{"mv", paths.Conf, paths.Previous}); err != nil {
			return false, fmt.Errorf("retain previous Solr conf: %w", err)
		}
		previousExists = true
	} else if previousExists && confExists {
		if err := removeRuntimePath(ctx, runtime, container, paths.Abandoned); err != nil {
			return false, err
		}
		if _, err := runtime.ExecCapture(ctx, container, "", []string{"mv", paths.Conf, paths.Abandoned}); err != nil {
			return false, fmt.Errorf("retain interrupted Solr conf: %w", err)
		}
	}
	if _, err := runtime.ExecCapture(ctx, container, "", []string{"mv", paths.New, paths.Conf}); err != nil {
		if previousExists {
			_, restoreErr := runtime.ExecCapture(ctx, container, "", []string{"mv", paths.Previous, paths.Conf})
			return true, errors.Join(fmt.Errorf("activate pending Solr conf: %w", err), restoreErr)
		}
		return false, fmt.Errorf("activate pending Solr conf: %w", err)
	}
	active, activeExists, err := readRuntimeSolrConfig(ctx, runtime, container, paths.Conf)
	if err != nil {
		return previousExists, err
	}
	if !activeExists || solrConfigTreeSHA256(active) != desiredSHA256 {
		return previousExists, fmt.Errorf("activated Solr conf does not match pending config")
	}
	return previousExists, nil
}

func stagePendingSolrConfig(ctx context.Context, runtime solrConfigContainerRuntime, container string, paths pendingSolrConfigPaths, tree solrConfigTree) error {
	if err := removeRuntimePath(ctx, runtime, container, paths.New); err != nil {
		return fmt.Errorf("remove incomplete pending Solr conf: %w", err)
	}
	archive, err := buildSolrConfigTreeTar(path.Base(paths.New), tree)
	if err != nil {
		return err
	}
	if err := runtime.CopyTo(ctx, container, paths.Pending, bytes.NewReader(archive)); err != nil {
		return fmt.Errorf("stage pending Solr conf: %w", err)
	}
	staged, exists, err := readRuntimeSolrConfig(ctx, runtime, container, paths.New)
	if err != nil {
		return err
	}
	if !exists || !solrConfigTreesEqual(staged, tree) {
		return fmt.Errorf("staged pending Solr conf did not match generated config")
	}
	return nil
}

func rollbackPendingSolrConfig(ctx context.Context, runtime solrConfigContainerRuntime, container string, paths pendingSolrConfigPaths) error {
	previousExists, err := runtimeTreeExists(ctx, runtime, container, paths.Previous)
	if err != nil {
		return err
	}
	if !previousExists {
		return nil
	}
	confExists, err := runtimeTreeExists(ctx, runtime, container, paths.Conf)
	if err != nil {
		return err
	}
	if confExists {
		if err := removeRuntimePath(ctx, runtime, container, paths.New); err != nil {
			return err
		}
		if _, err := runtime.ExecCapture(ctx, container, "", []string{"mv", paths.Conf, paths.New}); err != nil {
			return fmt.Errorf("retain rejected Solr conf for retry: %w", err)
		}
	}
	if _, err := runtime.ExecCapture(ctx, container, "", []string{"mv", paths.Previous, paths.Conf}); err != nil {
		return fmt.Errorf("restore previous Solr conf: %w", err)
	}
	return nil
}

func rollbackAndReconcilePreviousSolrConfig(ctx context.Context, dependencies solrConfigDependencies, paths pendingSolrConfigPaths) error {
	if err := rollbackPendingSolrConfig(ctx, dependencies.Runtime, dependencies.SolrContainer, paths); err != nil {
		return fmt.Errorf("restore previous Solr config: %w", err)
	}
	if _, err := reconcileSolrCore(ctx, dependencies.Runtime, dependencies.SolrContainer, path.Base(paths.Core)); err != nil {
		return fmt.Errorf("reload restored previous Solr config: %w", err)
	}
	return nil
}

func readPendingSolrConfigState(ctx context.Context, runtime solrConfigContainerRuntime, container, statePath string) (pendingSolrConfigState, bool, error) {
	archive, err := runtime.CopyFrom(ctx, container, statePath)
	if err != nil {
		if containerPathMissing(err) {
			return pendingSolrConfigState{}, false, nil
		}
		return pendingSolrConfigState{}, false, fmt.Errorf("read pending Solr config state: %w", err)
	}
	data, readErr := readSingleRegularFileTar(archive, path.Base(statePath), maxPendingSolrStateBytes)
	closeErr := archive.Close()
	if readErr != nil {
		return pendingSolrConfigState{}, false, fmt.Errorf("validate pending Solr config state archive: %w", readErr)
	}
	if closeErr != nil {
		return pendingSolrConfigState{}, false, closeErr
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state pendingSolrConfigState
	if err := decoder.Decode(&state); err != nil {
		return pendingSolrConfigState{}, false, fmt.Errorf("decode pending Solr config state: %w", err)
	}
	if err := validatePendingSolrConfigState(state); err != nil {
		return pendingSolrConfigState{}, false, err
	}
	return state, true, nil
}

func validatePendingSolrConfigState(state pendingSolrConfigState) error {
	if state.Version != pendingSolrConfigVersion {
		return fmt.Errorf("unsupported pending Solr config state version %d", state.Version)
	}
	digest, err := hex.DecodeString(state.DesiredSHA256)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("invalid pending Solr config SHA-256 %q", state.DesiredSHA256)
	}
	if state.CoreReconciled && state.CoreAction != "created" && state.CoreAction != "reloaded" {
		return fmt.Errorf("invalid reconciled Solr core action %q", state.CoreAction)
	}
	if !state.CoreReconciled && state.CoreAction != "" {
		return fmt.Errorf("pending Solr config has an action before core reconciliation")
	}
	if (state.TrackersReset || state.ReindexComplete) && (!state.Reindex || !state.CoreReconciled) {
		return fmt.Errorf("pending Solr config has reindex progress before a requested reconciled reindex")
	}
	if state.ReindexComplete && !state.TrackersReset {
		return fmt.Errorf("pending Solr config completed reindex before resetting trackers")
	}
	return nil
}

func writePendingSolrConfigState(ctx context.Context, runtime solrConfigContainerRuntime, container string, paths pendingSolrConfigPaths, state pendingSolrConfigState) error {
	if err := validatePendingSolrConfigState(state); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	suffix, err := secureSolrConfigSuffix()
	if err != nil {
		return err
	}
	temporaryName := ".state-" + suffix + ".json"
	temporaryPath := path.Join(paths.Pending, temporaryName)
	archive, err := buildSingleRuntimeFileTar(temporaryName, data)
	if err != nil {
		return err
	}
	if err := runtime.CopyTo(ctx, container, paths.Pending, bytes.NewReader(archive)); err != nil {
		return fmt.Errorf("stage pending Solr config state: %w", err)
	}
	if _, err := runtime.ExecCapture(ctx, container, "", []string{"mv", temporaryPath, paths.State}); err != nil {
		cleanupErr := removeRuntimePath(ctx, runtime, container, temporaryPath)
		return errors.Join(fmt.Errorf("publish pending Solr config state: %w", err), cleanupErr)
	}
	return nil
}

func buildSingleRuntimeFileTar(name string, data []byte) ([]byte, error) {
	if _, err := cleanArchivePath(name); err != nil || strings.Contains(name, "/") {
		return nil, fmt.Errorf("invalid runtime file name %q", name)
	}
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	if err := writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(data)), Uid: 100, Gid: 1000}); err != nil {
		return nil, err
	}
	if _, err := writer.Write(data); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return archive.Bytes(), nil
}

func runtimeTreeExists(ctx context.Context, runtime solrConfigContainerRuntime, container, sourcePath string) (bool, error) {
	_, exists, err := readRuntimeSolrConfig(ctx, runtime, container, sourcePath)
	return exists, err
}

func runtimePathExists(ctx context.Context, runtime solrConfigContainerRuntime, container, sourcePath string) (bool, error) {
	archive, err := runtime.CopyFrom(ctx, container, sourcePath)
	if err != nil {
		if containerPathMissing(err) {
			return false, nil
		}
		return false, err
	}
	if err := archive.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func removeRuntimePath(ctx context.Context, runtime solrConfigContainerRuntime, container, target string) error {
	_, err := runtime.ExecCapture(ctx, container, "", []string{"rm", "-rf", target})
	return err
}

func solrConfigTreeSHA256(tree solrConfigTree) string {
	digest := sha256.New()
	files := make([]string, 0, len(tree))
	for name := range tree {
		files = append(files, name)
	}
	sort.Strings(files)
	var length [8]byte
	for _, name := range files {
		binary.BigEndian.PutUint64(length[:], uint64(len(name)))
		_, _ = digest.Write(length[:]) // hash.Hash.Write always returns a nil error.
		_, _ = digest.Write([]byte(name))
		binary.BigEndian.PutUint64(length[:], uint64(len(tree[name])))
		_, _ = digest.Write(length[:])
		_, _ = digest.Write(tree[name])
	}
	return hex.EncodeToString(digest.Sum(nil))
}
