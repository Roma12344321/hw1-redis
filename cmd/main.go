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
	addr           = ":8080"
	shutdownTimout = 15 * time.Second
	sentinelAddrs  = "sentinel-1:26379,sentinel-2:26379,sentinel-3:26379"
)

func main() {
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
