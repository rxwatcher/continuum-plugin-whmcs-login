package whmcs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
)

func TestGetProducts_PostsExpectedFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.URL.Path != "/includes/api.php" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Form.Get("action") != "GetProducts" {
			t.Errorf("action = %q", r.Form.Get("action"))
		}
		if r.Form.Get("identifier") != "id" || r.Form.Get("secret") != "sec" || r.Form.Get("responsetype") != "json" {
			t.Errorf("auth params = %v", r.Form)
		}
		_, _ = w.Write([]byte(`{
			"result":"success",
			"totalresults":2,
			"products":{"product":[
				{"pid":1,"name":"Basic","gid":2,"type":"hostingaccount","paytype":"recurring"},
				{"pid":5,"name":"Pro","gid":2,"type":"hostingaccount","paytype":"recurring"}
			]}
		}`))
	}))
	defer srv.Close()

	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	prods, err := c.GetProducts(context.Background())
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(prods) != 2 || prods[0].PID != 1 || prods[1].Name != "Pro" {
		t.Errorf("prods = %+v", prods)
	}
}

func TestGetProducts_StringPID(t *testing.T) {
	// WHMCS sometimes returns pid as a string. Confirm we parse it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":"7","name":"Strong"}]}}`))
	}))
	defer srv.Close()
	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	prods, err := c.GetProducts(context.Background())
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(prods) != 1 || prods[0].PID != 7 {
		t.Errorf("prods = %+v", prods)
	}
}

func TestGetProducts_RejectsMalformedPID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":"not-a-number","name":"Bad"}]}}`))
	}))
	defer srv.Close()

	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	_, err := c.GetProducts(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pid") {
		t.Fatalf("expected pid parse error, got %v", err)
	}
}

func TestGetProducts_EmptyEnvelope(t *testing.T) {
	// WHMCS returns the empty string for products when there are none.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"success","totalresults":0,"products":""}`))
	}))
	defer srv.Close()
	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	prods, err := c.GetProducts(context.Background())
	if err != nil {
		t.Fatalf("GetProducts: %v", err)
	}
	if len(prods) != 0 {
		t.Errorf("expected empty slice, got %+v", prods)
	}
}

func TestGetClientByEmail_ReturnsExactMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("action") != "GetClients" {
			t.Errorf("action = %q", r.Form.Get("action"))
		}
		if r.Form.Get("search") != "u@x.com" {
			t.Errorf("search = %q", r.Form.Get("search"))
		}
		_, _ = w.Write([]byte(`{"result":"success","clients":{"client":[{"id":"41","email":"other@x.com"},{"id":"42","email":"U@x.com"}]}}`))
	}))
	defer srv.Close()

	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	client, err := c.GetClientByEmail(context.Background(), "u@x.com")
	if err != nil {
		t.Fatalf("GetClientByEmail: %v", err)
	}
	if client == nil || client.ID != "42" {
		t.Fatalf("client = %+v, want id 42", client)
	}
}

func TestGetClientsProducts_FiltersByClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("action") != "GetClientsProducts" {
			t.Errorf("action = %q", r.Form.Get("action"))
		}
		if r.Form.Get("clientid") != "42" {
			t.Errorf("clientid = %q", r.Form.Get("clientid"))
		}
		_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":5,"name":"Pro","status":"Active"}]}}`))
	}))
	defer srv.Close()

	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	prods, err := c.GetClientsProducts(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetClientsProducts: %v", err)
	}
	if len(prods) != 1 || prods[0].PID != 5 || prods[0].Status == nil || *prods[0].Status != "Active" {
		t.Errorf("prods = %+v", prods)
	}
}

func TestGetClientsProducts_SingleObjectForm(t *testing.T) {
	// WHMCS sometimes returns products.product as a single object (not array)
	// when there is exactly one product.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"success","products":{"product":{"pid":9,"name":"Solo","status":"Active"}}}`))
	}))
	defer srv.Close()
	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	prods, err := c.GetClientsProducts(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetClientsProducts: %v", err)
	}
	if len(prods) != 1 || prods[0].PID != 9 {
		t.Errorf("prods = %+v", prods)
	}
}

func TestGetClientsProducts_RejectsZeroPID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"success","products":{"product":[{"pid":0,"name":"Bad","status":"Active"}]}}`))
	}))
	defer srv.Close()

	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	_, err := c.GetClientsProducts(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("expected positive integer error, got %v", err)
	}
}

func TestGetClientsProducts_RejectsUnexpectedArrayEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"success","products":[{"pid":5,"name":"Bad","status":"Active"}]}`))
	}))
	defer srv.Close()

	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	_, err := c.GetClientsProducts(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), "unexpected products array") {
		t.Fatalf("expected unexpected products array error, got %v", err)
	}
}

func TestGetClientsDetails_ReturnsCustomFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("action") != "GetClientsDetails" {
			t.Errorf("action = %q", r.Form.Get("action"))
		}
		_, _ = w.Write([]byte(`{
			"result":"success",
			"client":{
				"id":"42","email":"u@x.com","firstname":"U","lastname":"R",
				"customfields":[
					{"value":"183xxx","fieldname":"Discord ID"},
					{"value":"abc","fieldname":"Other"}
				]
			}
		}`))
	}))
	defer srv.Close()

	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	cd, err := c.GetClientsDetails(context.Background(), "42")
	if err != nil {
		t.Fatalf("GetClientsDetails: %v", err)
	}
	if cd.CustomFields["Discord ID"] != "183xxx" {
		t.Errorf("custom = %v", cd.CustomFields)
	}
	if cd.Email != "u@x.com" || cd.FirstName != "U" || cd.LastName != "R" {
		t.Errorf("cd = %+v", cd)
	}
}

func TestErrorEnvelope_BubblesUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":"error","message":"Invalid API Credentials"}`))
	}))
	defer srv.Close()

	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	if _, err := c.GetProducts(context.Background()); err == nil {
		t.Error("expected error envelope to bubble up")
	} else if !strings.Contains(err.Error(), "Invalid API Credentials") {
		t.Errorf("error should include WHMCS message, got: %v", err)
	}
}

func TestHTTPError_BubblesUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`internal server error`))
	}))
	defer srv.Close()
	c := whmcs.NewAPIClient(srv.URL, "id", "sec")
	if _, err := c.GetProducts(context.Background()); err == nil {
		t.Error("expected http error to bubble up")
	}
}
