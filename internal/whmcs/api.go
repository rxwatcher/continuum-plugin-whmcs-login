package whmcs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	hc        *http.Client
}

func NewAPIClient(serverURL, apiID, apiSecret string) *APIClient {
	return &APIClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		apiID:     apiID,
		apiSecret: apiSecret,
		hc:        &http.Client{Timeout: httpTimeout},
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

func (r rawProduct) toProduct() Product {
	return Product{
		PID:       parseRawInt(r.PID),
		GID:       parseRawInt(r.GID),
		Name:      r.Name,
		Type:      r.Type,
		PayType:   r.PayType,
		GroupName: r.GroupName,
	}
}

func (r rawProduct) toClientProduct() ClientProduct {
	return ClientProduct{
		PID:    parseRawInt(r.PID),
		Name:   r.Name,
		Status: r.Status,
	}
}

func parseRawInt(b json.RawMessage) int {
	if len(b) == 0 {
		return 0
	}
	// Try direct int.
	var n int
	if err := json.Unmarshal(b, &n); err == nil {
		return n
	}
	// Try string-wrapped int.
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		n, _ = strconv.Atoi(s)
		return n
	}
	return 0
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.serverURL+"/includes/api.php",
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whmcs api: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("whmcs api read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("whmcs api %d: %s", resp.StatusCode, string(body))
	}
	// WHMCS always returns a JSON envelope with "result": "success" | "error".
	var env struct {
		Result  string `json:"result"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode whmcs envelope: %w (body: %s)", err, string(body))
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
	for _, r := range raws {
		out = append(out, r.toProduct())
	}
	return out, nil
}

// GetClientByEmail returns the first WHMCS client whose email matches exactly,
// using GetClients(search=...) and a case-insensitive filter on the returned
// rows.
func (c *APIClient) GetClientByEmail(ctx context.Context, email string) (*Client, error) {
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
	body, err := c.post(ctx, "GetClientsProducts", url.Values{"clientid": {clientID}})
	if err != nil {
		return nil, err
	}
	raws, err := extractProductArray(body)
	if err != nil {
		return nil, err
	}
	out := make([]ClientProduct, 0, len(raws))
	for _, r := range raws {
		out = append(out, r.toClientProduct())
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
		return nil, nil
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
