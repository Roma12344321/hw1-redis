package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

const (
	playerKeyPrefix   = "player:"
	cacheKeyPrefix    = "cache:player:"
	loginKeyPrefix    = "logins:"
	achievementPrefix = "achievements:"

	leaderboardKey = "tournament:main"
	streamKey      = "notifications"
	streamGroup    = "notifications-group"

	cacheTTL  = 60 * time.Second
	loginTTL  = 24 * time.Hour
	streamTTL = 7 * 24 * time.Hour
)

var loginScript = redis.NewScript(`
local current = redis.call('INCR', KEYS[1])
redis.call('EXPIRE', KEYS[1], ARGV[1])
return current
`)

type Player struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Level     int64  `json:"level"`
	Region    string `json:"region"`
	CreatedAt int64  `json:"created_at"`
}

type createPlayerRequest struct {
	Name   string `json:"name"`
	Level  int64  `json:"level"`
	Region string `json:"region"`
}

type updateLevelRequest struct {
	Delta int64 `json:"delta"`
}

type scoreRequest struct {
	PlayerID string  `json:"player_id"`
	Score    float64 `json:"score"`
}

type achievementRequest struct {
	Name string `json:"name"`
}

type batchPlayer struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Level  int64  `json:"level"`
	Region string `json:"region"`
}

type batchRequest struct {
	Players []batchPlayer `json:"players"`
}

type Handler struct {
	redis *redis.Client
}

func New(redis *redis.Client) *Handler {
	h := &Handler{redis: redis}

	go h.consumeNotifications()

	return h
}

func playerKey(id string) string      { return playerKeyPrefix + id }
func cacheKey(id string) string       { return cacheKeyPrefix + id }
func loginKey(id string) string       { return loginKeyPrefix + id }
func achievementKey(id string) string { return achievementPrefix + id }

func isBusyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}

func errJSON(c echo.Context, status int, msg string) error {
	return c.JSON(status, map[string]string{"error": msg})
}

func playerFromMap(id string, m map[string]string) Player {
	p := Player{ID: id}
	p.Name = m["name"]
	p.Region = m["region"]
	p.Level, _ = strconv.ParseInt(m["level"], 10, 64)
	p.CreatedAt, _ = strconv.ParseInt(m["created_at"], 10, 64)
	return p
}

func (h *Handler) initNotificationGroup(ctx context.Context) error {
	err := h.redis.XGroupCreateMkStream(ctx, streamKey, streamGroup, "$").Err()
	if err != nil && !isBusyGroup(err) {
		return err
	}
	return nil
}

func (h *Handler) consumeNotifications() {
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := h.initNotificationGroup(ctx)
		cancel()

		if err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}

	ctx := context.Background()
	for {
		streams, err := h.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    streamGroup,
			Consumer: "gamehub-consumer",
			Streams:  []string{streamKey, ">"},
			Count:    10,
			Block:    5 * time.Second,
		}).Result()

		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			if strings.Contains(err.Error(), "NOGROUP") {
				_ = h.initNotificationGroup(ctx)
				time.Sleep(time.Second)
				continue
			}
			log.Printf("notifications read: %v", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				processNotification(msg.Values)
				if err := h.redis.XAck(ctx, streamKey, streamGroup, msg.ID).Err(); err != nil {
					log.Printf("notifications ack %s: %v", msg.ID, err)
				}
			}
		}
	}
}

func processNotification(values map[string]interface{}) {
	playerID, _ := values["player_id"].(string)
	ntype, _ := values["type"].(string)
	message, _ := values["message"].(string)
	timestamp, _ := values["timestamp"].(string)

	log.Printf("[notification] player_id=%s type=%s timestamp=%s message=%s",
		playerID, ntype, timestamp, message)
}

func (h *Handler) CreateOrUpdatePlayer(c echo.Context) error {
	id := c.Param("id")
	var req createPlayerRequest
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid body")
	}

	ctx := c.Request().Context()
	err := h.redis.HSet(ctx, playerKey(id), map[string]interface{}{
		"name":       req.Name,
		"level":      req.Level,
		"region":     req.Region,
		"created_at": time.Now().Unix(),
	}).Err()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	h.redis.Del(ctx, cacheKey(id))

	return c.JSON(http.StatusOK, Player{
		ID: id, Name: req.Name, Level: req.Level, Region: req.Region,
		CreatedAt: time.Now().Unix(),
	})
}

