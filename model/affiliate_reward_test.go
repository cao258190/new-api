package model

import (
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func configureAffiliateRewardTest(t *testing.T, rate int, confirmed bool) {
	t.Helper()
	previousQuotaPerUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 100
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousRate, hadRate := common.OptionMap[operation_setting.AffiliateRewardRateOptionKey]
	common.OptionMap[operation_setting.AffiliateRewardRateOptionKey] = strconv.Itoa(rate)
	common.OptionMapRWMutex.Unlock()
	payment := operation_setting.GetPaymentSetting()
	previousConfirmed, previousVersion := payment.ComplianceConfirmed, payment.ComplianceTermsVersion
	payment.ComplianceConfirmed = confirmed
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		common.QuotaPerUnit = previousQuotaPerUnit
		common.OptionMapRWMutex.Lock()
		if hadRate {
			common.OptionMap[operation_setting.AffiliateRewardRateOptionKey] = previousRate
		} else {
			delete(common.OptionMap, operation_setting.AffiliateRewardRateOptionKey)
		}
		common.OptionMapRWMutex.Unlock()
		payment.ComplianceConfirmed, payment.ComplianceTermsVersion = previousConfirmed, previousVersion
	})
}

func TestAffiliateRewardRateValidation(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int
		valid bool
	}{
		{value: "0", want: 0, valid: true},
		{value: "1250", want: 1250, valid: true},
		{value: "10000", want: 10000, valid: true},
		{value: "-1"},
		{value: "10001"},
		{value: "12.5"},
		{value: "invalid"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			rate, err := operation_setting.ParseAffiliateRewardRate(tc.value)
			if tc.valid {
				require.NoError(t, err)
				assert.Equal(t, tc.want, rate)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func createAffiliateUsersForTest(t *testing.T, suffix string, inviterQuota int, inviterRate *int) (*User, *User) {
	t.Helper()
	inviter := &User{
		Username: "affiliate-inviter-" + suffix, AffCode: "aff-inviter-" + suffix,
		Status: common.UserStatusEnabled, Quota: inviterQuota, AffQuota: 20, AffHistoryQuota: 50,
		AffRewardRate: inviterRate,
	}
	require.NoError(t, DB.Create(inviter).Error)
	invitee := &User{
		Username: "affiliate-invitee-" + suffix, AffCode: "aff-invitee-" + suffix,
		Status: common.UserStatusEnabled, InviterId: inviter.Id,
	}
	require.NoError(t, DB.Create(invitee).Error)
	return inviter, invitee
}

func createAffiliateTopUpForTest(t *testing.T, inviteeID int, suffix string, provider string, amount int64, money float64) *TopUp {
	t.Helper()
	topUp := &TopUp{
		UserId: inviteeID, Amount: amount, Money: money,
		TradeNo: "AFF-" + suffix, PaymentMethod: "alipay", PaymentProvider: provider,
		Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp(),
	}
	require.NoError(t, DB.Create(topUp).Error)
	return topUp
}

func TestAffiliateRewardRatePrecedenceAndWalletSplit(t *testing.T) {
	cases := []struct {
		name          string
		globalRate    int
		overrideRate  *int
		confirmed     bool
		withoutInvite bool
		inviterQuota  int
		wantRate      int
		wantReward    int
		wantCredited  int
	}{
		{name: "default closed", globalRate: 0, confirmed: true},
		{name: "global rate floors fractional quota", globalRate: 1250, confirmed: true, wantRate: 1250, wantReward: 37, wantCredited: 37},
		{name: "rebate repays an overdrawn wallet", globalRate: 1250, confirmed: true, inviterQuota: -50, wantRate: 1250, wantReward: 37, wantCredited: 37},
		{name: "personal rate overrides default", globalRate: 1250, overrideRate: common.GetPointer(2500), confirmed: true, wantRate: 2500, wantReward: 75, wantCredited: 75},
		{name: "personal zero disables reward", globalRate: 1250, overrideRate: common.GetPointer(0), confirmed: true},
		{name: "personal rate can enable global zero", globalRate: 0, overrideRate: common.GetPointer(10000), confirmed: true, wantRate: 10000, wantReward: 300, wantCredited: 300},
		{name: "invalid personal rate is disabled", globalRate: 1250, overrideRate: common.GetPointer(10001), confirmed: true},
		{name: "compliance gate disables existing rate", globalRate: 1250, confirmed: false},
		{name: "no inviter", globalRate: 1250, confirmed: true, withoutInvite: true},
		{name: "wallet ceiling holds remainder", globalRate: 1250, confirmed: true, inviterQuota: common.MaxWalletQuota - 5, wantRate: 1250, wantReward: 37, wantCredited: 5},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			configureAffiliateRewardTest(t, tc.globalRate, tc.confirmed)
			suffix := strconv.Itoa(i)
			inviter, invitee := createAffiliateUsersForTest(t, suffix, tc.inviterQuota, tc.overrideRate)
			if tc.withoutInvite {
				require.NoError(t, DB.Model(invitee).Update("inviter_id", 0).Error)
			}
			order := createAffiliateTopUpForTest(t, invitee.Id, suffix, PaymentProviderEpay, 3, 3)

			alreadyDone, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
			require.NoError(t, err)
			assert.False(t, alreadyDone)
			alreadyDone, err = RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
			require.NoError(t, err)
			assert.True(t, alreadyDone)

			var creditedInvitee, creditedInviter User
			require.NoError(t, DB.First(&creditedInvitee, invitee.Id).Error)
			require.NoError(t, DB.First(&creditedInviter, inviter.Id).Error)
			assert.Equal(t, 300, creditedInvitee.Quota)
			assert.Equal(t, tc.inviterQuota+tc.wantCredited, creditedInviter.Quota)
			assert.Equal(t, 20+tc.wantReward-tc.wantCredited, creditedInviter.AffQuota)
			assert.Equal(t, 50+tc.wantReward, creditedInviter.AffHistoryQuota)

			var rewards []AffiliateReward
			require.NoError(t, DB.Where("inviter_id = ?", inviter.Id).Find(&rewards).Error)
			if tc.wantReward == 0 {
				assert.Empty(t, rewards)
				return
			}
			require.Len(t, rewards, 1)
			assert.Equal(t, tc.wantRate, rewards[0].RateBps)
			assert.Equal(t, 300, rewards[0].BaseQuota)
			assert.Equal(t, tc.wantReward, rewards[0].RewardQuota)
			assert.Equal(t, tc.wantCredited, rewards[0].CreditedQuota)
			assert.Equal(t, tc.wantReward-tc.wantCredited, rewards[0].PendingQuota)
			assert.Equal(t, AffiliateRewardSourceTopUp, rewards[0].SourceType)
			assert.Equal(t, order.Id, rewards[0].SourceId)
		})
	}
}

func TestAffiliateRewardSettlesEveryWalletPaymentOnce(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		amount   int64
		settle   func(*TopUp) error
	}{
		{"epay", PaymentProviderEpay, 4, func(order *TopUp) error { _, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1"); return err }},
		{"stripe", PaymentProviderStripe, 4, func(order *TopUp) error { return Recharge(order.TradeNo, "customer", "127.0.0.1") }},
		{"creem", PaymentProviderCreem, 400, func(order *TopUp) error { return RechargeCreem(order.TradeNo, "", "", "127.0.0.1") }},
		{"waffo", PaymentProviderWaffo, 4, func(order *TopUp) error { return RechargeWaffo(order.TradeNo, "127.0.0.1") }},
		{"waffo pancake", PaymentProviderWaffoPancake, 4, func(order *TopUp) error { return RechargeWaffoPancake(order.TradeNo) }},
		{"manual completion", PaymentProviderEpay, 4, func(order *TopUp) error { return ManualCompleteTopUp(order.TradeNo, "127.0.0.1") }},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			configureAffiliateRewardTest(t, 2500, true)
			suffix := "provider-" + strconv.Itoa(i)
			inviter, invitee := createAffiliateUsersForTest(t, suffix, 0, nil)
			order := createAffiliateTopUpForTest(t, invitee.Id, suffix, tc.provider, tc.amount, 4)
			require.NoError(t, tc.settle(order))
			require.NoError(t, tc.settle(order))

			var creditedInvitee, creditedInviter User
			require.NoError(t, DB.First(&creditedInvitee, invitee.Id).Error)
			require.NoError(t, DB.First(&creditedInviter, inviter.Id).Error)
			assert.Equal(t, 400, creditedInvitee.Quota)
			assert.Equal(t, 100, creditedInviter.Quota)
			assert.Equal(t, 150, creditedInviter.AffHistoryQuota)
			var rewards []AffiliateReward
			require.NoError(t, DB.Where("inviter_id = ?", inviter.Id).Find(&rewards).Error)
			require.Len(t, rewards, 1)
			assert.Equal(t, 400, rewards[0].BaseQuota)
			assert.Equal(t, 100, rewards[0].RewardQuota)
			assert.Equal(t, order.Id, rewards[0].SourceId)
		})
	}
}

