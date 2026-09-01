package esmcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	core "github.com/takutakahashi/agentapi-proxy/internal/core/esmcontrol"
)

const (
	maxLen = int64(10000)
	// A direct-runtime request may legitimately run without producing frames
	// for longer than the manager heartbeat window. Keep its command, response,
	// and ownership records long enough to survive Cloud Run SSE reconnects and
	// long-running tools. The bounded streams and TTL still provide cleanup.
	streamTTL     = 24 * time.Hour
	connectionTTL = 75 * time.Second
)

type RedisStore struct{ client *redis.Client }

func NewRedisStore(client *redis.Client) *RedisStore { return &RedisStore{client: client} }

func commandKey(managerID string) string {
	return "agentapi:esm:{" + managerID + "}:commands"
}
func connectionKey(managerID string) string {
	return "agentapi:esm:{" + managerID + "}:control-connection"
}
func commandAckKey(managerID string) string {
	return "agentapi:esm:{" + managerID + "}:command-ack"
}
func responseKey(requestID string) string {
	return "agentapi:esm-request:{" + requestID + "}:responses"
}
func requestOwnerKey(requestID string) string {
	return "agentapi:esm-request:{" + requestID + "}:manager"
}
func frameDedupKey(requestID, frameID string) string {
	return "agentapi:esm-request:{" + requestID + "}:frame:" + frameID
}

var appendFrameScript = redis.NewScript(`
local last = ''
for i = 2, #KEYS do
  local payload = ARGV[i - 1]
  if redis.call('SET', KEYS[i], '1', 'NX', 'EX', ARGV[#ARGV]) then
    last = redis.call('XADD', KEYS[1], 'MAXLEN', '~', ARGV[#ARGV - 1], '*', 'frame', payload)
  end
end
redis.call('EXPIRE', KEYS[1], ARGV[#ARGV])
return last
`)

var refreshRequestOwnerScript = redis.NewScript(`
local owner = redis.call('GET', KEYS[1])
if owner == ARGV[1] then
  redis.call('EXPIRE', KEYS[1], ARGV[2])
  return 1
end
return 0
`)

func (s *RedisStore) TouchManager(ctx context.Context, managerID, instanceID string) error {
	if managerID == "" {
		return fmt.Errorf("manager id is required")
	}
	if err := s.client.Set(ctx, connectionKey(managerID), instanceID, connectionTTL).Err(); err != nil {
		return fmt.Errorf("touch ESM connection: %w", err)
	}
	return nil
}

func (s *RedisStore) IsManagerConnected(ctx context.Context, managerID string) (bool, error) {
	n, err := s.client.Exists(ctx, connectionKey(managerID)).Result()
	if err != nil {
		return false, fmt.Errorf("check ESM connection: %w", err)
	}
	return n > 0, nil
}

func (s *RedisStore) EnqueueCommand(ctx context.Context, managerID string, command core.Command) (string, error) {
	payload, err := json.Marshal(command)
	if err != nil {
		return "", err
	}
	key := commandKey(managerID)
	var add *redis.StringCmd
	_, err = s.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		add = pipe.XAdd(ctx, &redis.XAddArgs{Stream: key, MaxLen: maxLen, Approx: true, Values: map[string]interface{}{"command": payload}})
		pipe.Expire(ctx, key, streamTTL)
		pipe.Set(ctx, requestOwnerKey(command.ID), managerID, streamTTL)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("append ESM command: %w", err)
	}
	return add.Val(), nil
}

func (s *RedisStore) ReadCommands(ctx context.Context, managerID, after string, wait time.Duration, count int64) ([]core.Command, error) {
	ack, err := s.client.Get(ctx, commandAckKey(managerID)).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("read ESM command ack: %w", err)
	}
	if streamIDLess(after, ack) {
		after = ack
	}
	msgs, err := s.read(ctx, commandKey(managerID), after, wait, count)
	if err != nil {
		return nil, err
	}
	result := make([]core.Command, 0, len(msgs))
	for _, msg := range msgs {
		var command core.Command
		if err := decode(msg, "command", &command); err != nil {
			return nil, err
		}
		command.StreamID = msg.ID
		result = append(result, command)
	}
	return result, nil
}

