package protocol

import "fmt"

const (
	// MaxSubscribeMultiQueriesHard bounds decoder allocation independently of
	// hosted configuration.
	MaxSubscribeMultiQueriesHard uint32 = 4_096
	// DefaultSubscriptionMaxQueriesPerSet bounds compile work for one hosted
	// SubscribeMulti request.
	DefaultSubscriptionMaxQueriesPerSet = 256
)

// SubscriptionLimits bounds protocol-edge subscription admission work.
type SubscriptionLimits struct {
	MaxQueriesPerSet int
}

// NormalizeSubscriptionLimits validates and fills hosted defaults.
func NormalizeSubscriptionLimits(limits SubscriptionLimits) (SubscriptionLimits, error) {
	if limits.MaxQueriesPerSet < 0 {
		return SubscriptionLimits{}, fmt.Errorf("subscription max queries per set must not be negative")
	}
	if limits.MaxQueriesPerSet == 0 {
		limits.MaxQueriesPerSet = DefaultSubscriptionMaxQueriesPerSet
	}
	if uint64(limits.MaxQueriesPerSet) > uint64(MaxSubscribeMultiQueriesHard) {
		return SubscriptionLimits{}, fmt.Errorf("subscription max queries per set %d exceeds decoder hard limit %d", limits.MaxQueriesPerSet, MaxSubscribeMultiQueriesHard)
	}
	return limits, nil
}
