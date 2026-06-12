package whmcs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// APIClient wraps the WHMCS admin API at /includes/api.php. All calls are
// POST with form-encoded credentials.
type APIClient struct {
	serverURL string
	apiID     string
	apiSecret string
}

func NewAPIClient(serverURL, apiID, apiSecret string) *APIClient {
	return &APIClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		apiID:     apiID,
		apiSecret: apiSecret,
	}
}

// Product is a row from GetProducts. PID is normalised to int even if WHMCS
// returns it as a JSON string.
type Product struct {
	PID       int    `json:"pid"`
	GID       int    `json:"gid,omitempty"`
	Name      string `json:"name"`
	Type      string `json:"type,omitempty"`
	PayType   string `json:"paytype,omitempty"`
	GroupName string `json:"groupname,omitempty"`
}

// ClientProduct is a row from GetClientsProducts (note the different set of
// fields available on this endpoint compared to GetProducts).
//
// Status is a pointer so we can distinguish:
//   - nil           → WHMCS omitted the field (older versions did this on
//     GetClientsProducts). Treated as Active per historical
//     compatibility behavior.
//   - non-nil ""    → WHMCS returned an explicit empty status. Treated as
//     INACTIVE (defensive — we'd rather a future WHMCS
//     version use empty for suspended than have a suspended
//     user retain access).
//   - non-nil "Active" → Active.
//   - non-nil other → Inactive.
type ClientProduct struct {
	PID    int     `json:"pid"`
	Name   string  `json:"name"`
	Status *string `json:"status,omitempty"`
}

// ClientDetails is the typed projection of GetClientsDetails.
type ClientDetails struct {
	ID           string
	Email        string
	FirstName    string
	LastName     string
	CustomFields map[string]string
}

// Client is a row from GetClients.
type Client struct {
	ID    string
	Email string
}

// rawProduct is an intermediate decoding shape that tolerates WHMCS's loose
// types (pid as int or string, group as different field names). Status is a
// pointer so toClientProduct can preserve the field-omitted vs
// present-but-empty distinction (see ClientProduct docstring).
type rawProduct struct {
	PID       json.RawMessage `json:"pid"`
	GID       json.RawMessage `json:"gid"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	PayType   string          `json:"paytype"`
	GroupName string          `json:"groupname"`
	Status    *string         `json:"status,omitempty"`
}

func (r rawProduct) toProduct() (Product, error) {
	pid, err := parseRequiredPositiveInt(r.PID)
	if err != nil {
		return Product{}, fmt.Errorf("pid: %w", err)
	}
	gid, err := parseOptionalInt(r.GID)
	if err != nil {
		return Product{}, fmt.Errorf("gid: %w", err)
	}
	return Product{
		PID:       pid,
		GID:       gid,
		Name:      r.Name,
		Type:      r.Type,
		PayType:   r.PayType,
		GroupName: r.GroupName,
	}, nil
}

func (r rawProduct) toClientProduct() (ClientProduct, error) {
	pid, err := parseRequiredPositiveInt(r.PID)
	if err != nil {
		return ClientProduct{}, fmt.Errorf("pid: %w", err)
	}
	return ClientProduct{
		PID:    pid,
		Name:   r.Name,
		Status: r.Status,
	}, nil
}

func parseOptionalInt(b json.RawMessage) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, nil
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		return n, nil
	}
	return 0, fmt.Errorf("not an integer")
}

func parseRequiredPositiveInt(b json.RawMessage) (int, error) {
	n, err := parseOptionalInt(b)
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return n, nil
}

// post issues a POST to /includes/api.php with the supplied action + extras.
// Returns the raw response body (already decoded as success).
func (c *APIClient) post(ctx context.Context, action string, extra url.Values) ([]byte, error) {
	form := url.Values{
		"identifier":   {c.apiID},
		"secret":       {c.apiSecret},
		"action":       {action},
		"responsetype": {"json"},
	}
	for k, v := range extra {
		form[k] = v
	}
	statusCode, body, err := doForm(ctx, http.MethodPost,
		c.serverURL+"/includes/api.php", "whmcs api", form, nil)
	if err != nil {
		return nil, err
	}
	if statusCode >= 400 {
		// Log raw upstream body server-side; surface only the status so the body
		// (which may echo request params or internal detail) never reaches
		// admin-facing JSON.
		logger.Debug("whmcs api error", "action", action, "status", statusCode, "body", string(body))
		err := fmt.Errorf("whmcs api returned status %d", statusCode)
		// 5xx is transient (upstream overload / restart); mark it retryable so
		// idempotent admin GETs wrapped in retry() can survive a single blip.
		// 4xx is a permanent client error and must not be retried.
		if statusCode >= 500 {
			return nil, markRetryable(err)
		}
		return nil, err
	}
	// WHMCS always returns a JSON envelope with "result": "success" | "error".
	var env struct {
		Result  string `json:"result"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		logger.Debug("whmcs api envelope decode failed", "action", action, "body", string(body))
		return nil, fmt.Errorf("decode whmcs envelope: %w", err)
	}
	if env.Result != "success" {
		msg := env.Message
		if msg == "" {
			msg = "(no message)"
		}
		return nil, fmt.Errorf("whmcs api error: %s", msg)
	}
	return body, nil
}