func (s *RedisStore) AckCommand(ctx context.Context, managerID, streamID string) error {
	if streamID == "" {
		return nil
	}
	key := commandAckKey(managerID)
	current, err := s.client.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	if !streamIDLess(current, streamID) {
		return nil
	}
	return s.client.Set(ctx, key, streamID, streamTTL).Err()
}

func (s *RedisStore) AppendFrames(ctx context.Context, requestID string, frames []core.ResponseFrame) (string, error) {
	key := responseKey(requestID)
	keys := make([]string, 1, len(frames)+1)
	keys[0] = key
	args := make([]interface{}, 0, len(frames)+2)
	for _, frame := range frames {
		payload, err := json.Marshal(frame)
		if err != nil {
			return "", err
		}
		keys = append(keys, frameDedupKey(requestID, frame.ID))
		args = append(args, payload)
	}
	args = append(args, maxLen, int64(streamTTL/time.Second))
	last, err := appendFrameScript.Run(ctx, s.client, keys, args...).Text()
	if err != nil {
		return "", fmt.Errorf("append ESM response frames: %w", err)
	}
	return last, nil
}

func (s *RedisStore) ReadFrames(ctx context.Context, requestID, after string, wait time.Duration, count int64) ([]core.ResponseFrame, error) {
	msgs, err := s.read(ctx, responseKey(requestID), after, wait, count)
	if err != nil {
		return nil, err
	}
	result := make([]core.ResponseFrame, 0, len(msgs))
	for _, msg := range msgs {
		var frame core.ResponseFrame
		if err := decode(msg, "frame", &frame); err != nil {
			return nil, err
		}
		frame.StreamID = msg.ID
		result = append(result, frame)
	}
	return result, nil
}

func (s *RedisStore) RequestBelongsToManager(ctx context.Context, requestID, managerID string) (bool, error) {
	key := requestOwnerKey(requestID)
	result, err := refreshRequestOwnerScript.Run(
		ctx, s.client, []string{key}, managerID, int64(streamTTL/time.Second),
	).Int()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

func (s *RedisStore) read(ctx context.Context, key, after string, wait time.Duration, count int64) ([]redis.XMessage, error) {
	if after == "" {
		after = "0-0"
	}
	if count <= 0 || count > 100 {
		count = 100
	}
	streams, err := s.client.XRead(ctx, &redis.XReadArgs{Streams: []string{key, after}, Count: count, Block: wait}).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read ESM control stream: %w", err)
	}
	if len(streams) == 0 {
		return nil, nil
	}
	return streams[0].Messages, nil
}

func decode(msg redis.XMessage, field string, target interface{}) error {
	raw, ok := msg.Values[field]
	if !ok {
		return fmt.Errorf("ESM stream entry %s has no %s", msg.ID, field)
	}
	var data []byte
	switch value := raw.(type) {
	case string:
		data = []byte(value)
	case []byte:
		data = value
	default:
		return fmt.Errorf("ESM stream entry %s has invalid %s", msg.ID, field)
	}
	return json.Unmarshal(data, target)
}

func streamIDLess(left, right string) bool {
	if right == "" {
		return false
	}
	if left == "" || left == "0-0" {
		return true
	}
	var leftMS, leftSeq, rightMS, rightSeq int64
	if _, err := fmt.Sscanf(left, "%d-%d", &leftMS, &leftSeq); err != nil {
		return true
	}
	if _, err := fmt.Sscanf(right, "%d-%d", &rightMS, &rightSeq); err != nil {
		return false
	}
	return leftMS < rightMS || (leftMS == rightMS && leftSeq < rightSeq)
}
