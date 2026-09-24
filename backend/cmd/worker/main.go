package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/Owlah2025/gradex/backend/internal/db"
	transactionalemail "github.com/Owlah2025/gradex/backend/internal/email"
	"github.com/Owlah2025/gradex/backend/internal/logging"
	"github.com/Owlah2025/gradex/backend/internal/media"
	"github.com/Owlah2025/gradex/backend/internal/outbox"
	"github.com/Owlah2025/gradex/backend/internal/queue"
	"github.com/Owlah2025/gradex/backend/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger := logging.New(os.Stderr, "gradex-worker", "unknown", logging.LevelFromString("info"))
		exitWorker(bootstrapLogger, "config_load", logging.ErrorClassOf(err))
		return
	}
	logger := logging.New(os.Stdout, "gradex-worker", string(cfg.Environment()), logging.LevelFromString(cfg.LogLevel()))
	if cfg.ServiceRole() != config.RoleWorker {
		exitWorker(logger, "service_role_validation", "invalid_service_role")
		return
	}
	logger.WorkerLifecycle(logging.WorkerStarting)
	queue.SuppressRedisInternalLogs()

	pool, err := db.Connect(ctx, cfg.DatabaseURL().Expose())
	if err != nil {
		exitWorker(logger, "database_connect", logging.ErrorClassOf(err))
		return
	}
	defer pool.Close()
	if cfg.Email().Enabled() {
		startupCtx, cancel := context.WithTimeout(ctx, cfg.ReadinessTimeout())
		err := db.CheckSchemaAtLeast(startupCtx, pool, db.TransactionalEmailSchemaVersion)
		cancel()
		if err != nil {
			exitWorker(logger, "email_schema_check", logging.ErrorClassOf(err))
			return
		}
	}
	// The media pipeline writes the D-098 processing-progress observation when
	// it claims a transcode. Against an older schema that write fails, and the
	// claim fails with it, so every video would sit unprocessed with no failure
	// state to show its Instructor. The worker refuses to start instead — an
	// unstarted worker is visible; a silently non-processing one is not.
	//
	// Phase 3C-B requires 0042: an enhancement claim must persist its kind in
	// the same update as its operation token, so stale recovery can record the
	// right failed attempt even if the worker dies before encoding any rung.
	//
	// Media workers still require schema 42 for the active attempt kind. The
	// device-rotation API requires schema 43 because it writes AUTO_REPLACED;
	// the worker accepts 43 without changing media processing semantics.
	{
		startupCtx, cancel := context.WithTimeout(ctx, cfg.ReadinessTimeout())
		err := db.CheckSchemaAtLeast(startupCtx, pool, db.ActiveProcessingKindSchemaVersion)
		cancel()
		if err != nil {
			exitWorker(logger, "media_schema_check", logging.ErrorClassOf(err))
			return
		}
	}

	// Phase 3C-C automatic enhancement recovery is a SEPARATE capability gate, not
	// a reason to raise the media floor. The base floor stays at 42: a worker
	// serving media correctly on 42 must not be refused for a capability it is not
	// configured to use.
	//
	// When the flag IS set, schema 45 is required and the check fails closed. A
	// worker that started anyway would look like automatic recovery was running
	// while no candidate could ever be scheduled, which is worse than not starting.
	autoRecoveryStateAvailable := false
	{
		startupCtx, cancel := context.WithTimeout(ctx, cfg.ReadinessTimeout())
		err := db.CheckSchemaAtLeast(startupCtx, pool, db.AutoEnhancementRecoverySchemaVersion)
		cancel()
		switch {
		case err == nil:
			autoRecoveryStateAvailable = true
		case cfg.MediaAutoEnhancementRecoveryEnabled():
			exitWorker(logger, "media_auto_recovery_schema_check", logging.ErrorClassOf(err))
			return
		}
	}

	storageClient, err := storage.New(ctx, storage.Options{
		Endpoint:        cfg.S3Endpoint(),
		PresignEndpoint: cfg.S3PresignEndpoint(),
		AccessKey:       cfg.S3AccessKey().Expose(),
		SecretKey:       cfg.S3SecretKey().Expose(),
		Bucket:          cfg.S3Bucket(),
		Region:          cfg.S3Region(),
		UsePathStyle:    cfg.S3UsePathStyle(),
	})
	if err != nil {
		exitWorker(logger, "storage_connect", logging.ErrorClassOf(err))
		return
	}
	startupCtx, cancelStartup := context.WithTimeout(ctx, cfg.ReadinessTimeout())
	if err := storageClient.CheckBucket(startupCtx); err != nil {
		cancelStartup()
		exitWorker(logger, "storage_check", logging.ErrorClassOf(err))
		return
	}

	redisConnection, err := queue.NewConnection(cfg.Redis())
	if err != nil {
		exitWorker(logger, "redis_config", logging.ErrorClassOf(err))
		return
	}
	queueClient := redisConnection.NewClient()
	defer queueClient.Close()
	redisHealth := redisConnection.NewHealthClient()
	defer redisHealth.Close()
	if err := redisHealth.Ping(startupCtx); err != nil {
		cancelStartup()
		exitWorker(logger, "redis_check", logging.ErrorClassOf(err))
		return
	}
	cancelStartup()

	writer, err := outbox.NewWriter(cfg.Admission().ProtectedPayloadKeyVersion(), []byte(cfg.Admission().ProtectedPayloadKey().Expose()))
	if err != nil {
		exitWorker(logger, "outbox_build", logging.ErrorClassOf(err))
		return
	}
	configuredScanner, err := media.NewConfiguredScanner(string(cfg.MediaScannerMode()), string(cfg.Environment()))
	if err != nil {
		exitWorker(logger, "scanner_build", logging.ErrorClassOf(err))
		return
	}
	scanner, err := media.NewScannerAdapter(configuredScanner)
	if err != nil {
		exitWorker(logger, "scanner_adapter_build", logging.ErrorClassOf(err))
		return
	}
	processor, err := media.NewFFmpegProcessor(storageClient, cfg.FFmpegBinaryPath(), cfg.FFprobeBinaryPath(), cfg.MediaProcessingTimeout())
	if err != nil {
		exitWorker(logger, "media_processor_build", logging.ErrorClassOf(err))
		return
	}
	worker, err := media.NewWorker(media.WorkerOptions{
		DB: pool, Scanner: scanner, Process: processor, Outbox: writer,
		ProcessingTimeout:    cfg.MediaProcessingTimeout(),
		TranscodeConcurrency: cfg.MediaTranscodeConcurrency(),
		ObserveTranscode: func(observation media.TranscodeObservation) {
			logger.WorkerTranscode(logging.WorkerTranscodeEvent{
				Phase: string(observation.Phase), OperationID: observation.OperationID,
				Active: observation.Active, Limit: observation.Limit, Outcome: observation.Outcome,
			})
		},
		AutoRecoveryStateAvailable:     autoRecoveryStateAvailable,
		AutoEnhancementRecoveryEnabled: cfg.MediaAutoEnhancementRecoveryEnabled(),
		ObserveAutoRecovery: func(observation media.AutoRecoveryObservation) {
			logger.MediaAutoRecovery(logging.MediaAutoRecoveryEvent{
				Phase: string(observation.Phase), AssetVersionID: observation.AssetVersionID,
				IntentID: observation.IntentID, OperationID: observation.OperationID,
				AttemptNumber: observation.AttemptNumber, ConsecutiveFailures: observation.Failures,
				FailureCategory:      observation.FailureCategory,
				NextAttemptInSeconds: int(observation.NextAttemptIn / time.Second),
			})
		},
	})
	if err != nil {
		exitWorker(logger, "media_worker_build", logging.ErrorClassOf(err))
		return
	}
	logger.WorkerConfiguration(logging.WorkerConfigurationEvent{
		MediaTranscodeConcurrency: cfg.MediaTranscodeConcurrency(),
	})
	dispatcher, err := media.NewDispatcher(pool, queueClient, cfg.MediaProcessingTimeout())
	if err != nil {
		exitWorker(logger, "media_dispatcher_build", logging.ErrorClassOf(err))
		return
	}

	emailDispatcher, err := buildTransactionalEmailDispatcher(transactionalEmailDependencies{
		pool: pool, config: cfg, outbox: writer, observer: logger,
	})
	if err != nil {
		exitWorker(logger, "email_dispatcher_build", logging.ErrorClassOf(err))
		return
	}

	mux := asynq.NewServeMux()
	if err := worker.Register(mux); err != nil {
		exitWorker(logger, "worker_registration", logging.ErrorClassOf(err))
		return
	}
	server := redisConnection.NewServer(queue.ServerOptions{
		Logger: workerQueueLogger{logger: logger},
		ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
			taskID, _ := asynq.GetTaskID(ctx)
			retryCount, _ := asynq.GetRetryCount(ctx)
			maxRetry, _ := asynq.GetMaxRetry(ctx)
			logger.WorkerFailed(logging.WorkerFailureEvent{
				Operation: "job_process", ErrorClass: logging.ErrorClassOf(err), JobType: task.Type(),
				TaskID: taskID, RetryCount: retryCount, MaxRetry: maxRetry,
			})
		}),
		HealthCheckFunc: func(err error) {
			if err != nil {
				logger.WorkerFailed(logging.WorkerFailureEvent{
					Operation: "redis_health", ErrorClass: logging.ErrorClassOf(err), RetryCount: -1, MaxRetry: -1,
				})
			}
		},
	})
	if err := server.Start(mux); err != nil {
		exitWorker(logger, "worker_start", logging.ErrorClassOf(err))
		return
	}
	logger.WorkerLifecycle(logging.WorkerReady)

	thumbnailCleanupDone := make(chan struct{})
	go func() { defer close(thumbnailCleanupDone); runThumbnailCleanup(ctx, pool, storageClient, logger) }()
	dispatcherDone := make(chan struct{})
	go func() {
		defer close(dispatcherDone)
		runMediaDispatcher(ctx, dispatcher, logger)
	}()
	mediaRecoveryDone := make(chan struct{})
	go func() {
		defer close(mediaRecoveryDone)
		runMediaRecovery(ctx, worker, logger)
	}()
	// Sibling of the stale-media recovery loop above, and started ONLY when the
	// flag and the schema both permit it. When it is not started nothing about
	// automatic recovery runs: no candidate query, no scheduler write, no outbox
	// intent.
	autoRecoveryDone := make(chan struct{})
	go func() {
		defer close(autoRecoveryDone)
		if worker.AutoEnhancementRecoveryEnabled() {
			runMediaAutoEnhancementRecovery(ctx, worker, logger)
		}
	}()
	emailDispatcherDone := make(chan struct{})
	go func() {
		defer close(emailDispatcherDone)
		if emailDispatcher != nil {
			runEmailDispatcher(ctx, emailDispatcher, logger)
		}
	}()

	<-ctx.Done()
	logger.WorkerLifecycle(logging.WorkerDraining)
	server.Shutdown()
	<-dispatcherDone
	<-mediaRecoveryDone
	<-autoRecoveryDone
	<-emailDispatcherDone
	<-thumbnailCleanupDone
	logger.WorkerLifecycle(logging.WorkerStopped)
}