// GetProducts lists all products configured in WHMCS.
func (c *APIClient) GetProducts(ctx context.Context) ([]Product, error) {
	body, err := c.post(ctx, "GetProducts", url.Values{})
	if err != nil {
		return nil, err
	}
	raws, err := extractProductArray(body)
	if err != nil {
		return nil, err
	}
	out := make([]Product, 0, len(raws))
	for i, r := range raws {
		p, err := r.toProduct()
		if err != nil {
			return nil, fmt.Errorf("product[%d]: %w", i, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// GetClientByEmail returns the first WHMCS client whose email matches exactly,
// using GetClients(search=...) and a case-insensitive filter on the returned
// rows.
func (c *APIClient) GetClientByEmail(ctx context.Context, email string) (*Client, error) {
	return retry(ctx, func(ctx context.Context) (*Client, error) {
		return c.getClientByEmail(ctx, email)
	})
}

func (c *APIClient) getClientByEmail(ctx context.Context, email string) (*Client, error) {
	body, err := c.post(ctx, "GetClients", url.Values{
		"search": {email},
		"stats":  {"false"},
	})
	if err != nil {
		return nil, err
	}
	var env struct {
		Clients struct {
			Client json.RawMessage `json:"client"`
		} `json:"clients"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode clients: %w", err)
	}
	if len(env.Clients.Client) == 0 {
		return nil, nil
	}
	var rows []struct {
		ID    json.RawMessage `json:"id"`
		Email string          `json:"email"`
	}
	if err := json.Unmarshal(env.Clients.Client, &rows); err != nil {
		var single struct {
			ID    json.RawMessage `json:"id"`
			Email string          `json:"email"`
		}
		if err := json.Unmarshal(env.Clients.Client, &single); err != nil {
			return nil, fmt.Errorf("decode client entries: %w", err)
		}
		rows = append(rows, single)
	}
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Email), strings.TrimSpace(email)) {
			return &Client{ID: rawToString(row.ID), Email: row.Email}, nil
		}
	}
	return nil, nil
}

// GetClientsProducts lists products owned by a specific WHMCS client.
func (c *APIClient) GetClientsProducts(ctx context.Context, clientID string) ([]ClientProduct, error) {
	return retry(ctx, func(ctx context.Context) ([]ClientProduct, error) {
		return c.getClientsProducts(ctx, clientID)
	})
}

func (c *APIClient) getClientsProducts(ctx context.Context, clientID string) ([]ClientProduct, error) {
	body, err := c.post(ctx, "GetClientsProducts", url.Values{"clientid": {clientID}})
	if err != nil {
		return nil, err
	}
	raws, err := extractProductArray(body)
	if err != nil {
		return nil, err
	}
	out := make([]ClientProduct, 0, len(raws))
	for i, r := range raws {
		p, err := r.toClientProduct()
		if err != nil {
			return nil, fmt.Errorf("product[%d]: %w", i, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// extractProductArray pulls the products.product array out of a WHMCS
// envelope, tolerating: array form, single-object form (no array), and the
// empty-string form ("products": "") that WHMCS returns when there are none.
func extractProductArray(body []byte) ([]rawProduct, error) {
	var env struct {
		Products json.RawMessage `json:"products"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode products envelope: %w", err)
	}
	if len(env.Products) == 0 {
		return nil, nil
	}
	// Empty form: "products": "" or "products": []
	var asString string
	if err := json.Unmarshal(env.Products, &asString); err == nil {
		return nil, nil
	}
	var asEmptyArr []any
	if err := json.Unmarshal(env.Products, &asEmptyArr); err == nil {
		if len(asEmptyArr) == 0 {
			return nil, nil
		}
		return nil, fmt.Errorf("unexpected products array envelope")
	}
	var wrapper struct {
		Product json.RawMessage `json:"product"`
	}
	if err := json.Unmarshal(env.Products, &wrapper); err != nil {
		return nil, fmt.Errorf("decode products inner: %w", err)
	}
	if len(wrapper.Product) == 0 {
		return nil, nil
	}
	// Try array first.
	var arr []rawProduct
	if err := json.Unmarshal(wrapper.Product, &arr); err == nil {
		return arr, nil
	}
	// Fall back to single object.
	var single rawProduct
	if err := json.Unmarshal(wrapper.Product, &single); err != nil {
		return nil, fmt.Errorf("decode product entries: %w", err)
	}
	return []rawProduct{single}, nil
}

// GetClientsDetails returns the WHMCS client record + custom-field map.
func (c *APIClient) GetClientsDetails(ctx context.Context, clientID string) (ClientDetails, error) {
	return retry(ctx, func(ctx context.Context) (ClientDetails, error) {
		return c.getClientsDetails(ctx, clientID)
	})
}

func (c *APIClient) getClientsDetails(ctx context.Context, clientID string) (ClientDetails, error) {
	body, err := c.post(ctx, "GetClientsDetails", url.Values{"clientid": {clientID}})
	if err != nil {
		return ClientDetails{}, err
	}
	// The client envelope returns id as either int or string depending on
	// the WHMCS version; we decode into RawMessage and coerce to string.
	var env struct {
		Client struct {
			ID           json.RawMessage  `json:"id"`
			Email        string           `json:"email"`
			FirstName    string           `json:"firstname"`
			LastName     string           `json:"lastname"`
			CustomFields []map[string]any `json:"customfields"`
		} `json:"client"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ClientDetails{}, fmt.Errorf("decode client details: %w", err)
	}
	cd := ClientDetails{
		ID:           rawToString(env.Client.ID),
		Email:        env.Client.Email,
		FirstName:    env.Client.FirstName,
		LastName:     env.Client.LastName,
		CustomFields: make(map[string]string),
	}
	for _, f := range env.Client.CustomFields {
		name, _ := f["fieldname"].(string)
		val, _ := f["value"].(string)
		if name != "" {
			cd.CustomFields[name] = val
		}
	}
	return cd, nil
}

func rawToString(b json.RawMessage) string {
	if len(b) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		return s
	}
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		return strconv.Itoa(n)
	}
	return strings.Trim(string(b), `"`)
}