func TestAffiliateRewardRedemptionDetailsAndExcludedSources(t *testing.T) {
	truncateTables(t)
	configureAffiliateRewardTest(t, 1000, true)
	inviter, invitee := createAffiliateUsersForTest(t, "redemption", 0, nil)
	require.NoError(t, DB.Model(invitee).Update("username", "小明同学").Error)
	code := &Redemption{
		Name: "quota-code", Key: "30000000000000000000000000000001",
		Type: RedemptionTypeQuota, Status: common.RedemptionCodeStatusEnabled, Quota: 101,
	}
	require.NoError(t, code.Insert())
	result, err := RedeemCode(code.Key, invitee.Id)
	require.NoError(t, err)
	assert.Equal(t, 101, result.Quota)
	_, err = RedeemCode(code.Key, invitee.Id)
	require.ErrorIs(t, err, ErrRedeemFailed)

	plan := &SubscriptionPlan{Title: "Affiliate exclusion plan", DurationUnit: SubscriptionDurationDay, DurationValue: 30, Enabled: true, TotalAmount: 1000}
	require.NoError(t, DB.Create(plan).Error)
	subscriptionCode := &Redemption{
		Name: "subscription-code", Key: "30000000000000000000000000000002",
		Type: RedemptionTypeSubscription, PlanId: plan.Id, Status: common.RedemptionCodeStatusEnabled,
	}
	require.NoError(t, subscriptionCode.Insert())
	_, err = RedeemCode(subscriptionCode.Key, invitee.Id)
	require.NoError(t, err)
	require.NoError(t, IncreaseUserQuota(invitee.Id, 50, true))

	page := &common.PageInfo{Page: 1, PageSize: 10}
	rewards, total, err := GetAffiliateRewards(inviter.Id, "", page)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rewards, 1)
	assert.Equal(t, "小***", rewards[0].InviteeName)
	assert.Equal(t, AffiliateRewardSourceRedemption, rewards[0].SourceType)
	assert.Equal(t, 10, rewards[0].RewardQuota)
	assert.Equal(t, 101, rewards[0].BaseQuota)
	assert.Equal(t, 1000, rewards[0].RateBps)

	// A later configuration change cannot rewrite the settled snapshot.
	common.OptionMapRWMutex.Lock()
	common.OptionMap[operation_setting.AffiliateRewardRateOptionKey] = "5000"
	common.OptionMapRWMutex.Unlock()
	rewards, _, err = GetAffiliateRewards(inviter.Id, AffiliateRewardSourceRedemption, page)
	require.NoError(t, err)
	assert.Equal(t, 1000, rewards[0].RateBps)
	assert.Equal(t, 10, rewards[0].RewardQuota)
	assert.Error(t, DB.Create(&AffiliateReward{
		InviterId: inviter.Id, InviteeId: invitee.Id, SourceType: AffiliateRewardSourceRedemption, SourceId: code.Id,
	}).Error, "a source cannot create a second rebate for the same inviter")

	otherInviter := &User{Username: "another-inviter", AffCode: "another-aff", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(otherInviter).Error)
	otherRewards, otherTotal, err := GetAffiliateRewards(otherInviter.Id, "", page)
	require.NoError(t, err)
	assert.Zero(t, otherTotal)
	assert.Empty(t, otherRewards)
}

