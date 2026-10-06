package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/IlnurShafikov/currency-quotes-service/internal/config"
	"github.com/IlnurShafikov/currency-quotes-service/internal/handler"
	"github.com/IlnurShafikov/currency-quotes-service/internal/provider"
	"github.com/IlnurShafikov/currency-quotes-service/internal/repository"
	"github.com/IlnurShafikov/currency-quotes-service/internal/scheduler"
	"github.com/IlnurShafikov/currency-quotes-service/internal/service"
	"github.com/IlnurShafikov/currency-quotes-service/internal/system"
	"github.com/IlnurShafikov/currency-quotes-service/internal/worker"
	"github.com/IlnurShafikov/currency-quotes-service/migrations"
)

// Limits of the HTTP server. They keep a slow or stuck client from holding a
// connection forever.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = time.Minute
)

// errBackgroundStillRunning is returned when the background work (the
// worker and, if enabled, the scheduler) does not stop within the shutdown
// timeout.
var errBackgroundStillRunning = errors.New("background work did not stop in time")

// app is the assembled service: every adapter wired to the service layer,
// plus the resources that have to be released on shutdown.
type app struct {
	cfg      config.Config
	log      *slog.Logger
	pool     *pgxpool.Pool
	listener net.Listener
	server   *http.Server
	worker   *worker.Worker
	// scheduler is nil when scheduled refreshing is turned off.
	scheduler *scheduler.Scheduler
}

// newApp connects to the database, brings its schema up to date, wires the
// components together and starts listening on the configured address. The
// service does not handle requests until serve is called.
func newApp(ctx context.Context, cfg config.Config, log *slog.Logger) (*app, error) {
	pool, err := openDatabase(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	application, err := wire(ctx, cfg, log, pool)
	if err != nil {
		pool.Close()

		return nil, err
	}

	return application, nil
}

// openDatabase returns a connection pool for a database that is reachable
// and has every migration applied.
func openDatabase(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}

	// pgxpool connects lazily; Ping makes a wrong address or password fail
	// here, at start-up, instead of on the first request.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("connect to database: %w", err)
	}

	// goose works with database/sql; this is a view of the same pool.
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { _ = sqlDB.Close() }()

	if err := migrations.Up(ctx, sqlDB); err != nil {
		pool.Close()

		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return pool, nil
}

// wire builds the object graph: adapters on the outside, the service layer
// in the middle. This is the only place that knows every concrete type.
func wire(ctx context.Context, cfg config.Config, log *slog.Logger, pool *pgxpool.Pool) (*app, error) {
	db := repository.NewDB(pool)
	updates := repository.NewUpdateRequests(db)
	quotes := repository.NewQuotes(db)
	currencies := repository.NewCurrencies(db)
	clock := system.Clock{}
	ids := system.IDs{}

	rates, err := provider.NewFrankfurter(cfg.RatesAPIURL, &http.Client{Timeout: cfg.RatesAPITimeout})
	if err != nil {
		return nil, fmt.Errorf("create rate provider: %w", err)
	}

	quoteService := service.NewQuoteService(updates, quotes, currencies, clock, ids)

	processor, err := service.NewUpdateProcessor(updates, quotes, rates, db, clock, ids, service.ProcessorConfig{
		BatchSize:   cfg.WorkerBatchSize,
		MaxAttempts: cfg.WorkerMaxAttempts,
		Timeout:     cfg.WorkerTimeout,
		StaleAfter:  cfg.WorkerStaleAfter,
	})
	if err != nil {
		return nil, fmt.Errorf("create update processor: %w", err)
	}

	backgroundWorker, err := worker.New(processor, cfg.WorkerInterval, log)
	if err != nil {
		return nil, fmt.Errorf("create worker: %w", err)
	}

	refreshScheduler, err := newScheduler(cfg, quoteService, log)
	if err != nil {
		return nil, err
	}

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}

	return &app{
		cfg:       cfg,
		log:       log,
		pool:      pool,
		listener:  listener,
		worker:    backgroundWorker,
		scheduler: refreshScheduler,
		server: &http.Server{
			Handler:           handler.New(quoteService, log).Routes(),
			ReadHeaderTimeout: readHeaderTimeout,
			ReadTimeout:       readTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
		},
	}, nil
}

// addr returns the address the HTTP server listens on. It differs from the
// configured one when the port was chosen by the system (":0").
func (a *app) addr() string {
	return a.listener.Addr().String()
}

// serve runs the HTTP server and the background work until ctx is
// cancelled, then shuts both down gracefully: the server finishes the
// requests in flight and the worker finishes its current batch, within the
// shutdown timeout. It returns nil after a clean shutdown.
func (a *app) serve(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	serverErr := make(chan error, 1)

	go func() {
		serverErr <- a.server.Serve(a.listener)
	}()

	backgroundDone := a.runBackground(ctx)

	a.log.InfoContext(ctx, "service started", slog.String("addr", a.addr()))

	var err error

	select {
	case <-ctx.Done():
	case serveErr := <-serverErr:
		// The server stopped on its own; take the background work down with it.
		err = fmt.Errorf("http server: %w", serveErr)

		cancel()
	}

	a.log.InfoContext(ctx, "shutting down")

	// ctx is already cancelled, so the shutdown gets a deadline of its own.
	shutdownCtx, cancelShutdown := context.WithTimeout(context.WithoutCancel(ctx), a.cfg.ShutdownTimeout)
	defer cancelShutdown()

	if shutdownErr := a.server.Shutdown(shutdownCtx); shutdownErr != nil {
		err = errors.Join(err, fmt.Errorf("shut down http server: %w", shutdownErr))
	}

	select {
	case <-backgroundDone:
		// Nothing uses the database any more.
		a.pool.Close()
	case <-shutdownCtx.Done():
		err = errors.Join(err, errBackgroundStillRunning)
	}

	a.log.InfoContext(ctx, "service stopped")

	return err
}

// newScheduler returns the scheduler that refreshes stale quotes, or nil if
// no interval is configured and quotes are updated only on request.
func newScheduler(cfg config.Config, quotes scheduler.Refresher, log *slog.Logger) (*scheduler.Scheduler, error) {
	if cfg.SchedulerInterval == 0 {
		return nil, nil
	}

	refreshScheduler, err := scheduler.New(quotes, cfg.SchedulerInterval, log)
	if err != nil {
		return nil, fmt.Errorf("create scheduler: %w", err)
	}

	return refreshScheduler, nil
}

// runBackground starts the worker and, if it is enabled, the scheduler. The
// returned channel is closed once all of them have returned, which happens
// after ctx is cancelled.
func (a *app) runBackground(ctx context.Context) <-chan struct{} {
	var running sync.WaitGroup

	running.Go(func() { a.worker.Run(ctx) })

	if a.scheduler != nil {
		running.Go(func() { a.scheduler.Run(ctx) })
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		running.Wait()
	}()

	return done
}
