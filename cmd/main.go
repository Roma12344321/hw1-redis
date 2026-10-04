package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"hw1/internal/handler"
	"hw1/internal/server"
)

const (
	defaultAddr         = ":8080"
	shutdownTimout      = 15 * time.Second
	defaultSentinelAddr = "localhost:26379,localhost:26380,localhost:26381"
)

func main() {
	addr := os.Getenv("APP_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	sentinelAddrs := os.Getenv("SENTINEL_ADDRS")
	if sentinelAddrs == "" {
		sentinelAddrs = defaultSentinelAddr
	}

	redisCli := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    "mymaster",
		SentinelAddrs: strings.Split(sentinelAddrs, ","),
		ClientName:    "go-back",
	})
	if redisCli.Ping(context.Background()).Err() != nil {
		log.Fatal("ping redis")
	}
	defer redisCli.Close()

	hndlr := handler.New(redisCli)
	srv := server.New(hndlr)

	go func() {
		if err := srv.Start(addr); err != nil {
			log.Fatal(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal(err)
	}
}
