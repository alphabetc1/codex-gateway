package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"codex-gateway/internal/admin"
	"codex-gateway/internal/config"
	"codex-gateway/internal/deploy"
	"codex-gateway/internal/limiter"
	"codex-gateway/internal/logging"
	"codex-gateway/internal/proxy"
	"codex-gateway/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "deploy":
			if err := deploy.Run(args[1:], os.Stdout, os.Stderr); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			return 0
		case "version", "--version", "-version":
			fmt.Fprintln(os.Stdout, version.Version)
			return 0
		case "help", "--help", "-h":
			fmt.Fprintln(os.Stdout, "Usage:")
			fmt.Fprintln(os.Stdout, "  codex-gateway")
			fmt.Fprintln(os.Stdout, "  codex-gateway deploy vps [-config deploy/vps.yaml] [--write-only]")
			fmt.Fprintln(os.Stdout, "  codex-gateway deploy client [-config deploy/client.yaml] [--write-only]")
			return 0
		}
	}

	loader := newRuntimeLoader()
	cfg, runtimeConfig, err := loader.Load()
	if err != nil {
		slog.Error("load config failed", "error", err.Error())
		return 1
	}

	loggers, err := logging.New(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		slog.Error("initialize loggers failed", "error", err.Error())
		return 1
	}

	metrics := proxy.NewMetrics()

	handler := proxy.NewHandler(proxy.Options{
		AppLogger:                     loggers.App,
		AuditLogger:                   loggers.Audit,
		AccessLogEnabled:              cfg.AccessLogEnabled,
		AuthStore:                     runtimeConfig.AuthStore,
		Limiter:                       limiter.New(cfg.MaxConnsPerIP),
		Policy:                        runtimeConfig.Policy,
		SourceIPs:                     runtimeConfig.SourceIPs,
		Metrics:                       metrics,
		UpstreamDialTimeout:           cfg.UpstreamDialTimeout,
		UpstreamTLSHandshakeTimeout:   cfg.UpstreamTLSHandshakeTimeout,
		UpstreamResponseHeaderTimeout: cfg.UpstreamResponseHeaderTimeout,
		IdleTimeout:                   cfg.ServerIdleTimeout,
		TunnelIdleTimeout:             cfg.TunnelIdleTimeout,
	})

	proxyServer := &http.Server{
		Addr:              cfg.ProxyListenAddress(),
		Handler:           handler,
		ReadHeaderTimeout: cfg.ServerReadHeaderTimeout,
		IdleTimeout:       cfg.ServerIdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	adminServer := &http.Server{
		Addr:              cfg.AdminListenAddress(),
		Handler:           admin.NewHandler(admin.Options{MetricsEnabled: cfg.MetricsEnabled, Metrics: metrics, Version: version.Version}),
		ReadHeaderTimeout: cfg.ServerReadHeaderTimeout,
		IdleTimeout:       cfg.ServerIdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reloadCh := make(chan os.Signal, 1)
	signal.Notify(reloadCh, syscall.SIGHUP)
	defer signal.Stop(reloadCh)

	errCh := make(chan error, 2)

	go serveProxy(loggers.App, proxyServer, cfg, errCh)
	go serveHTTP(loggers.App, "admin", adminServer, errCh)

	for {
		select {
		case <-ctx.Done():
			loggers.App.Info("shutdown signal received")
			goto shutdown
		case <-reloadCh:
			nextCfg, nextRuntime, reloadErr := loader.Load()
			if reloadErr != nil {
				loggers.App.Error("config reload failed", "error", reloadErr.Error())
				continue
			}

			handler.UpdateRuntime(nextRuntime)

			if fields := changedReloadableFields(cfg, nextCfg); len(fields) > 0 {
				loggers.App.Info("config reload applied",
					"signal", "SIGHUP",
					"fields", strings.Join(fields, ","),
					"env_file", loader.envFilePath,
				)
			} else {
				loggers.App.Info("config reload completed with no reloadable changes",
					"signal", "SIGHUP",
					"env_file", loader.envFilePath,
				)
			}

			if fields := immutableConfigChanges(cfg, nextCfg); len(fields) > 0 {
				loggers.App.Warn("config reload requires restart for some fields",
					"fields", strings.Join(fields, ","),
				)
			}

			cfg = applyReloadableConfig(cfg, nextCfg)
		case serveErr := <-errCh:
			if serveErr != nil {
				loggers.App.Error("server exited", "error", serveErr.Error())
				stop()
			}
		}
	}

shutdown:
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adminServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		loggers.App.Error("admin shutdown failed", "error", err.Error())
	}
	if err := proxyServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		loggers.App.Error("proxy shutdown failed", "error", err.Error())
	}

	return 0
}

func serveProxy(logger *slog.Logger, server *http.Server, cfg config.Config, errCh chan<- error) {
	logger.Info("proxy server starting",
		"addr", server.Addr,
		"tls_enabled", cfg.ProxyTLSEnabled,
	)
	var err error
	if cfg.ProxyTLSEnabled {
		err = server.ListenAndServeTLS(cfg.ProxyTLSCertFile, cfg.ProxyTLSKeyFile)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
	}
}

func serveHTTP(logger *slog.Logger, name string, server *http.Server, errCh chan<- error) {
	logger.Info("admin server starting", "name", name, "addr", server.Addr)
	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		errCh <- err
	}
}