type transactionalEmailDependencies struct {
	pool     *pgxpool.Pool
	config   *config.Config
	outbox   *outbox.Writer
	observer transactionalemail.Observer
}

func buildTransactionalEmailDispatcher(dependencies transactionalEmailDependencies) (*transactionalemail.Dispatcher, error) {
	if !dependencies.config.Email().Enabled() {
		return nil, nil
	}
	repository, err := transactionalemail.NewRepository(dependencies.pool)
	if err != nil {
		return nil, err
	}
	renderer, err := transactionalemail.NewRenderer(transactionalemail.RendererOptions{
		PublicOrigin: dependencies.config.PublicOrigin(), FromAddress: dependencies.config.Email().FromAddress(),
		FromName: dependencies.config.Email().FromName(), ReplyTo: dependencies.config.Email().ReplyTo(),
	})
	if err != nil {
		return nil, err
	}
	sender, err := transactionalEmailSender(dependencies.config.Email())
	if err != nil {
		return nil, err
	}
	return transactionalemail.NewDispatcher(transactionalemail.DispatcherOptions{
		Repository: repository, Outbox: dependencies.outbox, Renderer: renderer, Sender: sender,
		Observer: dependencies.observer, LeaseDuration: dependencies.config.Email().Timeout() + time.Minute,
	})
}

