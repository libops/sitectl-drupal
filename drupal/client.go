package drupal

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

// Client provides access to Drupal APIs with bundle configuration support.
// Plugins should create their own NewClient wrapper that pre-loads their bundle definitions.
//
// Example usage in a plugin:
//
//	//go:embed bundles/*.yaml
//	var bundleFS embed.FS
//
//	func NewIslandoraClient(opts ...drupal.ClientOption) *drupal.Client {
//	    // Start with plugin's embedded bundles
//	    allOpts := []drupal.ClientOption{
//	        drupal.WithEmbeddedBundles(bundleFS, "bundles"),
//	    }
//	    // Add user's options (may override with custom bundles)
//	    allOpts = append(allOpts, opts...)
//	    return drupal.NewClient(allOpts...)
//	}
type Client struct {
	Registry *BundleRegistry
	BaseURL  string

	// HTTP client configuration
	Username string
	Password string
}

// ClientOption configures a Client
type ClientOption func(*Client)

// WithEmbeddedBundles loads bundle definitions from an embedded filesystem.
// This is the primary mechanism for plugins to ship their own bundle configs.
func WithEmbeddedBundles(fsys embed.FS, dir string) ClientOption {
	return func(c *Client) {
		if err := c.Registry.LoadEmbedded(fsys, dir); err != nil {
			slog.Warn("Failed to load embedded bundles", "dir", dir, "error", err)
		}
	}
}

// WithBundlesFromPath loads bundle definitions from a file or directory.
// Uses disk caching to avoid re-parsing on every invocation.
func WithBundlesFromPath(path string) ClientOption {
	return func(c *Client) {
		if path == "" {
			return
		}

		// Try to load from cache first
		cached, err := LoadCachedRegistry(path)
		if err == nil && cached != nil {
			c.Registry.Merge(cached)
			return
		}

		// Cache miss or stale - load from disk
		if err := c.Registry.LoadFromPath(path); err != nil {
			slog.Warn("Failed to load bundles from path", "path", path, "error", err)
			return
		}

		// Save to cache for next time (ignore errors)
		_ = SaveRegistryCache(path, c.Registry)
	}
}

// WithBaseURL sets the base URL for API calls
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.BaseURL = url
	}
}

// WithBasicAuth sets credentials for HTTP Basic authentication
func WithBasicAuth(username, password string) ClientOption {
	return func(c *Client) {
		c.Username = username
		c.Password = password
	}
}

// WithBasicAuthFromEnv loads credentials from environment variables.
// Falls back to DRUPAL_USERNAME/DRUPAL_PASSWORD if the specified vars are empty.
func WithBasicAuthFromEnv(usernameVar, passwordVar string) ClientOption {
	return func(c *Client) {
		username := os.Getenv(usernameVar)
		if username == "" {
			username = os.Getenv("DRUPAL_USERNAME")
		}
		password := os.Getenv(passwordVar)
		if password == "" {
			password = os.Getenv("DRUPAL_PASSWORD")
		}
		c.Username = username
		c.Password = password
	}
}

// NewClient creates a new Drupal API client.
// Unlike distribution-specific clients, this starts with an empty registry.
// Use WithEmbeddedBundles or WithBundlesFromPath to load bundle definitions.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		Registry: NewBundleRegistry(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// FetchNode fetches a single node from the Drupal API and attaches the registry.
func (c *Client) FetchNode(url string) (*Node, error) {
	var node Node
	if err := c.fetch(url, &node); err != nil {
		return nil, err
	}
	node.SetRegistry(c.Registry)
	return &node, nil
}

// FetchTerm fetches a single taxonomy term from the Drupal API and attaches the registry.
func (c *Client) FetchTerm(url string) (*Term, error) {
	var term Term
	if err := c.fetch(url, &term); err != nil {
		return nil, err
	}
	term.SetRegistry(c.Registry)
	return &term, nil
}

// FetchMedia fetches a single media entity from the Drupal API and attaches the registry.
func (c *Client) FetchMedia(url string) (*Media, error) {
	var media Media
	if err := c.fetch(url, &media); err != nil {
		return nil, err
	}
	media.SetRegistry(c.Registry)
	return &media, nil
}

// FetchUser fetches a single user from the Drupal API and attaches the registry.
func (c *Client) FetchUser(url string) (*User, error) {
	var user User
	if err := c.fetch(url, &user); err != nil {
		return nil, err
	}
	user.SetRegistry(c.Registry)
	return &user, nil
}

// fetch is a helper that performs an HTTP GET and decodes JSON
func (c *Client) fetch(url string, v any) error {
	req, err := c.newRequest("GET", url)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode failed: %w", err)
	}

	return nil
}

// newRequest creates an HTTP request with authentication if configured
func (c *Client) newRequest(method, url string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	if c.Username != "" && c.Password != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}

	return req, nil
}

// ValidateConfig checks if bundle configuration loaded successfully
func (c *Client) ValidateConfig() error {
	// Check if any entity type has bundles registered
	for _, et := range c.Registry.ListAllEntityTypes() {
		if len(c.Registry.ListBundles(et)) > 0 {
			return nil
		}
	}
	return ErrNoBundles
}

// Errors
type clientError string

func (e clientError) Error() string { return string(e) }

const (
	ErrNoBundles clientError = "no bundle definitions loaded"
)
