package kv

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type natsStore struct {
	conn   *nats.Conn
	js     jetstream.JetStream
	bucket jetstream.KeyValue
}

// NewNATS opens a dedicated cache bucket on NATS 2.11+ with JetStream enabled.
func NewNATS(ctx context.Context, rawURL, bucketName string) (Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := nats.Connect(rawURL,
		nats.Name("ticketopia-cache"),
		nats.Timeout(operationTimeout),
		nats.MaxReconnects(-1),
		nats.ReconnectBufSize(-1),
	)
	if err != nil {
		return nil, err
	}
	ready := false
	defer func() {
		if !ready {
			conn.Close()
		}
	}()

	js, err := jetstream.New(conn, jetstream.WithDefaultTimeout(operationTimeout))
	if err != nil {
		return nil, err
	}
	bucket, err := js.KeyValue(ctx, bucketName)
	if errors.Is(err, jetstream.ErrBucketNotFound) {
		bucket, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket:         bucketName,
			History:        1,
			LimitMarkerTTL: time.Minute,
		})
		// Another instance may have created the bucket in the meantime.
		if errors.Is(err, jetstream.ErrBucketExists) {
			bucket, err = js.KeyValue(ctx, bucketName)
		}
	}
	if err != nil {
		return nil, err
	}
	status, err := bucket.Status(ctx)
	if err != nil {
		return nil, err
	}
	config := status.Config()
	if config.History != 1 || config.TTL != 0 || config.LimitMarkerTTL <= 0 || config.Mirror != nil || len(config.Sources) != 0 {
		return nil, fmt.Errorf("NATS cache bucket %q must be a standalone bucket with history=1, no bucket TTL, and per-key TTL enabled", bucketName)
	}

	ready = true
	return &natsStore{conn: conn, js: js, bucket: bucket}, nil
}

func (s *natsStore) Get(ctx context.Context, key string) ([]byte, error) {
	entry, err := s.bucket.Get(ctx, natsKey(key))
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return entry.Value(), nil
}

func (s *natsStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	opts := []jetstream.PublishOpt{jetstream.WithExpectStream("KV_" + s.bucket.Bucket())}
	if ttl > 0 {
		opts = append(opts, jetstream.WithMsgTTL(ttl))
	}
	// KV Put does not accept a per-entry TTL. Publish to the bucket's subject
	// directly so each replacement also replaces its server-managed expiry.
	_, err := s.js.Publish(ctx, "$KV."+s.bucket.Bucket()+"."+natsKey(key), value, opts...)
	return err
}

func (s *natsStore) Close() error {
	s.conn.Close()
	return nil
}

func natsKey(key string) string {
	// NATS keys cannot contain characters such as the colon in "events:1".
	return "k_" + base64.RawURLEncoding.EncodeToString([]byte(key))
}