func (h *Handler) GetPlayer(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	cached, err := h.redis.Get(ctx, cacheKey(id)).Result()
	if err == nil {
		return c.JSONBlob(http.StatusOK, []byte(cached))
	}
	if err != redis.Nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	fields, err := h.redis.HGetAll(ctx, playerKey(id)).Result()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	if len(fields) == 0 {
		return errJSON(c, http.StatusNotFound, "player not found")
	}

	p := playerFromMap(id, fields)

	raw, _ := json.Marshal(p)
	if err := h.redis.Set(ctx, cacheKey(id), raw, cacheTTL).Err(); err != nil {
		log.Printf("cache set %s: %v", id, err)
	}

	return c.JSON(http.StatusOK, p)
}

func (h *Handler) UpdateLevel(c echo.Context) error {
	id := c.Param("id")
	var req updateLevelRequest
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid body")
	}

	ctx := c.Request().Context()
	newLevel, err := h.redis.HIncrBy(ctx, playerKey(id), "level", req.Delta).Result()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	h.redis.Del(ctx, cacheKey(id))

	minID := fmt.Sprintf("%d-0", time.Now().Add(-streamTTL).UnixMilli())

	_, err = h.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		MinID:  minID,
		Approx: true,
		Values: map[string]interface{}{
			"player_id": id,
			"type":      "level_change",
			"message":   fmt.Sprintf("player %s level changed to %d", id, newLevel),
			"timestamp": time.Now().UnixMilli(),
		},
	}).Result()
	if err != nil {
		log.Printf("xadd notification: %v", err)
	}

	return c.JSON(http.StatusOK, map[string]int64{"level": newLevel})
}

func (h *Handler) Login(c echo.Context) error {
	id := c.Param("id")
	ctx := c.Request().Context()

	count, err := loginScript.Run(ctx, h.redis,
		[]string{loginKey(id)}, int(loginTTL.Seconds())).Int()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]int64{"logins": int64(count)})
}

func (h *Handler) AddScore(c echo.Context) error {
	var req scoreRequest
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid body")
	}

	err := h.redis.ZIncrBy(c.Request().Context(), leaderboardKey, req.Score, req.PlayerID).Err()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]string{"ok": "score added"})
}

func (h *Handler) TopPlayers(c echo.Context) error {
	limit := 10
	if v := c.QueryParam("limit"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	entries, err := h.redis.ZRevRangeWithScores(
		c.Request().Context(), leaderboardKey, 0, int64(limit-1)).Result()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	type entry struct {
		PlayerID string  `json:"player_id"`
		Score    float64 `json:"score"`
	}
	out := make([]entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, entry{PlayerID: e.Member.(string), Score: e.Score})
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) PlayerRank(c echo.Context) error {
	playerID := c.Param("playerId")

	rank, err := h.redis.ZRank(c.Request().Context(), leaderboardKey, playerID).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return errJSON(c, http.StatusNotFound, "player not in leaderboard")
		}
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]int64{"rank": rank})
}

func (h *Handler) AddAchievement(c echo.Context) error {
	id := c.Param("id")
	var req achievementRequest
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid body")
	}

	err := h.redis.SAdd(c.Request().Context(), achievementKey(id), req.Name).Err()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]bool{"added": true})
}

func (h *Handler) HasAchievement(c echo.Context) error {
	id := c.Param("id")
	name := c.Param("name")

	member, err := h.redis.SIsMember(c.Request().Context(), achievementKey(id), name).Result()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]bool{"achieved": member})
}

func (h *Handler) CommonAchievements(c echo.Context) error {
	id1 := c.Param("id1")
	id2 := c.Param("id2")

	common, err := h.redis.SInter(
		c.Request().Context(), achievementKey(id1), achievementKey(id2)).Result()
	if err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, common)
}

func (h *Handler) BatchCreatePlayers(c echo.Context) error {
	var req batchRequest
	if err := c.Bind(&req); err != nil {
		return errJSON(c, http.StatusBadRequest, "invalid body")
	}
	if len(req.Players) == 0 {
		return errJSON(c, http.StatusBadRequest, "empty players list")
	}

	ctx := c.Request().Context()
	now := time.Now().Unix()

	start := time.Now()
	pipe := h.redis.Pipeline()
	for _, p := range req.Players {
		pipe.HSet(ctx, playerKey(p.ID), map[string]interface{}{
			"name":       p.Name,
			"level":      p.Level,
			"region":     p.Region,
			"created_at": now,
		})
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return errJSON(c, http.StatusInternalServerError, err.Error())
	}
	elapsed := time.Since(start)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"created":    len(req.Players),
		"elapsed_ms": elapsed.Milliseconds(),
	})
}
