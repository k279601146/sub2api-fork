package service

import (
	"errors"
	"net/netip"
	"testing"

	geoip2 "github.com/oschwald/geoip2-golang/v2"
	"github.com/stretchr/testify/require"
)

type geoIPCountryReaderStub struct {
	country string
	err     error
	noData  bool
}

func (s geoIPCountryReaderStub) Country(netip.Addr) (*geoip2.Country, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.noData {
		return &geoip2.Country{}, nil
	}
	return &geoip2.Country{Country: geoip2.CountryRecord{ISOCode: s.country}}, nil
}

func (s geoIPCountryReaderStub) Close() error {
	return nil
}

func TestModelRegionPolicyScopeForClientIP(t *testing.T) {
	t.Run("disabled returns global", func(t *testing.T) {
		policy := &ModelRegionPolicy{}
		require.Equal(t, ModelRegionScopeGlobal, policy.ScopeForClientIP("203.0.113.10"))
	})

	tests := []struct {
		name   string
		reader geoIPCountryReader
		ip     string
		want   string
	}{
		{name: "invalid ip", ip: "not-an-ip", want: ModelRegionScopeCN},
		{name: "private ip", ip: "10.0.0.1", want: ModelRegionScopeCN},
		{name: "loopback ip", ip: "127.0.0.1", want: ModelRegionScopeCN},
		{name: "local development", ip: "::1", want: ModelRegionScopeCN},
		{name: "mmdb disabled", ip: "8.8.8.8", want: ModelRegionScopeCN},
		{name: "lookup error", reader: geoIPCountryReaderStub{err: errors.New("lookup failed")}, ip: "8.8.8.8", want: ModelRegionScopeCN},
		{name: "no country data", reader: geoIPCountryReaderStub{noData: true}, ip: "8.8.8.8", want: ModelRegionScopeCN},
		{name: "cn country", reader: geoIPCountryReaderStub{country: "CN"}, ip: "1.1.1.1", want: ModelRegionScopeCN},
		{name: "overseas country", reader: geoIPCountryReaderStub{country: "US"}, ip: "8.8.8.8", want: ModelRegionScopeGlobal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &ModelRegionPolicy{enabled: true, reader: tt.reader}
			require.Equal(t, tt.want, policy.ScopeForClientIP(tt.ip))
		})
	}
}

func TestModelRegionPolicyModelAllowlist(t *testing.T) {
	policy := &ModelRegionPolicy{
		enabled: true,
		allowed: buildAllowedModelSet([]string{
			"gpt-5.4",
			"models/gemini-2.5-flash",
		}),
	}

	require.True(t, policy.IsModelAllowed(ModelRegionScopeGlobal, "not-listed"))
	require.True(t, policy.IsModelAllowed(ModelRegionScopeCN, "gpt-5.4"))
	require.True(t, policy.IsModelAllowed(ModelRegionScopeCN, "models/gemini-2.5-flash"))
	require.True(t, policy.IsModelAllowed(ModelRegionScopeCN, "gemini-2.5-flash"))
	require.False(t, policy.IsModelAllowed(ModelRegionScopeCN, "gpt-5.5"))

	filtered := policy.FilterModels(ModelRegionScopeCN, []string{
		"gpt-5.4",
		"gpt-5.5",
		"models/gemini-2.5-flash",
	})
	require.Equal(t, []string{"gpt-5.4", "models/gemini-2.5-flash"}, filtered)
}
