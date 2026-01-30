package drupal

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	cacheDir     = ".sitectl/cache"
	cacheVersion = "v1" // bump this to invalidate all caches on schema change
)

// CachedBundleConfig is the serialized format for disk cache
type CachedBundleConfig struct {
	Version    string             `json:"version"`
	ConfigPath string             `json:"config_path"`
	CachedAt   time.Time          `json:"cached_at"`
	Bundles    []BundleDefinition `json:"bundles"`
}

// getCacheDir returns the cache directory path, creating it if needed
func getCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}

	dir := filepath.Join(home, cacheDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating cache dir: %w", err)
	}

	return dir, nil
}

// getCacheFilename generates a cache filename from the config path
func getCacheFilename(configPath string) string {
	hash := md5.Sum([]byte(configPath))
	return fmt.Sprintf("bundles-%s-%s.json", cacheVersion, hex.EncodeToString(hash[:]))
}

// getNewestFileTime walks a directory and returns the newest modification time
func getNewestFileTime(path string) (time.Time, error) {
	var newest time.Time

	info, err := os.Stat(path)
	if err != nil {
		return newest, err
	}

	// If it's a single file, return its mod time
	if !info.IsDir() {
		return info.ModTime(), nil
	}

	// Walk directory for newest YAML file
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Only consider YAML files
		if !strings.HasSuffix(p, ".yaml") && !strings.HasSuffix(p, ".yml") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})

	return newest, err
}

// LoadCachedRegistry attempts to load a cached registry for the given config path.
// Returns nil if cache doesn't exist or is stale.
func LoadCachedRegistry(configPath string) (*BundleRegistry, error) {
	cacheDir, err := getCacheDir()
	if err != nil {
		return nil, err
	}

	cachePath := filepath.Join(cacheDir, getCacheFilename(configPath))

	// Check if cache file exists
	cacheInfo, err := os.Stat(cachePath)
	if os.IsNotExist(err) {
		return nil, nil // No cache, not an error
	}
	if err != nil {
		return nil, err
	}

	// Check if any source file is newer than cache
	newestSource, err := getNewestFileTime(configPath)
	if err != nil {
		return nil, err
	}

	if newestSource.After(cacheInfo.ModTime()) {
		// Cache is stale
		return nil, nil
	}

	// Load cache
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	var cached CachedBundleConfig
	if err := json.Unmarshal(data, &cached); err != nil {
		// Corrupted cache, ignore
		return nil, nil
	}

	// Verify cache version
	if cached.Version != cacheVersion {
		return nil, nil
	}

	// Rebuild registry from cached data
	registry := NewBundleRegistry()
	for i := range cached.Bundles {
		registry.RegisterBundle(&cached.Bundles[i])
	}

	return registry, nil
}

// SaveRegistryCache saves the bundles from a registry to disk cache
func SaveRegistryCache(configPath string, registry *BundleRegistry) error {
	cacheDir, err := getCacheDir()
	if err != nil {
		return err
	}

	// Collect all bundles from all entity types
	var bundles []BundleDefinition
	for _, et := range registry.ListAllEntityTypes() {
		for _, name := range registry.ListBundles(et) {
			if def, ok := registry.GetBundle(et, name); ok {
				bundles = append(bundles, *def)
			}
		}
	}

	cached := CachedBundleConfig{
		Version:    cacheVersion,
		ConfigPath: configPath,
		CachedAt:   time.Now(),
		Bundles:    bundles,
	}

	data, err := json.MarshalIndent(cached, "", "  ")
	if err != nil {
		return err
	}

	cachePath := filepath.Join(cacheDir, getCacheFilename(configPath))
	return os.WriteFile(cachePath, data, 0644)
}

// ClearCache removes all cached bundle registries
func ClearCache() error {
	cacheDir, err := getCacheDir()
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "bundles-") && strings.HasSuffix(entry.Name(), ".json") {
			if err := os.Remove(filepath.Join(cacheDir, entry.Name())); err != nil {
				return err
			}
		}
	}

	return nil
}
