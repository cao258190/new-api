package operation_setting

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

const (
	AffiliateRewardRateOptionKey = "AffiliateRewardRate"
	MaxAffiliateRewardRate       = 10000 // Basis points: 10000 = 100%.
)

// ParseAffiliateRewardRate accepts an integer number of basis points.
func ParseAffiliateRewardRate(value string) (int, error) {
	rate, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || rate < 0 || rate > MaxAffiliateRewardRate {
		return 0, common.NewMessage("Referral rebate rate must be between 0 and 100% with at most two decimal places")
	}
	return rate, nil
}

// GetAffiliateRewardRate returns the configured default rate in basis points.
func GetAffiliateRewardRate() int {
	common.OptionMapRWMutex.RLock()
	value := common.OptionMap[AffiliateRewardRateOptionKey]
	common.OptionMapRWMutex.RUnlock()
	rate, err := ParseAffiliateRewardRate(value)
	if err != nil {
		return 0
	}
	return rate
}

// EffectiveAffiliateRewardRate applies an inviter's override and the payment gate.
func EffectiveAffiliateRewardRate(override *int) int {
	if !IsPaymentComplianceConfirmed() {
		return 0
	}
	if override != nil {
		if *override < 0 || *override > MaxAffiliateRewardRate {
			return 0
		}
		return *override
	}
	return GetAffiliateRewardRate()
}
