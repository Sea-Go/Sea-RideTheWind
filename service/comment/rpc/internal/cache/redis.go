package cache

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

func InitRedis(Host string) *redis.Client {
	if Host == "" {
		log.Fatalln("redis host is empty")
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: Host,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		log.Fatalln(fmt.Errorf("redis ping failed, host=%s: %w", Host, err))
	}
	return rdb
}
