package service

import (
	"net/netip"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	geoip2 "github.com/oschwald/geoip2-golang/v2"
	"go.uber.org/zap"
)

const (
	ModelRegionScopeGlobal = "global"
	ModelRegionScopeCN     = "cn"
)

type geoIPCountryReader interface {
	Country(ip netip.Addr) (*geoip2.Country, error)
	Close() error
}

type ModelRegionPolicy struct {
	enabled  bool
	reader   geoIPCountryReader
	allowed  map[string]struct{}
	initErr  error
	closeMux sync.Mutex
}

func NewModelRegionPolicy(cfg *config.Config) *ModelRegionPolicy {
	policy := &ModelRegionPolicy{}
	if cfg == nil || !cfg.ModelRegionIsolation.Enabled {
		return policy
	}

	policy.enabled = true
	policy.allowed = buildAllowedModelSet(cfg.ModelRegionIsolation.CNAllowedModels)

	path := strings.TrimSpace(cfg.ModelRegionIsolation.GeoIPMMDBPath)
	if path == "" {
		return policy
	}
	reader, err := geoip2.Open(path)
	if err != nil {
		policy.initErr = err
		logger.L().Warn("model_region.geoip_open_failed", zap.String("path", path), zap.Error(err))
		return policy
	}
	policy.reader = reader
	return policy
}

func buildAllowedModelSet(models []string) map[string]struct{} {
	allowed := make(map[string]struct{}, len(models)*2)
	for _, model := range models {
		normalized := NormalizeRegionModelID(model)
		if normalized == "" {
			continue
		}
		allowed[normalized] = struct{}{}
		if strings.HasPrefix(normalized, "models/") {
			allowed[strings.TrimPrefix(normalized, "models/")] = struct{}{}
		} else {
			allowed["models/"+normalized] = struct{}{}
		}
	}
	return allowed
}

func NormalizeRegionModelID(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

func (p *ModelRegionPolicy) Enabled() bool {
	return p != nil && p.enabled
}

func (p *ModelRegionPolicy) ScopeForClientIP(clientIP string) string {
	if p == nil || !p.enabled {
		return ModelRegionScopeGlobal
	}

	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil || !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() {
		return ModelRegionScopeCN
	}
	if p.reader == nil {
		return ModelRegionScopeCN
	}

	record, err := p.reader.Country(addr)
	if err != nil || record == nil || !record.HasData() {
		return ModelRegionScopeCN
	}
	if strings.EqualFold(record.Country.ISOCode, "CN") {
		return ModelRegionScopeCN
	}
	return ModelRegionScopeGlobal
}

func (p *ModelRegionPolicy) IsModelAllowed(scope string, model string) bool {
	if p == nil || !p.enabled || scope != ModelRegionScopeCN {
		return true
	}
	_, ok := p.allowed[NormalizeRegionModelID(model)]
	return ok
}

func (p *ModelRegionPolicy) FilterModels(scope string, models []string) []string {
	if p == nil || !p.enabled || scope != ModelRegionScopeCN {
		return models
	}
	filtered := make([]string, 0, len(models))
	for _, model := range models {
		if p.IsModelAllowed(scope, model) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func (p *ModelRegionPolicy) Close() error {
	if p == nil || p.reader == nil {
		return nil
	}
	p.closeMux.Lock()
	defer p.closeMux.Unlock()
	if p.reader == nil {
		return nil
	}
	err := p.reader.Close()
	p.reader = nil
	return err
}
