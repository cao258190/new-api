package model

import (
	"errors"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	AffiliateRewardSourceTopUp      = "topup"
	AffiliateRewardSourceRedemption = "redemption"
)

// AffiliateReward is an immutable snapshot of a settled referral rebate.
type AffiliateReward struct {
	Id            int    `json:"id"`
	InviterId     int    `json:"inviter_id" gorm:"index;uniqueIndex:idx_affiliate_reward_source,priority:3"`
	InviteeId     int    `json:"invitee_id" gorm:"index"`
	InviteeName   string `json:"invitee_name" gorm:"-:all"`
	SourceType    string `json:"source_type" gorm:"type:varchar(16);uniqueIndex:idx_affiliate_reward_source,priority:1"`
	SourceId      int    `json:"source_id" gorm:"uniqueIndex:idx_affiliate_reward_source,priority:2"`
	RateBps       int    `json:"rate_bps"`
	BaseQuota     int    `json:"base_quota"`
	RewardQuota   int    `json:"reward_quota"`
	CreditedQuota int    `json:"credited_quota"`
	PendingQuota  int    `json:"pending_quota"`
	CreatedAt     int64  `json:"created_at" gorm:"autoCreateTime"`
}

// creditTopUpWithAffiliateRewardTx keeps the wallet credit and referral rebate atomic.
func creditTopUpWithAffiliateRewardTx(tx *gorm.DB, topUp *TopUp, quota int, updates map[string]any) (AffiliateReward, error) {
	if err := creditTopUpQuota(tx, topUp.UserId, quota, updates); err != nil {
		return AffiliateReward{}, err
	}
	return grantAffiliateRewardTx(tx, topUp.UserId, AffiliateRewardSourceTopUp, topUp.Id, quota)
}

// grantAffiliateRewardTx credits an inviter and records the source in the same
// transaction that credited the invitee. The caller syncs the inviter's cache
// only after the transaction commits.
func grantAffiliateRewardTx(tx *gorm.DB, inviteeID int, sourceType string, sourceID int, baseQuota int) (AffiliateReward, error) {
	var reward AffiliateReward
	if !operation_setting.IsPaymentComplianceConfirmed() || baseQuota <= 0 {
		return reward, nil
	}
	if sourceID <= 0 || (sourceType != AffiliateRewardSourceTopUp && sourceType != AffiliateRewardSourceRedemption) {
		return reward, errors.New("invalid affiliate reward source")
	}

	var invitee User
	if err := tx.Select("inviter_id").Where("id = ?", inviteeID).First(&invitee).Error; err != nil {
		return reward, err
	}
	if invitee.InviterId <= 0 || invitee.InviterId == inviteeID {
		return reward, nil
	}

	var inviter User
	err := lockForUpdate(tx).Select("id", "quota", "aff_quota", "aff_history", "aff_reward_rate").Where("id = ?", invitee.InviterId).First(&inviter).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return reward, nil
	}
	if err != nil {
		return reward, err
	}
	rate := operation_setting.EffectiveAffiliateRewardRate(inviter.AffRewardRate)
	if rate == 0 {
		return reward, nil
	}

	// Shift is exact for a basis-point rate, so flooring cannot round a rebate up.
	rewardQuota, err := common.WalletQuotaFromDecimalStrict(
		decimal.NewFromInt(int64(baseQuota)).Mul(decimal.NewFromInt(int64(rate))).Shift(-4).Floor(),
	)
	if err != nil {
		return reward, err
	}
	if rewardQuota == 0 {
		return reward, nil
	}

	creditedQuota := rewardQuota
	if inviter.Quota > common.MaxWalletQuota-rewardQuota {
		creditedQuota = max(0, common.MaxWalletQuota-inviter.Quota)
	}
	pendingQuota := rewardQuota - creditedQuota
	if inviter.AffQuota < 0 || inviter.AffHistoryQuota < 0 {
		return reward, errors.New("affiliate reward account has invalid balance")
	}
	if inviter.AffQuota > math.MaxInt-pendingQuota || inviter.AffHistoryQuota > math.MaxInt-rewardQuota {
		return reward, errors.New("affiliate reward counter exceeds integer range")
	}

	reward = AffiliateReward{
		InviterId:     inviter.Id,
		InviteeId:     inviteeID,
		SourceType:    sourceType,
		SourceId:      sourceID,
		RateBps:       rate,
		BaseQuota:     baseQuota,
		RewardQuota:   rewardQuota,
		CreditedQuota: creditedQuota,
		PendingQuota:  pendingQuota,
		CreatedAt:     common.GetTimestamp(),
	}
	if err := tx.Create(&reward).Error; err != nil {
		return AffiliateReward{}, err
	}

	query := tx.Model(&User{}).
		Where("id = ? AND aff_quota <= ? AND aff_history <= ?", inviter.Id, math.MaxInt-pendingQuota, math.MaxInt-rewardQuota)
	if creditedQuota > 0 {
		query = query.Where("quota <= ?", common.MaxWalletQuota-creditedQuota)
	}
	updated := query.Updates(map[string]any{
		"quota":       gorm.Expr("quota + ?", creditedQuota),
		"aff_quota":   gorm.Expr("aff_quota + ?", pendingQuota),
		"aff_history": gorm.Expr("aff_history + ?", rewardQuota),
	})
	if updated.Error != nil {
		return AffiliateReward{}, updated.Error
	}
	if updated.RowsAffected != 1 {
		return AffiliateReward{}, errors.New("affiliate reward balance changed during settlement")
	}
	return reward, nil
}

// syncAffiliateRewardQuotaCache makes the immediately credited share usable.
func syncAffiliateRewardQuotaCache(reward AffiliateReward) {
	syncCreditUserQuotaCache(reward.InviterId, reward.CreditedQuota, common.LogText("referral rebate"))
}

// GetAffiliateRewards returns one inviter's rebate snapshots, newest first.
func GetAffiliateRewards(inviterID int, sourceType string, pageInfo *common.PageInfo) ([]AffiliateReward, int64, error) {
	query := DB.Model(&AffiliateReward{}).Where("inviter_id = ?", inviterID)
	if sourceType != "" {
		query = query.Where("source_type = ?", sourceType)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rewards []AffiliateReward
	if err := query.Order("id DESC").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&rewards).Error; err != nil {
		return nil, 0, err
	}
	if len(rewards) == 0 {
		return rewards, total, nil
	}

	inviteeIDs := make([]int, 0, len(rewards))
	for _, reward := range rewards {
		inviteeIDs = append(inviteeIDs, reward.InviteeId)
	}
	var invitees []User
	if err := DB.Model(&User{}).Select("id", "username").Where("id IN ?", inviteeIDs).Find(&invitees).Error; err != nil {
		return nil, 0, err
	}
	names := make(map[int]string, len(invitees))
	for _, invitee := range invitees {
		runes := []rune(invitee.Username)
		if len(runes) == 0 {
			continue
		}
		if len(runes) == 1 {
			names[invitee.Id] = "*"
		} else {
			names[invitee.Id] = string(runes[0]) + strings.Repeat("*", min(len(runes)-1, 3))
		}
	}
	for i := range rewards {
		rewards[i].InviteeName = names[rewards[i].InviteeId]
	}
	return rewards, total, nil
}
