package jobseeker

import (
	"context"
	"strings"
	"sync"
	"time"

	configv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/config/v3"
	"google.golang.org/protobuf/types/known/structpb"
)

const dictionaryTTL = 10 * time.Minute

// Resolver 将模型给出的名称解析为内部字典 ID（职位名称、地点、经验要求），全部通过 config-go gRPC。
type Resolver interface {
	XpLvlIDs(ctx context.Context, names []string) ([]int64, error)
	Location(ctx context.Context, name string) (*LocationMatch, error)
	JobTitle(ctx context.Context, title string) (*JobTitleMatch, error)
}

// LocationMatch 地点解析结果：命中城市时 LocationID > 0，命中地区时 RegionID > 0。
type LocationMatch struct {
	LocationID int64
	RegionID   int64
	Name       string
}

// JobTitleMatch 标准职位名称解析结果。
type JobTitleMatch struct {
	ID    int64
	Title string
}

type dictEntry struct {
	data      *structpb.Struct
	expiresAt time.Time
}

type configResolver struct {
	config configv3.ConfigServiceClient
	search configv3.ConfigSearchServiceClient

	mu    sync.Mutex
	cache map[string]dictEntry
}

// NewResolver 创建基于 config-go gRPC 的名称解析器（字典按国家缓存 10 分钟）。
func NewResolver(config configv3.ConfigServiceClient, search configv3.ConfigSearchServiceClient) Resolver {
	return &configResolver{config: config, search: search, cache: map[string]dictEntry{}}
}

// dictionary 读取当前国家站点的字典（GetCountryConfig 按国家缓存，含 location_lists 与 xp_lvls）。
func (r *configResolver) dictionary(ctx context.Context) (*structpb.Struct, error) {
	st, ok := siteByID(int64(currentCountryID(ctx)))
	if !ok {
		return &structpb.Struct{}, nil
	}
	key := strings.ToLower(st.Code)
	r.mu.Lock()
	entry, ok := r.cache[key]
	r.mu.Unlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.data, nil
	}
	resp, err := r.config.GetCountryConfig(ctx, &configv3.GetCountryConfigRequest{CountryKey: key, LanguageCode: "en"})
	if err != nil {
		return nil, upstreamError(ctx, "config lookup", err)
	}
	data := resp.GetData()
	if data == nil {
		data = &structpb.Struct{}
	}
	r.mu.Lock()
	r.cache[key] = dictEntry{data: data, expiresAt: time.Now().Add(dictionaryTTL)}
	r.mu.Unlock()
	return data, nil
}

func (r *configResolver) XpLvlIDs(ctx context.Context, names []string) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}
	data, err := r.dictionary(ctx)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, item := range data.GetFields()["xp_lvls"].GetListValue().GetValues() {
		fields := item.GetStructValue().GetFields()
		for _, name := range names {
			if matchName(name, fields["value"].GetStringValue(), fields["key"].GetStringValue()) {
				ids = append(ids, int64(fields["id"].GetNumberValue()))
				break
			}
		}
	}
	return ids, nil
}

func (r *configResolver) Location(ctx context.Context, name string) (*LocationMatch, error) {
	if strings.TrimSpace(name) == "" {
		return nil, nil
	}
	data, err := r.dictionary(ctx)
	if err != nil {
		return nil, err
	}
	regions := data.GetFields()["location_lists"].GetListValue().GetValues()
	for _, region := range regions {
		for _, loc := range region.GetStructValue().GetFields()["locations"].GetListValue().GetValues() {
			f := loc.GetStructValue().GetFields()
			if matchName(name, f["value"].GetStringValue(), f["key"].GetStringValue()) {
				return &LocationMatch{LocationID: int64(f["id"].GetNumberValue()), Name: f["value"].GetStringValue()}, nil
			}
		}
	}
	for _, region := range regions {
		f := region.GetStructValue().GetFields()
		if matchName(name, f["value"].GetStringValue(), f["display_name"].GetStringValue(), f["key"].GetStringValue()) {
			return &LocationMatch{RegionID: int64(f["id"].GetNumberValue()), Name: f["value"].GetStringValue()}, nil
		}
	}
	return nil, nil
}

func (r *configResolver) JobTitle(ctx context.Context, title string) (*JobTitleMatch, error) {
	resp, err := r.search.SearchJobTitleLists(ctx, &configv3.SearchJobTitleListsRequest{Query: title, Page: 1, Size: 5})
	if err != nil {
		return nil, upstreamError(ctx, "job title lookup", err)
	}
	items := resp.GetJobTitles()
	if len(items) == 0 {
		return nil, nil
	}
	best := items[0]
	for _, item := range items {
		if matchName(title, item.GetValue()) {
			best = item
			break
		}
	}
	return &JobTitleMatch{ID: best.GetId(), Title: best.GetValue()}, nil
}

func matchName(want string, candidates ...string) bool {
	want = normalizeName(want)
	if want == "" {
		return false
	}
	for _, c := range candidates {
		if c != "" && normalizeName(c) == want {
			return true
		}
	}
	return false
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("_", " ", "-", " ", "  ", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
