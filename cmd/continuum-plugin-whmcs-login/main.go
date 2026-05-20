// Command continuum-plugin-whmcs-login serves the WHMCS auth provider plugin
// over hashicorp/go-plugin. main wires runtime + auth_provider + http_routes
// and serves the admin SPA + assets.
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	goruntime "runtime"
	"sync/atomic"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	publicmanifest "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/admin"
	pluginauth "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/auth"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/httproutes"
	pluginrt "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/server"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/store"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/whmcs"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/web"
)

//go:embed manifest.json
var manifestRaw []byte

//go:embed all:assets
var staticAssets embed.FS

// productCacheTTL is the lifetime of cached responses from /api/v1/admin/products.
// Admins can force a refresh from the SPA via the products/refresh endpoint.
const productCacheTTL = 5 * time.Minute

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "continuum-plugin-whmcs-login"})

	manifest, err := loadManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
		os.Exit(1)
	}

	httpSrv := httproutes.NewServer()

	// cfgPtr holds the latest Config so AuthServer + AdminServer see new
	// values immediately on Configure without rebuilding gRPC plumbing.
	var cfgPtr atomic.Pointer[pluginrt.Config]
	var poolPtr atomic.Pointer[pgxpool.Pool]
	var storePtr atomic.Pointer[store.Store]
	var cachePtr atomic.Pointer[whmcs.ProductCache]
	cfgFn := func() pluginrt.Config {
		if p := cfgPtr.Load(); p != nil {
			return *p
		}
		return pluginrt.Config{}
	}

	authSrv := pluginauth.NewServer(cfgFn)

	applyConfig := func(cfg pluginrt.Config) (*whmcs.ProductCache, error) {
		cfgPtr.Store(&cfg)
		var prodCache *whmcs.ProductCache
		if cfg.WHMCSAdminAPIID != "" && cfg.WHMCSAdminAPISecret != "" {
			apiClient := whmcs.NewAPIClient(cfg.WHMCSServerURL, cfg.WHMCSAdminAPIID, cfg.WHMCSAdminAPISecret)
			prodCache = whmcs.NewProductCache(apiClient, productCacheTTL)
		}
		cachePtr.Store(prodCache)
		logger.Info("configured",
			"whmcs_server_url", cfg.WHMCSServerURL,
			"admin_api_configured", prodCache != nil)
		return prodCache, nil
	}

	rt := pluginrt.New(manifest, func(cfg pluginrt.Config) error {
		if cfg.DatabaseURL == "" {
			return fmt.Errorf("database_url is required")
		}
		ctx := context.Background()
		pcfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
		if err != nil {
			return fmt.Errorf("parse database_url: %w", err)
		}
		if pcfg.MaxConns < 8 {
			pcfg.MaxConns = 8
		}
		pool, err := pgxpool.NewWithConfig(ctx, pcfg)
		if err != nil {
			return fmt.Errorf("connect database: %w", err)
		}
		if err := store.Migrate(ctx, pool); err != nil {
			pool.Close()
			return fmt.Errorf("migrate: %w", err)
		}
		st := store.New(pool)
		effective, err := st.ImportLegacyConfig(ctx, cfg)
		if err != nil {
			pool.Close()
			return fmt.Errorf("import app config: %w", err)
		}
		if _, err := applyConfig(effective); err != nil {
			pool.Close()
			return err
		}
		storePtr.Store(st)

		adminSrv := admin.NewServer(admin.Deps{
			ConfigFn:       cfgFn,
			ProductCacheFn: func() *whmcs.ProductCache { return cachePtr.Load() },
			UpdateConfigFn: func(ctx context.Context, next pluginrt.Config) error {
				st := storePtr.Load()
				if st == nil {
					return fmt.Errorf("store not configured")
				}
				if err := st.UpdateConfig(ctx, next); err != nil {
					return err
				}
				_, err := applyConfig(next)
				return err
			},
			APIFactory: func(c pluginrt.Config) admin.WHMCSAPI {
				if c.WHMCSAdminAPIID == "" || c.WHMCSAdminAPISecret == "" {
					return nil
				}
				return whmcs.NewAPIClient(c.WHMCSServerURL, c.WHMCSAdminAPIID, c.WHMCSAdminAPISecret)
			},
		})

		srv := server.New(server.Deps{
			Admin:      adminSrv,
			SPAFiles:   web.FS(),
			StaticFS:   staticAssets,
			StaticRoot: "assets",
		})
		httpSrv.SetHandler(srv.Handler())
		if old := poolPtr.Swap(pool); old != nil {
			old.Close()
		}
		return nil
	})

	sdkruntime.Serve(sdkruntime.ServeConfig{
		Logger: logger,
		Servers: sdkruntime.CapabilityServers{
			Runtime:      rt,
			HttpRoutes:   httpSrv,
			AuthProvider: authSrv,
		},
	})
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	if len(manifest.GetSupportedPlatforms()) == 0 {
		manifest.SupportedPlatforms = []*pluginv1.SupportedPlatform{
			{Os: goruntime.GOOS, Arch: goruntime.GOARCH},
		}
	}
	return manifest, nil
}
