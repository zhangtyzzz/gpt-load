// Package app provides the main application logic and lifecycle management.
package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"gpt-load/internal/config"
	database "gpt-load/internal/db"
	db "gpt-load/internal/db/migrations"
	"gpt-load/internal/i18n"
	"gpt-load/internal/keypool"
	"gpt-load/internal/models"
	"gpt-load/internal/proxy"
	"gpt-load/internal/services"
	"gpt-load/internal/store"
	"gpt-load/internal/types"
	"gpt-load/internal/version"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.uber.org/dig"
	"gorm.io/gorm"
)

// App holds all services and manages the application lifecycle.
type App struct {
	engine            *gin.Engine
	configManager     types.ConfigManager
	settingsManager   *config.SystemSettingsManager
	groupManager      *services.GroupManager
	logCleanupService *services.LogCleanupService
	requestLogService *services.RequestLogService
	cronChecker       *keypool.CronChecker
	keyPoolProvider   *keypool.KeyProvider
	proxyServer       *proxy.ProxyServer
	storage           store.Store
	db                *gorm.DB
	httpServer        *http.Server
}

// AppParams defines the dependencies for the App.
type AppParams struct {
	dig.In
	Engine            *gin.Engine
	ConfigManager     types.ConfigManager
	SettingsManager   *config.SystemSettingsManager
	GroupManager      *services.GroupManager
	LogCleanupService *services.LogCleanupService
	RequestLogService *services.RequestLogService
	CronChecker       *keypool.CronChecker
	KeyPoolProvider   *keypool.KeyProvider
	ProxyServer       *proxy.ProxyServer
	Storage           store.Store
	DB                *gorm.DB
}

type masterKeyPoolLoader interface {
	LoadKeysFromDB() error
}

// initializeMasterStorage rebuilds only the database-derived key-pool
// projection. The Store also owns pending-durable logs and transient
// coordination state, so normal startup must never call Store.Clear here.
// Keeping this policy at a narrow lifecycle boundary makes the no-clear
// invariant directly regression-testable with a guarded Store.
func initializeMasterStorage(storage store.Store, keyPool masterKeyPoolLoader) error {
	if storage == nil {
		return fmt.Errorf("master storage is not configured")
	}
	if keyPool == nil {
		return fmt.Errorf("master key pool is not configured")
	}
	return keyPool.LoadKeysFromDB()
}

// NewApp is the constructor for App, with dependencies injected by dig.
func NewApp(params AppParams) *App {
	return &App{
		engine:            params.Engine,
		configManager:     params.ConfigManager,
		settingsManager:   params.SettingsManager,
		groupManager:      params.GroupManager,
		logCleanupService: params.LogCleanupService,
		requestLogService: params.RequestLogService,
		cronChecker:       params.CronChecker,
		keyPoolProvider:   params.KeyPoolProvider,
		proxyServer:       params.ProxyServer,
		storage:           params.Storage,
		db:                params.DB,
	}
}