func TestAffiliateTransferRejectsWalletOverflow(t *testing.T) {
	truncateTables(t)
	configureAffiliateRewardTest(t, 0, true)
	common.QuotaPerUnit = 50
	user := &User{Username: "affiliate-transfer", AffCode: "affiliate-transfer", Quota: common.MaxWalletQuota - 50, AffQuota: 100}
	require.NoError(t, DB.Create(user).Error)
	require.ErrorIs(t, user.TransferAffQuotaToQuota(100), ErrWalletQuotaLimitExceeded)
	var unchanged User
	require.NoError(t, DB.First(&unchanged, user.Id).Error)
	assert.Equal(t, common.MaxWalletQuota-50, unchanged.Quota)
	assert.Equal(t, 100, unchanged.AffQuota)
	require.NoError(t, user.TransferAffQuotaToQuota(50))
	assert.Equal(t, common.MaxWalletQuota, user.Quota)
	assert.Equal(t, 50, user.AffQuota)
}

func TestAffiliateRewardCounterOverflowRollsBackPayment(t *testing.T) {
	truncateTables(t)
	configureAffiliateRewardTest(t, 10000, true)
	inviter, invitee := createAffiliateUsersForTest(t, "counter-overflow", 0, nil)
	require.NoError(t, DB.Model(inviter).Update("aff_history", math.MaxInt).Error)
	order := createAffiliateTopUpForTest(t, invitee.Id, "counter-overflow", PaymentProviderEpay, 1, 1)
	_, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.Error(t, err)
	require.NoError(t, DB.First(order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	require.NoError(t, DB.First(invitee, invitee.Id).Error)
	assert.Zero(t, invitee.Quota)
	require.NoError(t, DB.First(inviter, inviter.Id).Error)
	assert.Zero(t, inviter.Quota)
	assert.Equal(t, math.MaxInt, inviter.AffHistoryQuota)
	var count int64
	require.NoError(t, DB.Model(&AffiliateReward{}).Count(&count).Error)
	assert.Zero(t, count)
}

// TestAffiliateRewardDatabaseLifecycle verifies real database migrations and
// settlement. Upgrade mode requires a fixture created by the released binary.
func TestAffiliateRewardDatabaseLifecycle(t *testing.T) {
	mode := os.Getenv("TEST_AFFILIATE_DATABASE_MODE")
	if mode == "" {
		t.Skip("TEST_AFFILIATE_DATABASE_MODE is not configured")
	}
	require.Contains(t, []string{"fresh", "upgrade"}, mode)
	dialect := os.Getenv("TEST_DATABASE_DIALECT")
	require.Contains(t, []string{"sqlite", "mysql", "postgres"}, dialect)
	database := os.Getenv("TEST_DATABASE_NAME")
	previousDB, previousLogDB := DB, LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousSQLitePath := common.IsMasterNode, common.SQLitePath
	t.Cleanup(func() {
		if LOG_DB != nil && LOG_DB != previousLogDB && LOG_DB != DB {
			assert.NoError(t, closeDB(LOG_DB))
		}
		if DB != nil && DB != previousDB {
			assert.NoError(t, closeDB(DB))
		}
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.IsMasterNode, common.SQLitePath = previousMaster, previousSQLitePath
		initCol()
	})
	common.IsMasterNode = true
	t.Setenv("SKIP_64BIT_QUOTA_SCHEMA_CHECK", "false")
	t.Setenv("SQL_DSN", migrationFixtureDSN(t, dialect, database, mode == "fresh"))
	mainSQLitePath := common.SQLitePath
	t.Setenv("LOG_SQL_DSN", migrationFixtureDSN(t, dialect, database+"_log", mode == "fresh"))
	logSQLitePath := common.SQLitePath
	common.SQLitePath = mainSQLitePath
	require.NoError(t, InitDB())
	common.SQLitePath = logSQLitePath
	require.NoError(t, InitLogDB())
	for _, connection := range []*gorm.DB{DB, LOG_DB} {
		versionSQL := "SELECT version()"
		if connection.Dialector.Name() == "sqlite" {
			versionSQL = "SELECT sqlite_version()"
		}
		var version string
		require.NoError(t, connection.Raw(versionSQL).Scan(&version).Error)
		t.Logf("%s %s (%s)", connection.Dialector.Name(), version, mode)
	}

	var inviter, invitee User
	if mode == "upgrade" {
		require.NoError(t, DB.Where("username = ?", "existing-affiliate-inviter").First(&inviter).Error)
		require.NoError(t, DB.Where("username = ?", "existing-affiliate-invitee").First(&invitee).Error)
		assert.Equal(t, 5_000_000_000, inviter.Quota)
		assert.Equal(t, 30, inviter.AffQuota)
		assert.Equal(t, 50, inviter.AffHistoryQuota)
		assert.Nil(t, inviter.AffRewardRate)
		assert.Equal(t, inviter.Id, invitee.InviterId)
		var entry Log
		require.NoError(t, LOG_DB.Where("content = ?", "保留邀请记录").First(&entry).Error)
		assert.Equal(t, inviter.Id, entry.UserId)
		var storedIndexes Option
		require.NoError(t, DB.Where(commonKeyCol+" = ?", "AffiliateFixtureIndexes").First(&storedIndexes).Error)
		var baseline map[string]map[string][]string
		require.NoError(t, common.UnmarshalJsonStr(storedIndexes.Value, &baseline))
		for name, connection := range map[string]*gorm.DB{"main": DB, "log": LOG_DB} {
			for table, signatures := range baseline[name] {
				actual := migrationFixtureIndexes(t, connection, []string{table})
				for _, signature := range signatures {
					assert.Contains(t, actual[table], signature, "%s.%s index must survive upgrade", name, table)
				}
			}
		}
	} else {
		inviter = User{Username: "fresh-affiliate-inviter", AffCode: "fresh-inviter", Quota: 5_000_000_000, AffQuota: 30, AffHistoryQuota: 50}
		require.NoError(t, DB.Create(&inviter).Error)
		invitee = User{Username: "fresh-affiliate-invitee", AffCode: "fresh-invitee", InviterId: inviter.Id}
		require.NoError(t, DB.Create(&invitee).Error)
	}
	configureAffiliateRewardTest(t, 1250, true)
	order := createAffiliateTopUpForTest(t, invitee.Id, "matrix", PaymentProviderEpay, 3, 3)
	alreadyDone, err := RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.False(t, alreadyDone)
	alreadyDone, err = RechargeEpay(order.TradeNo, "alipay", "127.0.0.1")
	require.NoError(t, err)
	assert.True(t, alreadyDone)
	require.NoError(t, DB.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 5_000_000_037, inviter.Quota)
	require.NoError(t, DB.Model(&inviter).Update("quota", common.MaxWalletQuota-5).Error)
	code := &Redemption{Name: "matrix quota code", Key: "50000000000000000000000000000001", Status: common.RedemptionCodeStatusEnabled, Quota: 1001}
	require.NoError(t, code.Insert())
	_, err = RedeemCode(code.Key, invitee.Id)
	require.NoError(t, err)
	_, err = RedeemCode(code.Key, invitee.Id)
	require.ErrorIs(t, err, ErrRedeemFailed)
	require.NoError(t, DB.First(&inviter, inviter.Id).Error)
	assert.Equal(t, common.MaxWalletQuota, inviter.Quota)
	assert.Equal(t, 150, inviter.AffQuota)
	assert.Equal(t, 212, inviter.AffHistoryQuota)
	require.NoError(t, DB.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 1301, invitee.Quota)

	page := &common.PageInfo{Page: 1, PageSize: 1}
	rewards, total, err := GetAffiliateRewards(inviter.Id, "", page)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, rewards, 1)
	assert.Equal(t, AffiliateRewardSourceRedemption, rewards[0].SourceType)
	assert.Equal(t, 125, rewards[0].RewardQuota)
	assert.Equal(t, 5, rewards[0].CreditedQuota)
	assert.Equal(t, 120, rewards[0].PendingQuota)
	assert.NotEmpty(t, rewards[0].InviteeName)
	assert.NotEqual(t, invitee.Username, rewards[0].InviteeName)
	page.Page = 2
	rewards, _, err = GetAffiliateRewards(inviter.Id, "", page)
	require.NoError(t, err)
	require.Len(t, rewards, 1)
	assert.Equal(t, order.Id, rewards[0].SourceId)
	page.Page = 1
	rewards, total, err = GetAffiliateRewards(inviter.Id, AffiliateRewardSourceTopUp, page)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rewards, 1)
	assert.Error(t, DB.Create(&AffiliateReward{InviterId: inviter.Id, SourceType: AffiliateRewardSourceTopUp, SourceId: order.Id}).Error)
	assert.Error(t, DB.Create(&User{Username: inviter.Username, AffCode: "duplicate-user"}).Error)

	require.ErrorIs(t, inviter.TransferAffQuotaToQuota(100), ErrWalletQuotaLimitExceeded)
	require.NoError(t, DB.Model(&inviter).Update("quota", common.MaxWalletQuota-100).Error)
	require.NoError(t, inviter.TransferAffQuotaToQuota(100))
	assert.Equal(t, common.MaxWalletQuota, inviter.Quota)
	assert.Equal(t, 50, inviter.AffQuota)
	assert.Equal(t, 212, inviter.AffHistoryQuota)

	recorder := &migrationSQLRecorder{}
	DB = DB.Session(&gorm.Session{Logger: recorder})
	LOG_DB = LOG_DB.Session(&gorm.Session{Logger: recorder})
	for range 2 {
		recorder.reset()
		require.NoError(t, migrateDB())
		require.NoError(t, migrateLOGDB())
		assert.Empty(t, recorder.schemaMutations(), "repeated startup must preserve the schema")
	}
	rewards, total, err = GetAffiliateRewards(inviter.Id, "", page)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, rewards, 1)
	assert.Equal(t, 1250, rewards[0].RateBps)
	assert.Equal(t, 125, rewards[0].RewardQuota)
}

