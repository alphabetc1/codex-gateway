package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"claude-gateway/internal/admin"
	"claude-gateway/internal/auth"
	"claude-gateway/internal/config"
	"claude-gateway/internal/limiter"
	"claude-gateway/internal/logging"
	"claude-gateway/internal/netutil"
	"claude-gateway/internal/proxy"
	"claude-gateway/internal/version"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		slog.Error("load config failed", "error", err.Error())
		return 1
	}

	loggers, err := logging.New(cfg.LogLevel, cfg.LogFormat)
	if err != nil {
		slog.Error("initialize loggers failed", "error", err.Error())
		return 1
	}

	userStore, err := auth.LoadUserStore(cfg.AuthUsersFile, cfg.AuthUsers)
	if err != nil {
		loggers.App.Error("load auth users failed", "error", err.Error())
		return 1
	}

	metrics := proxy.NewMetrics()
	sourceMatcher := netutil.NewPrefixMatcher(cfg.SourceAllowlist)
	hostMatcher := netutil.NewHostMatcher(cfg.DestHosts, cfg.DestSuffixes)

	handler := proxy.NewHandler(proxy.Options{
		AppLogger:        loggers.App,
		AuditLogger:      loggers.Audit,
		AccessLogEnabled: cfg.AccessLogEnabled,
		AuthStore:        userStore,
		Limiter:          limiter.New(cfg.MaxConnsPerIP),
		Policy: proxy.Policy{
			AllowedPorts: cfg.DestPorts,
			HostMatcher:  hostMatcher,
			Resolver:     netutil.NetResolver{},
			AllowPrivate: cfg.AllowPrivateDestinations,
		},
		SourceIPs:                    sourceMatcher,
		Metrics:                      metrics,
		UpstreamDialTimeout:          cfg.UpstreamDialTimeout,
		UpstreamTLSHandshakeTimeout:  cfg.UpstreamTLSHandshakeTimeout,
		UpstreamResponseHeaderTimeout: cfg.UpstreamResponseHeaderTimeout,
		IdleTimeout:                  cfg.ServerIdleTimeout,
		TunnelIdleTimeout:            cfg.TunnelIdleTimeout,
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

	errCh := make(chan error, 2)

	go serveProxy(loggers.App, proxyServer, cfg, errCh)
	go serveHTTP(loggers.App, "admin", adminServer, errCh)

	select {
	case <-ctx.Done():
		loggers.App.Info("shutdown signal received")
	case serveErr := <-errCh:
		if serveErr != nil {
			loggers.App.Error("server exited", "error", serveErr.Error())
			stop()
		}
	}

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
