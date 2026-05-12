// Command continuum-plugin-whmcs-login serves the WHMCS auth provider plugin
// over hashicorp/go-plugin. main wires the SDK runtime + http_routes + auth
// provider capabilities and exposes a /api/v1/health stub. Later phases mount
// the admin SPA.
package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	goruntime "runtime"
	"sync/atomic"

	"github.com/hashicorp/go-hclog"

	pluginv1 "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginproto/continuum/plugin/v1"
	publicmanifest "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/ContinuumApp/continuum-plugin-sdk/pkg/pluginsdk/runtime"

	pluginauth "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/auth"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/httproutes"
	pluginrt "github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/runtime"
	"github.com/ContinuumApp/continuum-plugin-whmcs-login/internal/server"
)

//go:embed manifest.json
var manifestRaw []byte

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "continuum-plugin-whmcs-login"})

	manifest, err := loadManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
		os.Exit(1)
	}

	httpSrv := httproutes.NewServer()

	// cfgPtr holds the latest Config so capability handlers (AuthServer, the
	// admin HTTP server in later phases) can pick up Configure-driven
	// changes without holding their own copies.
	var cfgPtr atomic.Pointer[pluginrt.Config]
	cfgFn := func() pluginrt.Config {
		if p := cfgPtr.Load(); p != nil {
			return *p
		}
		return pluginrt.Config{}
	}

	authSrv := pluginauth.NewServer(cfgFn)

	rt := pluginrt.New(manifest, func(cfg pluginrt.Config) error {
		// Store snapshot first so subsequent in-flight RPCs see the new
		// values, then rewire the HTTP handler.
		cfgPtr.Store(&cfg)
		srv := server.New(server.Deps{})
		httpSrv.SetHandler(srv.Handler())
		logger.Info("configured", "whmcs_server_url", cfg.WHMCSServerURL)
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
