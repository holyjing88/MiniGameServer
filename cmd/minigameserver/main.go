package main

import (
	"context"
	"log"
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
	"minigameserver/internal/service"
	"minigameserver/internal/store"
	"minigameserver/internal/store/memory"
	storemysql "minigameserver/internal/store/mysql"
)

func main() {
	cfg := config.Load()
	st, err := openStore(cfg)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	sessions := auth.NewSessionManager(cfg.SessionSecret, cfg.SessionTTL)
	profiles, resolver := auth.BuildAuthStack(cfg)
	cache := hotcache.New()
	svc := service.NewWithAuth(cfg, st, cache, sessions, resolver, profiles)
	log.Printf("auth modes=%v tiktok_ready=%v", cfg.AuthModes, cfg.TikTokReady())
	if len(cfg.AuthChannelMap) > 0 {
		log.Printf("auth channel_map=%v", cfg.AuthChannelMap)
	} else {
		log.Printf("auth channel_map=defaults(tiktok_minis/douyin->tiktok, others->mock)")
	}

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(svc).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	lis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Fatalf("grpc listen: %v", err)
	}
	gs := grpc.NewServer()
	grpcapi.Register(gs, svc)

	go func() {
		log.Printf("HTTP listening on %s", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()
	go func() {
		log.Printf("gRPC listening on %s", cfg.GRPCAddr)
		if err := gs.Serve(lis); err != nil {
			log.Fatalf("grpc: %v", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	gs.GracefulStop()
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
		return ms, nil
	default:
		log.Printf("using memory store")
		return memory.New(), nil
	}
}
