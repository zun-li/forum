// Package vote maintains the time and score rankings and per-user votes in Redis.
package vote

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	timeIndex        = "forum:posts:time"
	scoreIndex       = "forum:posts:score"
	postCommunity    = "forum:posts:community"
	deletedCommunity = "deleted"
	voteWindow       = 7 * 24 * time.Hour
	pointsPerVote    = 432
)

var (
	ErrPostNotFound = errors.New("帖子不存在")
	ErrVoteExpired  = errors.New("投票时间已过")
	ErrVoteRepeated = errors.New("不能重复投票")
	ErrInvalidVote  = errors.New("投票参数无效")
)

type Store struct {
	client *redis.Client
}

func NewStore(client *redis.Client) *Store {
	return &Store{client: client}
}

func communityIndex(community, order string) string {
	return "forum:community:" + community + ":" + order
}

func votesKey(post string) string {
	return "forum:post:" + post + ":votes"
}

// A replay may have read MySQL just before deletion. Keep a tombstone in
// the community hash so that a late replay cannot put the post back.
var indexPost = redis.NewScript(`
if redis.call("HGET", KEYS[1], ARGV[1]) == ARGV[4] then return 0 end
redis.call("HSET", KEYS[1], ARGV[1], ARGV[2])
for i = 2, 5 do redis.call("ZADD", KEYS[i], "NX", ARGV[3], ARGV[1]) end
return 1
`)

func (s *Store) IndexPost(ctx context.Context, postID, communityID uint64, created time.Time) error {
	if postID == 0 || communityID == 0 || created.IsZero() {
		return errors.New("invalid post index entry")
	}
	id := strconv.FormatUint(postID, 10)
	community := strconv.FormatUint(communityID, 10)
	keys := []string{postCommunity, timeIndex, scoreIndex, communityIndex(community, "time"), communityIndex(community, "score")}
	return indexPost.Run(ctx, s.client, keys, id, community, created.Unix(), deletedCommunity).Err()
}

func (s *Store) RemovePost(ctx context.Context, postID, communityID uint64) error {
	id := strconv.FormatUint(postID, 10)
	community := strconv.FormatUint(communityID, 10)
	_, err := s.client.TxPipelined(ctx, func(tx redis.Pipeliner) error {
		tx.HSet(ctx, postCommunity, id, deletedCommunity)
		tx.ZRem(ctx, timeIndex, id)
		tx.ZRem(ctx, scoreIndex, id)
		tx.ZRem(ctx, communityIndex(community, "time"), id)
		tx.ZRem(ctx, communityIndex(community, "score"), id)
		tx.Unlink(ctx, votesKey(id))
		return nil
	})
	return err
}

// All ranking scores are Unix seconds; a change of one vote moves a post by
// 432 seconds. Read the old direction and update both rankings as one operation
// so concurrent requests cannot count the same vote twice.
var applyVote = redis.NewScript(`
local created = redis.call("ZSCORE", KEYS[1], ARGV[1])
if not created then return 1 end
local now = redis.call("TIME")
if tonumber(now[1]) - tonumber(created) > tonumber(ARGV[4]) then return 2 end

local previous = tonumber(redis.call("ZSCORE", KEYS[3], ARGV[2])) or 0
local direction = tonumber(ARGV[3])
if previous == direction then return 3 end

local delta = (direction - previous) * tonumber(ARGV[5])
local score = redis.call("ZINCRBY", KEYS[2], delta, ARGV[1])
redis.call("ZADD", KEYS[4], score, ARGV[1])
if direction == 0 then
  redis.call("ZREM", KEYS[3], ARGV[2])
else
  redis.call("ZADD", KEYS[3], direction, ARGV[2])
end
return 0
`)

// Cast sets this user's direction to -1, 0 (cancel), or 1.
func (s *Store) Cast(ctx context.Context, userID, postID uint64, direction int64) error {
	if userID == 0 || postID == 0 || direction < -1 || direction > 1 {
		return ErrInvalidVote
	}
	post := strconv.FormatUint(postID, 10)
	// The post's community is immutable. Look it up before the script so that
	// every Redis key the script accesses is passed explicitly.
	community, err := s.client.HGet(ctx, postCommunity, post).Result()
	if errors.Is(err, redis.Nil) || community == deletedCommunity {
		return ErrPostNotFound
	}
	if err != nil {
		return err
	}
	keys := []string{timeIndex, scoreIndex, votesKey(post), communityIndex(community, "score")}
	status, err := applyVote.Run(ctx, s.client, keys, post, userID, direction, int64(voteWindow/time.Second), pointsPerVote).Int()
	if err != nil {
		return err
	}
	switch status {
	case 0:
		return nil
	case 1:
		return ErrPostNotFound
	case 2:
		return ErrVoteExpired
	case 3:
		return ErrVoteRepeated
	default:
		return fmt.Errorf("unexpected vote result %d", status)
	}
}

func (s *Store) ListIDs(ctx context.Context, communityID uint64, order string, page, size int64) ([]uint64, error) {
	key := timeIndex
	switch order {
	case "time":
	case "score":
		key = scoreIndex
	default:
		return nil, errors.New("invalid ranking order")
	}
	if page <= 0 || size <= 0 || page > math.MaxInt64/size {
		return nil, errors.New("invalid ranking page")
	}
	if communityID != 0 {
		key = communityIndex(strconv.FormatUint(communityID, 10), order)
	}
	members, err := s.client.ZRevRange(ctx, key, (page-1)*size, page*size-1).Result()
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, len(members))
	for i, member := range members {
		ids[i], err = strconv.ParseUint(member, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid ranked post ID %q: %w", member, err)
		}
	}
	return ids, nil
}

func (s *Store) Upvotes(ctx context.Context, ids []uint64) (map[uint64]int64, error) {
	counts := make(map[uint64]int64, len(ids))
	if len(ids) == 0 {
		return counts, nil
	}
	pipe := s.client.Pipeline()
	commands := make([]*redis.IntCmd, len(ids))
	for i, id := range ids {
		commands[i] = pipe.ZCount(ctx, votesKey(strconv.FormatUint(id, 10)), "1", "1")
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	for i, command := range commands {
		counts[ids[i]] = command.Val()
	}
	return counts, nil
}
