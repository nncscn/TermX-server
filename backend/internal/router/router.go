package router

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ky/internal/config"
	"ky/internal/handler"
	"ky/internal/middleware"
	"ky/internal/model"
	"ky/internal/pkg/console"
	"ky/internal/pkg/keyring"
	"ky/internal/pkg/termxkey"
	"ky/internal/repository"
	"ky/internal/service"
	"ky/internal/web"
)

// Setup 组装依赖并返回 gin 引擎
func Setup(cfg *config.Config, db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	// 安全：不信任任何代理头。否则默认信任所有代理，ClientIP() 会采信可伪造的
	// X-Forwarded-For,验证码限流可被轮换假 XFF 绕过（直连部署无需代理链解析）。
	if err := engine.SetTrustedProxies(nil); err != nil {
		log.Fatalf("设置可信代理失败: %v", err)
	}
	// 请求日志不记录 query 参数（凭据搜索词等元数据不落日志）
	engine.Use(gin.LoggerWithConfig(gin.LoggerConfig{SkipPaths: nil, Formatter: func(p gin.LogFormatterParams) string {
		return fmt.Sprintf("[GIN] %v | %3d | %13v | %15s | %-7s %s\n",
			p.TimeStamp.Format("2006/01/02 - 15:04:05"),
			p.StatusCode, p.Latency, p.ClientIP, p.Method, p.Path)
	}}), gin.Recovery(), middleware.Security(), middleware.CORS(cfg.Server.AllowedOrigins))

	// 登录加密密钥：文件不存在则生成（密钥不入库）
	key, err := keyring.LoadOrCreate(cfg.Security.KeyFile)
	if err != nil {
		log.Fatalf("加载加密密钥失败: %v", err)
	}

	// 依赖装配：router → handler → service → repository，与规范一致
	authRepo := repository.NewAuthRepository(db)
	keyRepo0 := repository.NewSshKeyRepository(db)
	credRepo0 := repository.NewCredentialRepository(db)
	authSvc := service.NewAuthService(authRepo, keyRepo0, credRepo0, key)
	authHandler := handler.NewAuthHandler(authSvc)

	keyRepo := repository.NewSshKeyRepository(db)
	credRepo := repository.NewCredentialRepository(db)
	keySvc := service.NewKeyService(keyRepo, credRepo, authSvc)
	credSvc := service.NewCredentialService(credRepo, keyRepo, authSvc)
	trashSvc := service.NewTrashService(keyRepo, credRepo)
	dataSvc := service.NewDataService(keyRepo, credRepo, authSvc)
	keyHandler := handler.NewKeyHandler(keySvc)
	credHandler := handler.NewCredentialHandler(credSvc)
	trashHandler := handler.NewTrashHandler(trashSvc)
	dataHandler := handler.NewDataHandler(dataSvc)

	prefRepo := repository.NewPreferenceRepository(db)
	prefSvc := service.NewPreferenceService(prefRepo)
	prefHandler := handler.NewPreferenceHandler(prefSvc)
	dbCfgSvc := service.NewDbConfigService(cfg, db, authSvc)
	dbCfgHandler := handler.NewDbConfigHandler(dbCfgSvc)

	// 明文 HTTPS + X-Server-Key 工作区身份 + X-Vault-Token 仓库第二道门
	// 可配置的是"基地址前缀"（默认 /api,客户端自行拼接 client/… 子路径）;
	//旧配置存过完整前缀 /api/client 的自动剥掉尾部 /client 兼容
	termxPrefix := cfg.Server.TermxBasePath
	if termxPrefix == "" {
		termxPrefix = "/api"
	}
	termxPrefix = strings.TrimRight(termxPrefix, "/")
	termxPrefix = strings.TrimSuffix(termxPrefix, "/client")
	termxKeyVal, err := termxkey.Resolve(cfg.Security.TermxApiKey, key, "data/termx-key.bin")
	if err != nil {
		log.Fatalf("解析 TermX 同步密钥失败: %v", err)
	}
	termxRepo := repository.NewTermxRepository(db)
	termxSvc := service.NewTermxService(termxRepo, authSvc, termxKeyVal)
	authSvc.AfterVaultReady = func(uid uint) {
		termxSvc.LazyTranslateAll(context.Background(), uid)
	}
	credSvc.SetTermxBridge(func(uid uint, c *model.Credential) {
		termxSvc.SyncNativeToTermx(uid, c)
	})
	termxHandler := handler.NewTermxHandler(termxSvc, termxKeyVal)
	client := engine.Group(termxPrefix + "/client")
	{
		client.POST("/ext/verify", termxHandler.Verify)
		ext := client.Group("/ext/vault", middleware.TermxKey(termxKeyVal, termxRepo.FirstAccountID))
		{
			ext.POST("/params", termxHandler.VaultParams)
			ext.POST("/setup", termxHandler.VaultSetup)
			ext.POST("/unlock", termxHandler.VaultUnlock)
		}
		//条目同步（双令牌 + 限流：每 IP 每分钟 60 次）
		termxRL := middleware.NewRateLimiter(60, time.Minute)
		go termxRL.Cleanup()
		entries := client.Group("/vault/entries",
			middleware.TermxKey(termxKeyVal, termxRepo.FirstAccountID),
			middleware.TermxVault(termxRepo.StateOf),
			termxRL.Handle())
		{
			entries.POST("/list", termxHandler.ListEntries)
			entries.POST("/push", termxHandler.PushEntries)
		}
	}

	accountsReady := false
	if n, err := authRepo.CountAccounts(context.Background()); err == nil && n > 0 {
		accountsReady = true
	}
	console.Startup(cfg.Server.Addr(), displayAccess(cfg), termxPrefix, accountsReady)

	// 播种初始管理员（仅显式配置 seed_admin 且账户表为空时的运维兜底；
	if cfg.Security.SeedAdmin && !accountsReady {
		if err := authSvc.SeedAdmin(context.Background(), cfg.Security.SeedUsername, cfg.Security.SeedPassword); err != nil {
			log.Fatalf("播种初始账户失败: %v", err)
		}
	}

	api := engine.Group("/api/v1")
	{
		api.GET("/health", handler.Health)

		// 安装引导面（公开）：状态查询始终可用（前端守卫判定需要，仅暴露布尔值）；
		// 测试与完成接口只在未初始化时注册——初始化完成后路由不存在（404）,
		// 彻底作废：既无法重装抢注,也探不到可用接口（封堵）。
		setupSvc := service.NewSetupService(cfg, db, dbCfgSvc, termxKeyVal)
		setupHandler := handler.NewSetupHandler(setupSvc)
		setupRL := middleware.NewRateLimiter(20, time.Minute)
		go setupRL.Cleanup()
		setup := api.Group("/setup", setupRL.Handle())
		{
			setup.GET("/status", setupHandler.Status)
			if !accountsReady {
				setup.POST("/test-connection", setupHandler.TestConnection)
				setup.POST("/test-server", setupHandler.TestServer)
				setup.POST("/complete", setupHandler.Complete)
			}
		}

		auth := api.Group("/auth")
		{
			loginRL := middleware.NewRateLimiter(10, time.Minute)
			go loginRL.Cleanup()
			auth.POST("/login", loginRL.Handle(), authHandler.Login)
			// 验证码出题限流：每 IP 每分钟最多 60 次,超出直接 429（不显示计数,只报过频）
			auth.GET("/captcha", middleware.NewRateLimiter(60, time.Minute).Handle(), authHandler.GetCaptcha)
			auth.POST("/logout", authHandler.Logout)

			auth.POST("/password/change", middleware.Auth(authSvc), authHandler.ChangePassword)
			auth.POST("/recovery/generate", middleware.Auth(authSvc), authHandler.GenerateRecoveryKey)

			// 忘记密码（公开，两步：验恢复密钥 → 凭证重置）
			auth.POST("/forgot/verify", authHandler.ForgotVerify)
			auth.POST("/forgot/reset", authHandler.ForgotReset)
		}

		keys := api.Group("/keys", middleware.Auth(authSvc))
		{
			keys.GET("", keyHandler.List)
			keys.POST("", keyHandler.Create)
			keys.PUT("/:id", keyHandler.Update)
			keys.DELETE("/:id", keyHandler.Delete)
			keys.POST("/:id/restore", keyHandler.Restore)
			keys.POST("/:id/purge", keyHandler.Purge)
			keys.POST("/:id/reveal", keyHandler.Reveal)
		}

		//连接凭据：密文层（主机/账号/密码等）加密存储，reveal 验主密码后返回
		credentials := api.Group("/credentials", middleware.Auth(authSvc))
		{
			credentials.GET("", credHandler.List)
			credentials.POST("", credHandler.Create)
			credentials.PUT("/:id", credHandler.Update)
			credentials.DELETE("/:id", credHandler.Delete)
			credentials.POST("/:id/restore", credHandler.Restore)
			credentials.POST("/:id/purge", credHandler.Purge)
			credentials.POST("/:id/reveal", credHandler.Reveal)
			credentials.POST("/:id/touch", credHandler.Touch)
		}

		// 回收站：密钥与凭据软删条目的合并视图（共用业务表，deleted_at 软删）
		trash := api.Group("/trash", middleware.Auth(authSvc))
		{
			trash.GET("", trashHandler.List)
			trash.POST("/empty", trashHandler.Empty)
			trash.POST("/clean", trashHandler.Clean)
		}

		data := api.Group("/data", middleware.Auth(authSvc))
		{
			data.POST("/export", dataHandler.Export)
			data.POST("/import", dataHandler.Import)
		}

		settings := api.Group("/settings", middleware.Auth(authSvc))
		{
			settings.GET("", prefHandler.Get)
			settings.PUT("", prefHandler.Update)
			settings.GET("/database", dbCfgHandler.Get)
			settings.POST("/database/test", dbCfgHandler.Test)
			settings.PUT("/database", dbCfgHandler.Save)
			settings.POST("/database/revert", dbCfgHandler.Revert)
			settings.POST("/server/check-port", dbCfgHandler.CheckPort)
		}
	}

	web.Register(engine)
	return engine
}

// displayAccess 给用户展示的访问地址：监听 0.0.0.0/:: 时用探测到的局域网 IP
// （远程浏览器可直接打开），回环监听用 127.0.0.1；探测失败回退配置值。
func displayAccess(cfg *config.Config) string {
	host := cfg.Server.Host
	if host == "0.0.0.0" || host == "::" || host == "" {
		if ip := lanIP(); ip != "" {
			return fmt.Sprintf("http://%s:%d", ip, cfg.Server.Port)
		}
		return cfg.Server.DisplayAddr()
	}
	return cfg.Server.DisplayAddr()
}

func lanIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
