package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type CacheRepository struct {
	client *redis.Client
}

func NewCacheRepository(client *redis.Client) *CacheRepository {
	return &CacheRepository{client: client}
}

func (r *CacheRepository) GetLink(ctx context.Context, code string) (string, error) {
	return r.client.Get(ctx, fmt.Sprintf("link:%s", code)).Result()
}

func (r *CacheRepository) SetLink(ctx context.Context, code, url string, ttl time.Duration) error {
	return r.client.Set(ctx, fmt.Sprintf("link:%s", code), url, ttl).Err()
}

func (r *CacheRepository) DeleteLink(ctx context.Context, code string) error {
	return r.client.Del(ctx, fmt.Sprintf("link:%s", code)).Err()
}

func (r *CacheRepository) RateLimitCheck(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	pipe := r.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, err
	}
	return incr.Val() <= int64(limit), nil
}

func (r *CacheRepository) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *CacheRepository) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *CacheRepository) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *CacheRepository) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.Set(ctx, key, string(data), ttl)
}

func (r *CacheRepository) GetJSON(ctx context.Context, key string, dest any) error {
	data, err := r.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), dest)
}

func (r *CacheRepository) IncrementWithTTL(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	pipe := r.client.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, ttl)
	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	return incr.Val(), nil
}

func (r *CacheRepository) GetSuffix(ctx context.Context) (string, error) {
	return r.client.Get(ctx, "admin:suffix").Result()
}

func (r *CacheRepository) SetSuffix(ctx context.Context, suffix string, ttl time.Duration) error {
	return r.client.Set(ctx, "admin:suffix", suffix, ttl).Err()
}

func (r *CacheRepository) DeleteSuffix(ctx context.Context) error {
	return r.client.Del(ctx, "admin:suffix").Err()
}

func (r *CacheRepository) IncrementPV(ctx context.Context, linkID uint64, date string) error {
	return r.client.Incr(ctx, fmt.Sprintf("stats:pv:%d:%s", linkID, date)).Err()
}

func (r *CacheRepository) AddUV(ctx context.Context, linkID uint64, date, ip string) error {
	return r.client.PFAdd(ctx, fmt.Sprintf("stats:uv:%d:%s", linkID, date), ip).Err()
}
