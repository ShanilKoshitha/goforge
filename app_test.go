package forge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

type lifecycleServerStub struct {
	serveStarted chan struct{}
	serveDone    chan error
	shutdownErr  error
	closeErr     error
	serveOnClose error
	shutdowns    int
	closes       int
}

func newLifecycleServerStub() *lifecycleServerStub {
	return &lifecycleServerStub{
		serveStarted: make(chan struct{}),
		serveDone:    make(chan error, 1),
	}
}

func (server *lifecycleServerStub) ListenAndServe() error {
	close(server.serveStarted)
	return <-server.serveDone
}

func (server *lifecycleServerStub) Shutdown(context.Context) error {
	server.shutdowns++
	if server.shutdownErr == nil {
		server.finish(http.ErrServerClosed)
	}
	return server.shutdownErr
}

func (server *lifecycleServerStub) Close() error {
	server.closes++
	serveErr := server.serveOnClose
	if serveErr == nil {
		serveErr = http.ErrServerClosed
	}
	server.finish(serveErr)
	return server.closeErr
}

func (server *lifecycleServerStub) finish(err error) {
	select {
	case server.serveDone <- err:
	default:
	}
}

func TestNewNormalizesLifecycleConfig(t *testing.T) {
	app := New(Config{Address: " \t", ShutdownTimeout: -time.Second})

	if app.config.Address != ":8080" {
		t.Fatalf("Address = %q, want %q", app.config.Address, ":8080")
	}
	if app.config.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want %s", app.config.ShutdownTimeout, 10*time.Second)
	}
	if app.config.Logger == nil {
		t.Fatal("Logger is nil")
	}
	if app.config.ReadHeaderTimeout != 5*time.Second || app.config.ReadTimeout != 30*time.Second ||
		app.config.WriteTimeout != 30*time.Second || app.config.IdleTimeout != 2*time.Minute ||
		app.config.MaxHeaderBytes != 1<<20 {
		t.Fatalf("HTTP limits were not defaulted: %+v", app.config)
	}
}

func TestNewPreservesExplicitLifecycleConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := New(Config{
		Address:           " 127.0.0.1:9000 ",
		ShutdownTimeout:   3 * time.Second,
		ReadHeaderTimeout: 4 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      6 * time.Second,
		IdleTimeout:       7 * time.Second,
		MaxHeaderBytes:    2048,
		Logger:            logger,
	})

	if app.config.Address != "127.0.0.1:9000" {
		t.Fatalf("Address = %q, want trimmed explicit address", app.config.Address)
	}
	if app.config.ShutdownTimeout != 3*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want %s", app.config.ShutdownTimeout, 3*time.Second)
	}
	if app.config.Logger != logger {
		t.Fatal("Logger was not preserved")
	}
	if app.config.ReadHeaderTimeout != 4*time.Second || app.config.ReadTimeout != 5*time.Second ||
		app.config.WriteTimeout != 6*time.Second || app.config.IdleTimeout != 7*time.Second ||
		app.config.MaxHeaderBytes != 2048 {
		t.Fatalf("explicit HTTP limits were not preserved: %+v", app.config)
	}
}

func TestNewCheckedRejectsInvalidExplicitLimits(t *testing.T) {
	tests := []Config{
		{ShutdownTimeout: -time.Nanosecond},
		{ReadHeaderTimeout: -time.Nanosecond},
		{ReadTimeout: -time.Nanosecond},
		{WriteTimeout: -time.Nanosecond},
		{IdleTimeout: -time.Nanosecond},
		{MaxHeaderBytes: -1},
	}
	for _, config := range tests {
		if _, err := NewChecked(config); err == nil {
			t.Fatalf("invalid config was accepted: %+v", config)
		}
	}
	app, err := NewChecked(Config{})
	if err != nil || app.config.ReadTimeout != 30*time.Second {
		t.Fatalf("valid defaults: app=%v err=%v", app, err)
	}
}

func TestHTTPServerUsesEveryBoundedLimit(t *testing.T) {
	config := normalizeConfig(Config{})
	server, ok := newHTTPServer(config, http.NotFoundHandler()).(*http.Server)
	if !ok {
		t.Fatal("newHTTPServer did not return *http.Server")
	}
	if server.ReadHeaderTimeout != config.ReadHeaderTimeout || server.ReadTimeout != config.ReadTimeout ||
		server.WriteTimeout != config.WriteTimeout || server.IdleTimeout != config.IdleTimeout ||
		server.MaxHeaderBytes != config.MaxHeaderBytes {
		t.Fatalf("server limits = %+v, config = %+v", server, config)
	}
}

func TestRunRejectsNilContext(t *testing.T) {
	err := New(Config{}).Run(nil)
	if err == nil || err.Error() != "run: nil context" {
		t.Fatalf("Run(nil) error = %v, want run: nil context", err)
	}
}

func TestRunReturnsServeFailure(t *testing.T) {
	serveErr := errors.New("listener failed")
	server := newLifecycleServerStub()
	server.serveDone <- serveErr
	app := appWithLifecycleServer(server)

	err := app.Run(context.Background())
	if !errors.Is(err, serveErr) {
		t.Fatalf("Run error = %v, want wrapped serve error", err)
	}
	if server.shutdowns != 0 || server.closes != 0 {
		t.Fatalf("shutdowns = %d, closes = %d; want neither", server.shutdowns, server.closes)
	}
}

func TestRunGracefullyShutsDownOnCancellation(t *testing.T) {
	server := newLifecycleServerStub()
	app := appWithLifecycleServer(server)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	<-server.serveStarted

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("Run error = %v, want nil", err)
	}
	if server.shutdowns != 1 || server.closes != 0 {
		t.Fatalf("shutdowns = %d, closes = %d; want 1 graceful shutdown and no close", server.shutdowns, server.closes)
	}
}

func TestRunForceClosesAfterGracefulShutdownFailure(t *testing.T) {
	shutdownErr := errors.New("shutdown timed out")
	server := newLifecycleServerStub()
	server.shutdownErr = shutdownErr
	app := appWithLifecycleServer(server)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	<-server.serveStarted

	cancel()
	err := <-done

	if !errors.Is(err, shutdownErr) {
		t.Fatalf("Run error = %v, want wrapped shutdown error", err)
	}
	if server.shutdowns != 1 || server.closes != 1 {
		t.Fatalf("shutdowns = %d, closes = %d; want graceful attempt followed by forced close", server.shutdowns, server.closes)
	}
}

func TestRunPreservesShutdownCloseAndServeFailures(t *testing.T) {
	shutdownErr := errors.New("shutdown timed out")
	closeErr := errors.New("close failed")
	serveErr := errors.New("serve failed while stopping")
	server := newLifecycleServerStub()
	server.shutdownErr = shutdownErr
	server.closeErr = closeErr
	server.serveOnClose = serveErr
	app := appWithLifecycleServer(server)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	<-server.serveStarted

	cancel()
	err := <-done

	for _, want := range []error{shutdownErr, closeErr, serveErr} {
		if !errors.Is(err, want) {
			t.Errorf("Run error = %v, want it to include %v", err, want)
		}
	}
}

func appWithLifecycleServer(server lifecycleServer) *App {
	app := New(Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	app.newServer = func(Config, http.Handler) lifecycleServer { return server }
	return app
}
