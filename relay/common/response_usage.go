package common

import "github.com/QuantumNous/new-api/relaykit/dto"

// RewriteResponseUsage applies this attempt's usage policy before a protocol
// handler writes usage or returns accounting facts. Rewritten copies carry a
// local guard through protocol conversion; original accumulators stay intact.
func (info *RelayInfo) RewriteResponseUsage(usage *dto.Usage) *dto.Usage {
	if info == nil || usage == nil || info.ResponseUsageRewrite == nil || usage.ResponseUsageRewritten ||
		usage.BillingUsage != nil && usage.BillingUsage.ResponseUsageRewritten {
		return usage
	}
	return info.ResponseUsageRewrite(usage)
}