// Start runs the application, it is a non-blocking call.
func (a *App) Start() error {
	// 初始化 i18n
	if err := i18n.Init(); err != nil {
		return fmt.Errorf("failed to initialize i18n: %w", err)
	}
	logrus.Info("i18n initialized successfully.")

	// Master 节点执行初始化
	if a.configManager.IsMaster() {
		logrus.Info("Starting as Master Node.")

		// 数据库迁移
		db.HandleLegacyIndexes(a.db)
		if err := a.db.AutoMigrate(
			&models.SystemSetting{},
			&models.Group{},
			&models.GroupSubGroup{},
			&models.APIKey{},
			&models.RequestLog{},
			&models.GroupHourlyStat{},
		); err != nil {
			return fmt.Errorf("database auto-migration failed: %w", err)
		}
		// 数据修复
		if err := db.MigrateDatabase(a.db); err != nil {
			return fmt.Errorf("database data migration failed: %w", err)
		}
		logrus.Info("Database auto-migration completed.")

		// 初始化系统设置
		if err := a.settingsManager.EnsureSettingsInitialized(a.configManager.GetAuthConfig()); err != nil {
			return fmt.Errorf("failed to initialize system settings: %w", err)
		}
		logrus.Info("System settings initialized in DB.")

		if err := a.settingsManager.Initialize(a.storage, a.groupManager, a.configManager.IsMaster()); err != nil {
			return fmt.Errorf("failed to initialize system settings manager: %w", err)
		}

		// Rebuild only the database-derived key pool. Pending logs, affinity,
		// cooldowns, and task coordination in the shared Store must survive.
		if err := initializeMasterStorage(a.storage, a.keyPoolProvider); err != nil {
			return fmt.Errorf("failed to load keys into key pool: %w", err)
		}
		logrus.Debug("API keys loaded into Redis cache by master.")

	} else {
		logrus.Info("Starting as Slave Node.")
		if err := a.settingsManager.Initialize(a.storage, a.groupManager, a.configManager.IsMaster()); err != nil {
			return fmt.Errorf("failed to initialize system settings manager: %w", err)
		}
	}

	// 显示配置并启动所有后台服务
	a.configManager.DisplayServerConfig()

	if err := a.groupManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize group manager: %w", err)
	}
	databaseIdleMode := a.configManager.GetDatabaseConfig().IdleMode
	if databaseIdleMode {
		if err := database.EnableIdleMode(a.db); err != nil {
			return fmt.Errorf("failed to enable database idle mode: %w", err)
		}
		a.requestLogService.EnableDatabaseIdleMode()
		logrus.Info("Database idle mode enabled: key validation and log cleanup are driven by real traffic; idle SQL connections will not be retained")
	}

	// Start master-only background services only after every fallible cache
	// initialization has completed, so a startup error cannot leave workers
	// running behind a server that never became ready.
	if a.configManager.IsMaster() {
		a.requestLogService.Start()
		if databaseIdleMode {
			a.logCleanupService.StartActivityDriven()
			a.cronChecker.StartActivityDriven()
		} else {
			a.logCleanupService.Start()
			a.cronChecker.Start()
		}
	}

	// Create HTTP server
	serverConfig := a.configManager.GetEffectiveServerConfig()
	a.httpServer = &http.Server{
		Addr:           fmt.Sprintf("%s:%d", serverConfig.Host, serverConfig.Port),
		Handler:        a.engine,
		ReadTimeout:    time.Duration(serverConfig.ReadTimeout) * time.Second,
		WriteTimeout:   time.Duration(serverConfig.WriteTimeout) * time.Second,
		IdleTimeout:    time.Duration(serverConfig.IdleTimeout) * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Start HTTP server in a new goroutine
	go func() {
		logrus.Infof("GPT-Load proxy server started successfully on Version: %s", version.Version)
		logrus.Infof("Server address: http://%s:%d", serverConfig.Host, serverConfig.Port)
		logrus.Info("")
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Fatalf("Server startup failed: %v", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the application.
func (a *App) Stop(ctx context.Context) {
	logrus.Info("Shutting down server...")

	serverConfig := a.configManager.GetEffectiveServerConfig()
	totalTimeout := time.Duration(serverConfig.GracefulShutdownTimeout) * time.Second

	// 动态计算 HTTP 关机超时时间，为后台服务固定预留 5 秒
	httpShutdownTimeout := totalTimeout - 5*time.Second
	httpShutdownCtx, cancelHttpShutdown := context.WithTimeout(context.Background(), httpShutdownTimeout)
	defer cancelHttpShutdown()

	logrus.Debugf("Attempting to gracefully shut down HTTP server (max %v)...", httpShutdownTimeout)
	if err := a.httpServer.Shutdown(httpShutdownCtx); err != nil {
		logrus.Debugf("HTTP server graceful shutdown timed out as expected, forcing remaining connections to close.")
		if closeErr := a.httpServer.Close(); closeErr != nil {
			logrus.Errorf("Error forcing HTTP server to close: %v", closeErr)
		}
	}
	logrus.Info("HTTP server has been shut down.")

	// 使用原始的总超时 context 继续关闭其他后台服务
	stoppableServices := []func(context.Context){
		a.groupManager.Stop,
		a.settingsManager.Stop,
	}

	if serverConfig.IsMaster {
		stoppableServices = append(stoppableServices,
			a.cronChecker.Stop,
			a.logCleanupService.Stop,
			a.requestLogService.Stop,
		)
	}

	var wg sync.WaitGroup
	wg.Add(len(stoppableServices))

	for _, stopFunc := range stoppableServices {
		go func(stop func(context.Context)) {
			defer wg.Done()
			stop(ctx)
		}(stopFunc)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logrus.Info("All background services stopped.")
	case <-ctx.Done():
		logrus.Warn("Shutdown timed out, some services may not have stopped gracefully.")
	}

	if a.storage != nil {
		if err := a.storage.Close(); err != nil {
			logrus.WithError(err).Error("Failed to close storage")
		}
	}

	logrus.Info("Server exited gracefully")
}
