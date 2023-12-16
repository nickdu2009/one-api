package common

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis/extra/redisotel/v8"
	"github.com/go-redis/redis/v8"
	"os"
	"time"
)

var RDB *redis.Client
var RedisEnabled = true

// InitRedisClient This function is called after init()
func InitRedisClient(ctx context.Context) (err error) {
	if os.Getenv("REDIS_CONN_STRING") == "" {
		RedisEnabled = false
		SysLog("REDIS_CONN_STRING not set, Redis is not enabled")
		return nil
	}
	if os.Getenv("SYNC_FREQUENCY") == "" {
		RedisEnabled = false
		SysLog("SYNC_FREQUENCY not set, Redis is disabled")
		return nil
	}
	SysLog("Redis is enabled")
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	RDB = redis.NewClient(opt)
	RDB.AddHook(redisotel.NewTracingHook())
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	RDB.PoolStats()
	_, err = RDB.Ping(ctx).Result()
	if err != nil {
		FatalLog("Redis ping test failed: " + err.Error())
		return err
	}
	go func() {
		for {
			time.Sleep(time.Second)
			data, _ := json.Marshal(RDB.PoolStats())
			SysLog(fmt.Sprintf("redis db stats %s", data))
		}
	}()
	return nil
}

func ParseRedisOption() *redis.Options {
	opt, err := redis.ParseURL(os.Getenv("REDIS_CONN_STRING"))
	if err != nil {
		FatalLog("failed to parse Redis connection string: " + err.Error())
	}
	return opt
}

func RedisSet(ctx context.Context, key string, value string, expiration time.Duration) error {
	return RDB.Set(ctx, key, value, expiration).Err()
}

func RedisGet(ctx context.Context, key string) (string, error) {
	return RDB.Get(ctx, key).Result()
}

func RedisDel(ctx context.Context, key string) error {
	return RDB.Del(ctx, key).Err()
}

func RedisDecrease(ctx context.Context, key string, value int64) error {
	return RDB.DecrBy(ctx, key, value).Err()
}
