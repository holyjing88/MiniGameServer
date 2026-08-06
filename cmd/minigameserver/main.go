package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	grpcapi "minigameserver/internal/api/grpc"
	httpapi "minigameserver/internal/api/http"
	"minigameserver/internal/auth"
	"minigameserver/internal/config"
	"minigameserver/internal/hotcache"
	"minigameserver/internal/logging"
	"minigameserver/internal/service"
	"minigameserver/internal/store"
	"minigameserver/internal/store/memory"
	storemysql "minigameserver/internal/store/mysql"
)

func main() {
	cfg := config.Load()
	logging.SetDefault(logging.New(logging.ParseLevel(cfg.LogLevel), os.Stdout))
	logging.Info("startup log_level=%s", logging.ParseLevel(cfg.LogLevel))

	st, err := openStore(cfg)
	if err != nil {
		logging.Fatal("store: %v", err)
	}
	defer st.Close()

	sessions := auth.NewSessionManager(cfg.SessionSecret, cfg.SessionTTL)
	profiles, resolver := auth.BuildAuthStack(cfg)
	cache := hotcache.New()
	svc := service.NewWithAuth(cfg, st, cache, sessions, resolver, profiles)
	logging.Info("auth modes=%v tiktok_ready=%v", cfg.AuthModes, cfg.TikTokReady())
	if len(cfg.AuthChannelMap) > 0 {
		logging.Info("auth channel_map=%v", cfg.AuthChannelMap)
	} else {
		logging.Info("auth channel_map=defaults(tiktok_minis/douyin->tiktok, others->mock)")
	}

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           logging.HTTPMiddleware(httpapi.New(svc).Handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		logging.Fatal("grpc listen: %v", err)
	}
	gs := grpc.NewServer(grpc.UnaryInterceptor(logging.UnaryServerInterceptor()))
	grpcapi.Register(gs, svc)

	go func() {
		logging.Info("HTTP listening on %s", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Fatal("http: %v", err)
		}
	}()
	go func() {
		logging.Info("gRPC listening on %s", cfg.GRPCAddr)
		if err := gs.Serve(lis); err != nil {
			logging.Fatal("grpc: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	sig := <-ch
	logging.Info("shutdown signal=%v", sig)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	gs.GracefulStop()
	logging.Info("shutdown complete")
}

func openStore(cfg config.Config) (store.Store, error) {
	switch cfg.StoreDriver {
	case "mysql":
		ms, err := storemysql.Open(cfg.MySQLDSN)
		if err != nil {
			return nil, err
		}
		if err := ms.EnsureSchema(context.Background()); err != nil {
			_ = ms.Close()
			return nil, err
		}
		logging.Info("using mysql store")
		return ms, nil
	default:
		logging.Info("using memory store")
		return memory.New(), nil
	}
}
