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
	enabled         bool
	reader          geoIPCountryReader
	allowed         map[string]struct{}
	allowedPatterns []string
	initErr         error
	closeMux        sync.Mutex
}

func NewModelRegionPolicy(cfg *config.Config) *ModelRegionPolicy {
	policy := &ModelRegionPolicy{}
	if cfg == nil || !cfg.ModelRegionIsolation.Enabled {
		return policy
	}

	policy.enabled = true
	policy.allowed = buildAllowedModelSet(cfg.ModelRegionIsolation.CNAllowedModels)
	policy.allowedPatterns = buildAllowedModelPatterns(cfg.ModelRegionIsolation.CNAllowedModels)

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

func buildAllowedModelPatterns(models []string) []string {
	seen := make(map[string]struct{}, len(models)*2)
	patterns := make([]string, 0, len(models)*2)
	for _, model := range models {
		normalized := NormalizeRegionModelID(model)
		if normalized == "" {
			continue
		}
		candidates := []string{normalized}
		if strings.HasPrefix(normalized, "models/") {
			candidates = append(candidates, strings.TrimPrefix(normalized, "models/"))
		}
		for _, candidate := range candidates {
			if candidate == "" {
				continue
			}
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			patterns = append(patterns, candidate)
		}
	}
	return patterns
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
	normalized := NormalizeRegionModelID(model)
	if normalized == "" {
		return false
	}
	if _, ok := p.allowed[normalized]; ok {
		return true
	}
	bareModel := strings.TrimPrefix(normalized, "models/")
	for _, pattern := range p.allowedPatterns {
		if strings.Contains(normalized, pattern) || strings.Contains(bareModel, pattern) {
			return true
		}
	}
	return false
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
