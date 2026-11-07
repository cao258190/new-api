package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSearchRedemptionsFiltersAndPaginates(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	})

	now := common.GetTimestamp()
	redemptions := []Redemption{
		{Id: 1, Name: "alpha-active", Key: "00000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: 0},
		{Id: 2, Name: "alpha-future", Key: "00000000000000000000000000000002", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now + 3600},
		{Id: 3, Name: "alpha-expired", Key: "00000000000000000000000000000003", Status: common.RedemptionCodeStatusEnabled, ExpiredTime: now - 10},
		{Id: 4, Name: "beta-disabled", Key: "00000000000000000000000000000004", Status: common.RedemptionCodeStatusDisabled, ExpiredTime: 0},
		{Id: 5, Name: "beta-used", Key: "00000000000000000000000000000005", Status: common.RedemptionCodeStatusUsed, ExpiredTime: 0},
	}
	require.NoError(t, DB.Create(&redemptions).Error)

	tests := []struct {
		name      string
		keyword   string
		status    string
		startIdx  int
		num       int
		wantTotal int64
		wantIds   []int
	}{
		{
			name:      "no filters returns all rows",
			num:       10,
			wantTotal: 5,
			wantIds:   []int{5, 4, 3, 2, 1},
		},
		{
			name:      "keyword filters by name prefix",
			keyword:   "alpha",
			num:       10,
			wantTotal: 3,
			wantIds:   []int{3, 2, 1},
		},
		{
			name:      "enabled status excludes expired rows",
			status:    "1",
			num:       10,
			wantTotal: 2,
			wantIds:   []int{2, 1},
		},
		{
			name:      "expired status returns enabled expired rows",
			status:    "expired",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{3},
		},
		{
			name:      "disabled status",
			status:    "2",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{4},
		},
		{
			name:      "used status",
			status:    "3",
			num:       10,
			wantTotal: 1,
			wantIds:   []int{5},
		},
		{
			name:      "pagination keeps unpaged total",
			startIdx:  1,
			num:       2,
			wantTotal: 5,
			wantIds:   []int{4, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, total, err := SearchRedemptions(tt.keyword, tt.status, tt.startIdx, tt.num)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			gotIds := make([]int, 0, len(rows))
			for _, row := range rows {
				gotIds = append(gotIds, row.Id)
			}
			assert.Equal(t, tt.wantIds, gotIds)
		})
	}
}

func setupRedeemFixture(t *testing.T, quota int) (userId int, key string) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
	t.Cleanup(func() {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&Redemption{}).Error)
		DB.Exec("DELETE FROM users")
		DB.Exec("DELETE FROM logs")
	})

	user := &User{Username: "redeem-user", Password: "password", Status: common.UserStatusEnabled, Quota: 0}
	require.NoError(t, DB.Create(user).Error)

	key = "10000000000000000000000000000001"
	redemption := &Redemption{
		Name:        "redeem-test",
		Key:         key,
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       quota,
		CreatedTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(redemption).Error)
	return user.Id, key
}

func TestRedeemCreditsQuotaExactlyOnce(t *testing.T) {
	userId, key := setupRedeemFixture(t, 500)

	quota, err := Redeem(key, userId)
	require.NoError(t, err)
	assert.Equal(t, 500, quota)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "name = ?", "redeem-test").Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, redemption.Status)
	assert.Equal(t, userId, redemption.UsedUserId)

	// Redeeming the same code again must fail and must not credit quota.
	_, err = Redeem(key, userId)
	require.Error(t, err)
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 500, user.Quota)
}

func TestRedeemRejectsWalletOverflow(t *testing.T) {
	userId, key := setupRedeemFixture(t, 11)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", userId).Update("quota", common.MaxWalletQuota-10).Error)

	_, err := Redeem(key, userId)
	require.ErrorIs(t, err, ErrRedeemFailed)

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, common.MaxWalletQuota-10, user.Quota)

	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status)
}

func TestRedemptionQuotaRejectsWalletOverflow(t *testing.T) {
	setupRedeemFixture(t, 500)

	redemption := &Redemption{
		Name:        "overflow-redemption",
		Key:         "10000000000000000000000000000002",
		Status:      common.RedemptionCodeStatusEnabled,
		Quota:       common.MaxWalletQuota + 1,
		CreatedTime: common.GetTimestamp(),
	}
	require.Error(t, redemption.Insert())
}

// Exactly one of several concurrent redeems of the same code may win, and
// quota must be credited exactly once.
func TestRedeemConcurrentSingleSuccess(t *testing.T) {
	userId, key := setupRedeemFixture(t, 300)

	const goroutines = 5
	successes := make([]bool, goroutines)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			if _, err := Redeem(key, userId); err == nil {
				successes[idx] = true
			}
		}(i)
	}
	wg.Wait()

	successCount := 0
	for _, ok := range successes {
		if ok {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount, "exactly one concurrent redeem should succeed")

	var user User
	require.NoError(t, DB.First(&user, "id = ?", userId).Error)
	assert.Equal(t, 300, user.Quota, "quota must be credited exactly once")
}

