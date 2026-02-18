// TEMPLATE: Typed HTTP client SDK for __SERVICE_FULL__
// Copy this file and replace all __PLACEHOLDER__ markers with your service values.
//
// This client is the official Go SDK for interacting with the __SERVICE__
// API. It is imported by the CLI, other services, and integration tests.
//
// Conventions:
//   - Methods mirror the API: Get, List, Create, Update, Patch, Delete.
//   - Every method accepts context.Context for cancellation, timeout,
//     and trace propagation.
//   - Errors are parsed as RFC 9457 Problem Details when the server returns
//     a 4xx or 5xx status.
//   - Request ID and trace context are propagated automatically via the
//     chamicore-lib base HTTP client.
package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	// TEMPLATE: Update this import to match your service module path.
	"git.cscs.ch/openchami/__SERVICE_FULL__/pkg/types"

	chamihttp "git.cscs.ch/openchami/chamicore-lib/pkg/http"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Config holds the client configuration. Use functional options or struct
// literals to construct.
type Config struct {
	// BaseURL is the root URL of the __SERVICE__ API (e.g., "https://api.example.com").
	BaseURL string

	// Token is the Bearer token for authentication. If empty, requests are
	// sent without authorization.
	Token string

	// Timeout is the per-request timeout. Defaults to 30 seconds.
	Timeout time.Duration

	// MaxRetries is the number of automatic retries for transient errors
	// (5xx, connection reset). Defaults to 3.
	MaxRetries int

	// HTTPClient is an optional custom http.Client. If nil, a default is used.
	HTTPClient *http.Client
}

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client is the typed HTTP SDK for the __SERVICE__ service.
type Client struct {
	cfg    Config
	client *chamihttp.Client
}

// New creates a new Client with the given configuration.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("client: BaseURL is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}

	// chamicore-lib base HTTP client handles:
	//   - Bearer token injection
	//   - Request ID propagation (X-Request-ID header)
	//   - OpenTelemetry trace context propagation
	//   - Retry with exponential backoff
	//   - RFC 9457 error parsing
	httpClient := chamihttp.NewClient(chamihttp.ClientConfig{
		BaseURL:    cfg.BaseURL,
		Token:      cfg.Token,
		Timeout:    cfg.Timeout,
		MaxRetries: cfg.MaxRetries,
		HTTPClient: cfg.HTTPClient,
	})

	return &Client{
		cfg:    cfg,
		client: httpClient,
	}, nil
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// ListOptions configures the List request.
type ListOptions struct {
	Limit  int
	Offset int

	// TEMPLATE: Add filter parameters matching your API query params.
	// Example:
	// Type string
}

// List retrieves a paginated list of __RESOURCE_LOWER__ resources.
func (c *Client) List(ctx context.Context, opts ListOptions) (*types.ResourceList[types.__RESOURCE__], error) {
	params := url.Values{}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", strconv.Itoa(opts.Offset))
	}
	// TEMPLATE: Add filter parameters to the query string.
	// if opts.Type != "" {
	//     params.Set("type", opts.Type)
	// }

	path := "__API_PREFIX__/__RESOURCE_PLURAL__"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result types.ResourceList[types.__RESOURCE__]
	if err := c.client.Get(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("listing __RESOURCE_PLURAL__: %w", err)
	}

	return &result, nil
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

// Get retrieves a single __RESOURCE_LOWER__ by ID.
func (c *Client) Get(ctx context.Context, id string) (*types.Resource[types.__RESOURCE__], error) {
	path := fmt.Sprintf("__API_PREFIX__/__RESOURCE_PLURAL__/%s", url.PathEscape(id))

	var result types.Resource[types.__RESOURCE__]
	if err := c.client.Get(ctx, path, &result); err != nil {
		return nil, fmt.Errorf("getting __RESOURCE_LOWER__ %q: %w", id, err)
	}

	return &result, nil
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// Create creates a new __RESOURCE_LOWER__.
func (c *Client) Create(ctx context.Context, req types.Create__RESOURCE__Request) (*types.Resource[types.__RESOURCE__], error) {
	path := "__API_PREFIX__/__RESOURCE_PLURAL__"

	var result types.Resource[types.__RESOURCE__]
	if err := c.client.Post(ctx, path, req, &result); err != nil {
		return nil, fmt.Errorf("creating __RESOURCE_LOWER__: %w", err)
	}

	return &result, nil
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

// Update performs a full replacement of an existing __RESOURCE_LOWER__.
// The etag parameter must be the current ETag of the resource (from a prior
// Get call) and is sent as the If-Match header.
func (c *Client) Update(ctx context.Context, id string, etag string, req types.Update__RESOURCE__Request) (*types.Resource[types.__RESOURCE__], error) {
	path := fmt.Sprintf("__API_PREFIX__/__RESOURCE_PLURAL__/%s", url.PathEscape(id))

	headers := map[string]string{
		"If-Match": etag,
	}

	var result types.Resource[types.__RESOURCE__]
	if err := c.client.PutWithHeaders(ctx, path, req, &result, headers); err != nil {
		return nil, fmt.Errorf("updating __RESOURCE_LOWER__ %q: %w", id, err)
	}

	return &result, nil
}

// ---------------------------------------------------------------------------
// Patch
// ---------------------------------------------------------------------------

// Patch performs a partial update of an existing __RESOURCE_LOWER__.
func (c *Client) Patch(ctx context.Context, id string, req types.Patch__RESOURCE__Request) (*types.Resource[types.__RESOURCE__], error) {
	path := fmt.Sprintf("__API_PREFIX__/__RESOURCE_PLURAL__/%s", url.PathEscape(id))

	var result types.Resource[types.__RESOURCE__]
	if err := c.client.Patch(ctx, path, req, &result); err != nil {
		return nil, fmt.Errorf("patching __RESOURCE_LOWER__ %q: %w", id, err)
	}

	return &result, nil
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

// Delete removes a __RESOURCE_LOWER__ by ID.
func (c *Client) Delete(ctx context.Context, id string) error {
	path := fmt.Sprintf("__API_PREFIX__/__RESOURCE_PLURAL__/%s", url.PathEscape(id))

	if err := c.client.Delete(ctx, path); err != nil {
		return fmt.Errorf("deleting __RESOURCE_LOWER__ %q: %w", id, err)
	}

	return nil
}
