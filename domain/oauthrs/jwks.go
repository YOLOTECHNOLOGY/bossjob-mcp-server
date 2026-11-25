package oauthrs

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// ErrUnknownKey 表示 JWKS 中不存在 Token 使用的 kid。
var ErrUnknownKey = errors.New("unknown signing key")

// KeySource 按 kid 提供 RS256 验签公钥。
type KeySource interface {
	Key(ctx context.Context, kid string) (*rsa.PublicKey, error)
}

// JWKSKeySource 从 auth-go JWKS 拉取并缓存公钥；未知 kid 触发刷新，刷新受最短间隔限制。
type JWKSKeySource struct {
	url      string
	interval time.Duration
	client   *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	lastFetch time.Time
}

// NewJWKSKeySource 创建 JWKS 公钥源。
func NewJWKSKeySource(url string, interval time.Duration, client *http.Client) *JWKSKeySource {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &JWKSKeySource{url: url, interval: interval, client: client, keys: map[string]*rsa.PublicKey{}}
}

// Key 实现 KeySource。
func (s *JWKSKeySource) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if key, ok := s.keys[kid]; ok {
		return key, nil
	}
	if !s.lastFetch.IsZero() && time.Since(s.lastFetch) < s.interval {
		return nil, ErrUnknownKey
	}
	keys, err := s.fetch(ctx)
	s.lastFetch = time.Now()
	if err != nil {
		return nil, err
	}
	s.keys = keys
	if key, ok := s.keys[kid]; ok {
		return key, nil
	}
	return nil, ErrUnknownKey
}

type jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (s *JWKSKeySource) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch jwks: status %d", resp.StatusCode)
	}
	var doc struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	return ParseJWKS(doc.Keys)
}

// ParseJWKS 解析 RS256 签名公钥；忽略非 RSA / 非签名用途的条目。
func ParseJWKS(keys []jwk) (map[string]*rsa.PublicKey, error) {
	out := make(map[string]*rsa.PublicKey, len(keys))
	for _, k := range keys {
		if k.Kty != "RSA" || k.Kid == "" || (k.Use != "" && k.Use != "sig") || (k.Alg != "" && k.Alg != "RS256") {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("jwks kid %s: invalid n", k.Kid)
		}
		e, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil || len(e) == 0 || len(e) > 4 {
			return nil, fmt.Errorf("jwks kid %s: invalid e", k.Kid)
		}
		key := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		if key.N.BitLen() < 2048 {
			return nil, fmt.Errorf("jwks kid %s: key too small", k.Kid)
		}
		out[k.Kid] = key
	}
	return out, nil
}
