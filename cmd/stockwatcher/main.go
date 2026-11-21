package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	webpush "github.com/SherClockHolmes/webpush-go"

	"stockwatcher/internal/config"
	"stockwatcher/internal/httpapi"
	"stockwatcher/internal/market/binance"
	"stockwatcher/internal/market/finnhub"
	"stockwatcher/internal/market/yahoo"
	"stockwatcher/internal/push"
	"stockwatcher/internal/store"
	"stockwatcher/internal/syncer"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := serve(); err != nil {
			slog.Error("serve", "err", err)
			os.Exit(1)
		}
	case "tick":
		if err := tick(); err != nil {
			slog.Error("tick", "err", err)
			os.Exit(1)
		}
	case "vapid":
		if err := vapid(); err != nil {
			slog.Error("vapid", "err", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: stockwatcher serve | tick | vapid")
}

func serve() error {
	cfg := config.FromEnv()
	if cfg.AccessToken == "" {
		return errors.New("ACCESS_TOKEN is required")
	}
	st, sy, err := build(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(st, sy, cfg).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ranges, err := sy.RefreshRanges(ctx)
	if err != nil {
		return err
	}
	slog.Info("ranges", "updated", ranges)
	go func() {
		logoCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		n, err := sy.RefreshLogos(logoCtx)
		if err != nil {
			slog.Error("logo", "err", err)
			return
		}
		slog.Info("logos", "updated", n)
	}()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	slog.Info("listen", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func tick() error {
	cfg := config.FromEnv()
	st, sy, err := build(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	return sy.Tick(ctx)
}

func vapid() error {
	privateKey, publicKey, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return err
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", publicKey, privateKey)
	return nil
}

func build(cfg config.Config) (*store.Store, *syncer.Syncer, error) {
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, nil, err
	}
	sy := syncer.New(
		st,
		&finnhub.Client{Key: cfg.FinnhubKey},
		&yahoo.Client{},
		&binance.Client{},
		&push.Sender{
			Store:      st,
			Public:     cfg.VAPIDPublic,
			Private:    cfg.VAPIDPrivate,
			Subscriber: cfg.VAPIDSubscriber,
		},
	)
	return st, sy, nil
}
