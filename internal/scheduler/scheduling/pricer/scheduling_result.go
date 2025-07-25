package pricer

import (
	"github.com/armadaproject/armada/internal/scheduler/internaltypes"
	"github.com/armadaproject/armada/internal/scheduler/scheduling/context"
)

type schedulingResult struct {
	scheduled      bool
	schedulingCost float64
	results        []*NodeSchedulingResult
	// Used to tie-break when sorting
	resultId string
}

type schedulingCostOrder []*schedulingResult

func (sco schedulingCostOrder) Len() int {
	return len(sco)
}

func (sco schedulingCostOrder) Less(i, j int) bool {
	if sco[i].schedulingCost < sco[j].schedulingCost {
		return true
	}
	if sco[i].schedulingCost == sco[j].schedulingCost {
		return sco[i].resultId < sco[j].resultId
	}

	return false
}

func (sco schedulingCostOrder) Swap(i, j int) {
	sco[i], sco[j] = sco[j], sco[i]
}

type NodeSchedulingResult struct {
	scheduled       bool
	jctx            *context.JobSchedulingContext
	node            *internaltypes.Node
	price           float64
	jobIdsToPreempt []string
	// Used to tie-break when sorting
	resultId string
}

type nodeCostOrder []*NodeSchedulingResult

func (nco nodeCostOrder) Len() int {
	return len(nco)
}

func (nco nodeCostOrder) Less(i, j int) bool {
	if nco[i].price != nco[j].price {
		return nco[i].price < nco[j].price
	}

	return nco[i].resultId < nco[j].resultId
}

func (nco nodeCostOrder) Swap(i, j int) {
	nco[i], nco[j] = nco[j], nco[i]
}
