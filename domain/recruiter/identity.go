package recruiter

import (
	"context"
	"sync"
	"time"

	recruiterv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/recruiter/v3"
	"github.com/YOLOTECHNOLOGY/bossjob-lib-go/log"
)

// DefaultIdentityTTL 招聘者身份上下文进程内缓存时长（mcp-server 无 Redis）。
const DefaultIdentityTTL = 5 * time.Minute

// Identity 招聘者身份上下文：recruiter_sid 与所属公司国家站点（不在 Token 中）。
type Identity struct {
	SID              uint64
	CompanyID        int64
	CompanyCountryID int32
}

// IdentityResolver 按 uid 解析招聘者身份上下文。
type IdentityResolver interface {
	Resolve(ctx context.Context, userID int64) (*Identity, error)
}

type identityEntry struct {
	identity  *Identity
	expiresAt time.Time
}

type cachedIdentityResolver struct {
	internal recruiterv3.RecruiterInternalServiceClient
	ttl      time.Duration
	now      func() time.Time

	mu    sync.Mutex
	cache map[int64]identityEntry
}

// NewIdentityResolver 基于 recruiter-go GetRecruiterContextInternal 的身份解析器，结果按 uid 缓存 Options.IdentityTTL。
func NewIdentityResolver(internal recruiterv3.RecruiterInternalServiceClient, opts Options) IdentityResolver {
	ttl := opts.IdentityTTL
	if ttl <= 0 {
		ttl = DefaultIdentityTTL
	}
	return &cachedIdentityResolver{internal: internal, ttl: ttl, now: time.Now, cache: map[int64]identityEntry{}}
}

func (r *cachedIdentityResolver) Resolve(ctx context.Context, userID int64) (*Identity, error) {
	now := r.now()
	r.mu.Lock()
	entry, ok := r.cache[userID]
	r.mu.Unlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.identity, nil
	}
	resp, err := r.internal.GetRecruiterContextInternal(ctx, &recruiterv3.GetRecruiterContextInternalRequest{UserId: userID})
	if err != nil {
		return nil, err
	}
	id := &Identity{CompanyID: resp.GetCompanyId(), CompanyCountryID: int32(resp.GetCompanyCountryId())}
	if resp.GetRecruiterSid() > 0 {
		id.SID = uint64(resp.GetRecruiterSid())
	}
	r.mu.Lock()
	r.cache[userID] = identityEntry{identity: id, expiresAt: now.Add(r.ttl)}
	// 防止长时间运行后缓存无限增长：顺带清理过期项。
	if len(r.cache) > 10000 {
		for k, v := range r.cache {
			if !now.Before(v.expiresAt) {
				delete(r.cache, k)
			}
		}
	}
	r.mu.Unlock()
	log.DebugfCtx(ctx, "recruiter identity resolved: uid=%d company_id=%d country_id=%d", userID, id.CompanyID, id.CompanyCountryID)
	return id, nil
}