func setupSubscriptionRedemption(t *testing.T, key string) (*User, *SubscriptionPlan, *Redemption) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&Redemption{}))
	user := &User{Username: "subscription-user-" + key[len(key)-4:], Password: "password", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(user).Error)
	plan := &SubscriptionPlan{Title: "Original plan", DurationUnit: SubscriptionDurationDay, DurationValue: 30, Enabled: true, TotalAmount: 1000}
	require.NoError(t, DB.Create(plan).Error)
	redemption := &Redemption{Name: "subscription-code", Key: key, Type: RedemptionTypeSubscription, PlanId: plan.Id, Status: common.RedemptionCodeStatusEnabled}
	require.NoError(t, redemption.Insert())
	t.Cleanup(func() {
		require.NoError(t, DB.Where("user_id = ?", user.Id).Unscoped().Delete(&UserSubscription{}).Error)
		require.NoError(t, DB.Unscoped().Delete(redemption).Error)
		require.NoError(t, DB.Unscoped().Delete(plan).Error)
		require.NoError(t, DB.Unscoped().Delete(user).Error)
	})
	return user, plan, redemption
}

func TestRedeemSubscriptionCodeCreatesSubscription(t *testing.T) {
	user, plan, redemption := setupSubscriptionRedemption(t, "20000000000000000000000000000001")
	assert.Zero(t, redemption.Quota, "subscription codes must not carry the legacy quota default")
	require.NoError(t, DB.Model(plan).Updates(map[string]any{"title": "Updated plan", "total_amount": 2500, "upgrade_group": "vip"}).Error)
	var stored Redemption
	require.NoError(t, DB.First(&stored, redemption.Id).Error)
	assert.Zero(t, stored.Quota, "subscription codes must not inherit the legacy quota default")

	result, err := RedeemCode(redemption.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, RedemptionTypeSubscription, result.Type)
	assert.Equal(t, plan.Id, result.PlanId)
	assert.Equal(t, "Updated plan", result.PlanTitle)

	var subscription UserSubscription
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&subscription).Error)
	assert.Equal(t, plan.Id, subscription.PlanId)
	assert.Equal(t, "redemption", subscription.Source)
	assert.Equal(t, int64(2500), subscription.AmountTotal)
	assert.Equal(t, "vip", subscription.UpgradeGroup)
	assert.Equal(t, int64(30*24*60*60), subscription.EndTime-subscription.StartTime)
	require.NoError(t, DB.First(&stored, redemption.Id).Error)
	assert.Equal(t, common.RedemptionCodeStatusUsed, stored.Status)
	assert.Equal(t, user.Id, stored.UsedUserId)
	var updatedUser User
	require.NoError(t, DB.First(&updatedUser, user.Id).Error)
	assert.Equal(t, "vip", updatedUser.Group)
	assert.Zero(t, updatedUser.Quota, "subscription codes must not credit wallet quota")
}

func TestRedeemSubscriptionCodeRejectsUnavailableEntitlements(t *testing.T) {
	tests := []struct {
		name          string
		key           string
		status        int
		expired       bool
		disablePlan   bool
		purchaseLimit bool
	}{
		{name: "disabled code", key: "20000000000000000000000000000002", status: common.RedemptionCodeStatusDisabled},
		{name: "expired code", key: "20000000000000000000000000000003", expired: true},
		{name: "disabled plan", key: "20000000000000000000000000000004", disablePlan: true},
		{name: "purchase limit rolls back code use", key: "20000000000000000000000000000005", purchaseLimit: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			user, plan, redemption := setupSubscriptionRedemption(t, tc.key)
			if tc.status != 0 {
				require.NoError(t, DB.Model(redemption).Update("status", tc.status).Error)
			}
			if tc.expired {
				require.NoError(t, DB.Model(redemption).Update("expired_time", common.GetTimestamp()-1).Error)
			}
			if tc.disablePlan {
				require.NoError(t, DB.Model(plan).Update("enabled", false).Error)
			}
			if tc.purchaseLimit {
				require.NoError(t, DB.Model(plan).Update("max_purchase_per_user", 1).Error)
				require.NoError(t, DB.Create(&UserSubscription{UserId: user.Id, PlanId: plan.Id, Status: "active"}).Error)
			}

			_, err := RedeemCode(redemption.Key, user.Id)
			require.ErrorIs(t, err, ErrRedeemFailed)
			var stored Redemption
			require.NoError(t, DB.First(&stored, redemption.Id).Error)
			expectedStatus := common.RedemptionCodeStatusEnabled
			if tc.status != 0 {
				expectedStatus = tc.status
			}
			assert.Equal(t, expectedStatus, stored.Status)
			assert.Zero(t, stored.UsedUserId)
			var count int64
			require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
			if tc.purchaseLimit {
				assert.Equal(t, int64(1), count)
			} else {
				assert.Zero(t, count)
			}
		})
	}
}

func TestRedeemSubscriptionCodeConcurrentSingleSuccess(t *testing.T) {
	user, _, redemption := setupSubscriptionRedemption(t, "20000000000000000000000000000006")
	var wg sync.WaitGroup
	successes := make([]bool, 2)
	for i := range successes {
		wg.Go(func() {
			_, err := RedeemCode(redemption.Key, user.Id)
			successes[i] = err == nil
		})
	}
	wg.Wait()
	successCount := 0
	for _, success := range successes {
		if success {
			successCount++
		}
	}
	assert.Equal(t, 1, successCount)
	var count int64
	require.NoError(t, DB.Model(&UserSubscription{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