func TestAffiliateRewardSQLiteMigrationPreservesExistingUsers(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	require.NoError(t, db.AutoMigrate(&User{}))
	require.NoError(t, db.Migrator().DropColumn(&User{}, "aff_reward_rate"))
	require.NoError(t, db.Exec("INSERT INTO users (id, username, password, quota, aff_quota, aff_history) VALUES (?, ?, ?, ?, ?, ?)", 11, "existing-inviter", "unused-hash", 120, 30, 50).Error)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&User{}, &AffiliateReward{}))
	}
	var user User
	require.NoError(t, db.First(&user, 11).Error)
	assert.Equal(t, 120, user.Quota)
	assert.Equal(t, 30, user.AffQuota)
	assert.Equal(t, 50, user.AffHistoryQuota)
	assert.Nil(t, user.AffRewardRate)

	reward := &AffiliateReward{InviterId: 11, InviteeId: 12, SourceType: AffiliateRewardSourceTopUp, SourceId: 20, RateBps: 1250, BaseQuota: 100, RewardQuota: 12, CreditedQuota: 12}
	require.NoError(t, db.Create(reward).Error)
	assert.Error(t, db.Create(&AffiliateReward{InviterId: 11, InviteeId: 12, SourceType: AffiliateRewardSourceTopUp, SourceId: 20}).Error)
}