func transactionalEmailSender(settings config.EmailSettings) (transactionalemail.Sender, error) {
	switch settings.Provider() {
	case config.EmailProviderFake:
		return transactionalemail.NewFakeSender(), nil
	case config.EmailProviderMailpit:
		return transactionalemail.NewMailpitSender(transactionalemail.MailpitOptions{Address: settings.SMTPAddress(), Timeout: settings.Timeout()})
	case config.EmailProviderResend:
		return transactionalemail.NewResendSender(transactionalemail.ResendOptions{APIKey: settings.APIKey(), Timeout: settings.Timeout()})
	default:
		return nil, errors.New("unsupported transactional email provider")
	}
}

func runEmailDispatcher(ctx context.Context, dispatcher *transactionalemail.Dispatcher, logger *logging.Logger) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := dispatcher.DispatchPending(ctx, 25); err != nil && !errors.Is(err, context.Canceled) {
			logger.WorkerFailed(logging.WorkerFailureEvent{
				Operation: "transactional_email_dispatch", ErrorClass: logging.ErrorClassOf(err), RetryCount: -1, MaxRetry: -1,
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func exitWorker(logger *logging.Logger, operation, errorClass string) {
	logger.WorkerFailed(logging.WorkerFailureEvent{
		Operation: operation, ErrorClass: errorClass, RetryCount: -1, MaxRetry: -1,
	})
	os.Exit(1)
}

type workerQueueLogger struct{ logger *logging.Logger }

func (l workerQueueLogger) Debug(...interface{}) {}
func (l workerQueueLogger) Info(...interface{})  {}
func (l workerQueueLogger) Warn(...interface{}) {
	l.failed("queue_runtime_warning")
}
func (l workerQueueLogger) Error(...interface{}) {
	l.failed("queue_runtime_error")
}
func (l workerQueueLogger) Fatal(...interface{}) {
	l.failed("queue_runtime_fatal")
	os.Exit(1)
}
func (l workerQueueLogger) failed(classification string) {
	l.logger.WorkerFailed(logging.WorkerFailureEvent{
		Operation: "queue_runtime", ErrorClass: classification, RetryCount: -1, MaxRetry: -1,
	})
}

func runMediaDispatcher(ctx context.Context, dispatcher *media.Dispatcher, logger *logging.Logger) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := dispatcher.DispatchPending(ctx, 50); err != nil {
			logger.WorkerFailed(logging.WorkerFailureEvent{
				Operation: "media_outbox_dispatch", ErrorClass: logging.ErrorClassOf(err), RetryCount: -1, MaxRetry: -1,
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runMediaAutoEnhancementRecovery is the 3C-C reconciler: a worker-local sibling
// of runMediaRecovery, on the same interval and batch size, because the work it
// does is the same shape — a bounded scan of durable database state that
// converges whether one worker runs it or several do.
//
// It is never the source of correctness. Every decision it makes is a
// compare-and-set against state 0045 owns, so a second reconciler on another
// worker produces one intent rather than two, and a reconciler that dies mid-pass
// loses nothing.
func runMediaAutoEnhancementRecovery(ctx context.Context, worker *media.Worker, logger *logging.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := worker.ScheduleAutoEnhancementRecovery(ctx, 25); err != nil && !errors.Is(err, context.Canceled) {
			logger.WorkerFailed(logging.WorkerFailureEvent{
				Operation: "media_auto_enhancement_recovery", ErrorClass: logging.ErrorClassOf(err),
				RetryCount: -1, MaxRetry: -1,
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func runMediaRecovery(ctx context.Context, worker *media.Worker, logger *logging.Logger) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		if _, err := worker.RecoverStale(ctx, 25); err != nil && !errors.Is(err, context.Canceled) {
			logger.WorkerFailed(logging.WorkerFailureEvent{
				Operation: "media_stale_work_recovery", ErrorClass: logging.ErrorClassOf(err), RetryCount: -1, MaxRetry: -1,
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
