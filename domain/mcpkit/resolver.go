package mcpkit

import (
	"context"
	"strings"
	"sync"
	"time"

	configv3 "github.com/YOLOTECHNOLOGY/bossjob-api-public/go/config/v3"
	"google.golang.org/protobuf/types/known/structpb"
)

const dictionaryTTL = 10 * time.Minute

// Resolver 将模型给出的名称解析为内部字典 ID（职位名称、地点、经验、学历、到岗时间、语言），全部通过 config-go gRPC。
// 无法识别的名称被忽略（由调用方决定是否报错）。
type Resolver interface {
	XpLvlIDs(ctx context.Context, names []string) ([]int64, error)
	DegreeIDs(ctx context.Context, names []string) ([]int64, error)
	NoticePeriodIDs(ctx context.Context, names []string) ([]int64, error)
	LanguageIDs(ctx context.Context, names []string) ([]int64, error)
	Location(ctx context.Context, name string) (*LocationMatch, error)
	// Locations 批量解析地点：城市进入 locationIDs，地区进入 regionIDs。
	Locations(ctx context.Context, names []string) (locationIDs, regionIDs []int64, err error)
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
	st, ok := SiteByID(int64(CurrentCountryID(ctx)))
	if !ok {
		return &structpb.Struct{}, nil
	}
	key := st.Key
	r.mu.Lock()
	entry, ok := r.cache[key]
	r.mu.Unlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.data, nil
	}
	resp, err := r.config.GetCountryConfig(ctx, &configv3.GetCountryConfigRequest{CountryKey: key, LanguageCode: "en"})
	if err != nil {
		return nil, UpstreamError(ctx, "config lookup", err)
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
	return r.listIDs(ctx, "xp_lvls", names, "value", "key")
}

func (r *configResolver) DegreeIDs(ctx context.Context, names []string) ([]int64, error) {
	return r.listIDs(ctx, "degrees", names, "value", "key")
}

func (r *configResolver) NoticePeriodIDs(ctx context.Context, names []string) ([]int64, error) {
	return r.listIDs(ctx, "notice_period_lists", names, "value", "key")
}

// LanguageIDs 解析 system_languages（language_lists 的 value 为语言名，code 为 ISO 语言代码）。
func (r *configResolver) LanguageIDs(ctx context.Context, names []string) ([]int64, error) {
	return r.listIDs(ctx, "language_lists", names, "value", "code", "key")
}

// listIDs 在字典 key 对应的扁平列表中按名称匹配 id。
func (r *configResolver) listIDs(ctx context.Context, key string, names []string, fields ...string) ([]int64, error) {
	if len(names) == 0 {
		return nil, nil
	}
	data, err := r.dictionary(ctx)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, item := range data.GetFields()[key].GetListValue().GetValues() {
		f := item.GetStructValue().GetFields()
		candidates := make([]string, 0, len(fields))
		for _, name := range fields {
			candidates = append(candidates, f[name].GetStringValue())
		}
		for _, name := range names {
			if MatchName(name, candidates...) {
				ids = append(ids, int64(f["id"].GetNumberValue()))
				break
			}
		}
	}
	return ids, nil
}

func (r *configResolver) Locations(ctx context.Context, names []string) ([]int64, []int64, error) {
	var locationIDs, regionIDs []int64
	for _, name := range names {
		m, err := r.Location(ctx, name)
		if err != nil {
			return nil, nil, err
		}
		switch {
		case m == nil:
		case m.LocationID > 0:
			locationIDs = append(locationIDs, m.LocationID)
		case m.RegionID > 0:
			regionIDs = append(regionIDs, m.RegionID)
		}
	}
	return locationIDs, regionIDs, nil
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
			if MatchName(name, f["value"].GetStringValue(), f["key"].GetStringValue()) {
				return &LocationMatch{LocationID: int64(f["id"].GetNumberValue()), Name: f["value"].GetStringValue()}, nil
			}
		}
	}
	for _, region := range regions {
		f := region.GetStructValue().GetFields()
		if MatchName(name, f["value"].GetStringValue(), f["display_name"].GetStringValue(), f["key"].GetStringValue()) {
			return &LocationMatch{RegionID: int64(f["id"].GetNumberValue()), Name: f["value"].GetStringValue()}, nil
		}
	}
	return nil, nil
}

func (r *configResolver) JobTitle(ctx context.Context, title string) (*JobTitleMatch, error) {
	resp, err := r.search.SearchJobTitleLists(ctx, &configv3.SearchJobTitleListsRequest{Query: title, Page: 1, Size: 5})
	if err != nil {
		return nil, UpstreamError(ctx, "job title lookup", err)
	}
	items := resp.GetJobTitles()
	if len(items) == 0 {
		return nil, nil
	}
	best := items[0]
	for _, item := range items {
		if MatchName(title, item.GetValue()) {
			best = item
			break
		}
	}
	return &JobTitleMatch{ID: best.GetId(), Title: best.GetValue()}, nil
}

func MatchName(want string, candidates ...string) bool {
	want = NormalizeName(want)
	if want == "" {
		return false
	}
	for _, c := range candidates {
		if c != "" && NormalizeName(c) == want {
			return true
		}
	}
	return false
}

func NormalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("_", " ", "-", " ", "  ", " ").Replace(s)
	return strings.Join(strings.Fields(s), " ")
}
