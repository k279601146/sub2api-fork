package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/stretchr/testify/require"
)

type billingCacheWorkerStub struct {
	balanceUpdates      int64
	subscriptionUpdates int64
}

func (b *billingCacheWorkerStub) GetUserBalance(ctx context.Context, userID int64) (float64, error) {
	return 0, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetUserBalance(ctx context.Context, userID int64, balance float64) error {
	atomic.AddInt64(&b.balanceUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) DeductUserBalance(ctx context.Context, userID int64, amount float64) error {
	atomic.AddInt64(&b.balanceUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) InvalidateUserBalance(ctx context.Context, userID int64) error {
	return nil
}

func (b *billingCacheWorkerStub) GetSubscriptionCache(ctx context.Context, userID, groupID int64) (*SubscriptionCacheData, error) {
	return nil, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetSubscriptionCache(ctx context.Context, userID, groupID int64, data *SubscriptionCacheData) error {
	atomic.AddInt64(&b.subscriptionUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) UpdateSubscriptionUsage(ctx context.Context, userID, groupID int64, cost float64) error {
	atomic.AddInt64(&b.subscriptionUpdates, 1)
	return nil
}

func (b *billingCacheWorkerStub) InvalidateSubscriptionCache(ctx context.Context, userID, groupID int64) error {
	return nil
}

func (b *billingCacheWorkerStub) GetAPIKeyRateLimit(ctx context.Context, keyID int64) (*APIKeyRateLimitCacheData, error) {
	return nil, errors.New("not implemented")
}

func (b *billingCacheWorkerStub) SetAPIKeyRateLimit(ctx context.Context, keyID int64, data *APIKeyRateLimitCacheData) error {
	return nil
}

func (b *billingCacheWorkerStub) UpdateAPIKeyRateLimitUsage(ctx context.Context, keyID int64, cost float64) error {
	return nil
}

func (b *billingCacheWorkerStub) InvalidateAPIKeyRateLimit(ctx context.Context, keyID int64) error {
	return nil
}

func TestBillingCacheServiceQueueHighLoad(t *testing.T) {
	cache := &billingCacheWorkerStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{})
	t.Cleanup(svc.Stop)

	start := time.Now()
	for i := 0; i < cacheWriteBufferSize*2; i++ {
		svc.QueueDeductBalance(1, 1)
	}
	require.Less(t, time.Since(start), 2*time.Second)

	svc.QueueUpdateSubscriptionUsage(1, 2, 1.5)

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&cache.balanceUpdates) > 0
	}, 2*time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		return atomic.LoadInt64(&cache.subscriptionUpdates) > 0
	}, 2*time.Second, 10*time.Millisecond)
}

func TestBillingCacheServiceEnqueueAfterStopReturnsFalse(t *testing.T) {
	cache := &billingCacheWorkerStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{})
	svc.Stop()

	enqueued := svc.enqueueCacheWrite(cacheWriteTask{
		kind:   cacheWriteDeductBalance,
		userID: 1,
		amount: 1,
	})
	require.False(t, enqueued)
}

type billingUserRepoStub struct {
	UserRepository
	user *User
	err  error
}

func (s *billingUserRepoStub) GetByID(ctx context.Context, id int64) (*User, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.user != nil {
		user := *s.user
		return &user, nil
	}
	return &User{ID: id}, nil
}

type billingDev2UsageReaderStub struct {
	values []float64
	err    error
	calls  int
}

func (s *billingDev2UsageReaderStub) GetUsageUnitsWithFilters(ctx context.Context, filters usagestats.UsageLogFilters) (float64, error) {
	if s.err != nil {
		return 0, s.err
	}
	if s.calls >= len(s.values) {
		return 0, nil
	}
	value := s.values[s.calls]
	s.calls++
	return value, nil
}

type billingSettingRepoStub struct {
	values map[string]string
}

func (s *billingSettingRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *billingSettingRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *billingSettingRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *billingSettingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		out[key] = s.values[key]
	}
	return out, nil
}

func (s *billingSettingRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *billingSettingRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *billingSettingRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

func TestBillingCacheServiceIDEGatewayAllowsZeroBalanceWithFreeUsageWindow(t *testing.T) {
	usageReader := &billingDev2UsageReaderStub{values: []float64{20, 200}}
	svc := NewBillingCacheService(nil, &billingUserRepoStub{user: &User{ID: 10, Balance: 0}}, nil, nil, nil, nil, &config.Config{})
	t.Cleanup(svc.Stop)
	svc.SetDev2UsageGate(usageReader, nil)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 10, Balance: 0},
		&APIKey{Name: IDEGatewayAPIKeyName, RuntimeAuthType: AuthTypeIDEJWT},
		nil,
		nil,
	)

	require.NoError(t, err)
	require.Equal(t, 2, usageReader.calls)
}

func TestBillingCacheServiceIDEGatewayRejectsWhenFreeWindowAndBalanceInsufficient(t *testing.T) {
	usageReader := &billingDev2UsageReaderStub{values: []float64{100, 700}}
	svc := NewBillingCacheService(nil, &billingUserRepoStub{user: &User{ID: 10, Balance: 0}}, nil, nil, nil, nil, &config.Config{})
	t.Cleanup(svc.Stop)
	svc.SetDev2UsageGate(usageReader, nil)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 10, Balance: 0},
		&APIKey{Name: IDEGatewayAPIKeyName, RuntimeAuthType: AuthTypeIDEJWT},
		nil,
		nil,
	)

	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestBillingCacheServiceIDEGatewayAllowsPartialOverageWhenBalanceCoversReserve(t *testing.T) {
	usageReader := &billingDev2UsageReaderStub{values: []float64{98, 698}}
	svc := NewBillingCacheService(nil, &billingUserRepoStub{user: &User{ID: 10, Balance: 6}}, nil, nil, nil, nil, &config.Config{})
	t.Cleanup(svc.Stop)
	svc.SetDev2UsageGate(usageReader, &billingSettingRepoStub{
		values: map[string]string{
			SettingKeyUsageWindowLimitUnits: "100",
			SettingKeyUsageWeeklyLimitUnits: "700",
		},
	})

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 10, Balance: 6},
		&APIKey{Name: IDEGatewayAPIKeyName, RuntimeAuthType: AuthTypeIDEJWT},
		nil,
		nil,
	)

	require.NoError(t, err)
}

func TestBillingCacheServiceRegularAPIKeyStillRejectsZeroBalance(t *testing.T) {
	svc := NewBillingCacheService(nil, &billingUserRepoStub{user: &User{ID: 10, Balance: 0}}, nil, nil, nil, nil, &config.Config{})
	t.Cleanup(svc.Stop)
	svc.SetDev2UsageGate(&billingDev2UsageReaderStub{values: []float64{0, 0}}, nil)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 10, Balance: 0},
		&APIKey{Name: "regular"},
		nil,
		nil,
	)

	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestBillingCacheServiceAPIKeyNamedIDEClientWithoutJWTStillRejectsZeroBalance(t *testing.T) {
	svc := NewBillingCacheService(nil, &billingUserRepoStub{user: &User{ID: 10, Balance: 0}}, nil, nil, nil, nil, &config.Config{})
	t.Cleanup(svc.Stop)
	svc.SetDev2UsageGate(&billingDev2UsageReaderStub{values: []float64{0, 0}}, nil)

	err := svc.CheckBillingEligibility(
		context.Background(),
		&User{ID: 10, Balance: 0},
		&APIKey{Name: IDEGatewayAPIKeyName},
		nil,
		nil,
	)

	require.ErrorIs(t, err, ErrInsufficientBalance)
}
