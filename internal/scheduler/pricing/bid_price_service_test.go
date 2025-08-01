package pricing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/resource"

	schedulerconfiguration "github.com/armadaproject/armada/internal/scheduler/configuration"
	"github.com/armadaproject/armada/internal/scheduler/internaltypes"
	"github.com/armadaproject/armada/pkg/bidstore"
)

var testResourceListFactory, _ = internaltypes.NewResourceListFactory(
	[]schedulerconfiguration.ResourceType{
		{Name: "memory", Resolution: resource.MustParse("1")},
		{Name: "cpu", Resolution: resource.MustParse("1m")},
		{Name: "nvidia.com/gpu", Resolution: resource.MustParse("1m")},
	},
	[]schedulerconfiguration.FloatingResourceConfig{},
)

func TestConvert(t *testing.T) {
	tests := map[string]struct {
		input    *bidstore.RetrieveBidsResponse
		expected BidPriceSnapshot
	}{
		"empty input": {
			input: &bidstore.RetrieveBidsResponse{},
			expected: BidPriceSnapshot{
				Bids:          make(map[PriceKey]map[string]Bid),
				ResourceUnits: map[string]internaltypes.ResourceList{},
			},
		},
		"resourceUnit": {
			input: &bidstore.RetrieveBidsResponse{
				PoolResourceUnits: map[string]*bidstore.ResourceShape{
					"pool1": {
						Resources: map[string]*resource.Quantity{
							"cpu":    mustParsePtr("1"),
							"memory": mustParsePtr("1Gi"),
						},
					},
					"pool2": {
						Resources: map[string]*resource.Quantity{
							"cpu":            mustParsePtr("2"),
							"memory":         mustParsePtr("2Gi"),
							"nvidia.com/gpu": mustParsePtr("1"),
						},
					},
				},
			},
			expected: BidPriceSnapshot{
				Bids: make(map[PriceKey]map[string]Bid),
				ResourceUnits: map[string]internaltypes.ResourceList{
					"pool1": testResourceListFactory.FromNodeProto(
						map[string]*resource.Quantity{
							"cpu":    mustParsePtr("1"),
							"memory": mustParsePtr("1Gi"),
						},
					),
					"pool2": testResourceListFactory.FromNodeProto(
						map[string]*resource.Quantity{
							"cpu":            mustParsePtr("2"),
							"memory":         mustParsePtr("2Gi"),
							"nvidia.com/gpu": mustParsePtr("1"),
						},
					),
				},
			},
		},
		"single bid with both phases": {
			input: &bidstore.RetrieveBidsResponse{
				QueueBids: map[string]*bidstore.QueueBids{
					"queue1": {
						PoolBids: map[string]*bidstore.PoolBids{
							"pool1": {
								PriceBandBids: []*bidstore.PriceBandBid{
									{
										PriceBand: bidstore.PriceBand_PRICE_BAND_A,
										PriceBandBids: &bidstore.PriceBandBids{
											PricingPhaseBids: []*bidstore.PricingPhaseBid{
												CreateQueuedBid(1.0),
												CreateRunningBid(2.0),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			expected: BidPriceSnapshot{
				Bids: map[PriceKey]map[string]Bid{
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_A,
					}: {
						"pool1": {
							QueuedBid:  1.0,
							RunningBid: 2.0,
						},
					},
				},
				ResourceUnits: map[string]internaltypes.ResourceList{},
			},
		},
		"uses fallback if direct bid is missing": {
			input: &bidstore.RetrieveBidsResponse{
				QueueBids: map[string]*bidstore.QueueBids{
					"queue1": {
						PoolBids: map[string]*bidstore.PoolBids{
							"pool1": {
								FallbackBids: &bidstore.PriceBandBids{
									PricingPhaseBids: []*bidstore.PricingPhaseBid{
										CreateQueuedBid(5.0),
										CreateRunningBid(10.0),
									},
								},
							},
						},
					},
				},
			},
			expected: BidPriceSnapshot{
				Bids: map[PriceKey]map[string]Bid{
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_A,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_B,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_C,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_D,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_E,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_F,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_G,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_H,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
					{
						Queue: "queue1",
						Band:  bidstore.PriceBand_PRICE_BAND_UNSPECIFIED,
					}: {
						"pool1": {
							QueuedBid:  5.0,
							RunningBid: 10.0,
						},
					},
				},
				ResourceUnits: map[string]internaltypes.ResourceList{},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			actual := convert(tc.input, testResourceListFactory)

			// Remove non-deterministic time field before comparison
			require.False(t, actual.Timestamp.IsZero(), "timestamp should be set")
			actual.Timestamp = time.Time{}

			assert.Equal(t, tc.expected, actual)
		})
	}
}

func CreateQueuedBid(p float64) *bidstore.PricingPhaseBid {
	return &bidstore.PricingPhaseBid{
		PricingPhase: bidstore.PricingPhase_PRICING_PHASE_QUEUEING,
		Bid: &bidstore.Bid{
			Amount: p,
		},
	}
}

func CreateRunningBid(p float64) *bidstore.PricingPhaseBid {
	return &bidstore.PricingPhaseBid{
		PricingPhase: bidstore.PricingPhase_PRICING_PHASE_RUNNING,
		Bid: &bidstore.Bid{
			Amount: p,
		},
	}
}

func mustParsePtr(qty string) *resource.Quantity {
	q := resource.MustParse(qty)
	return &q
}
