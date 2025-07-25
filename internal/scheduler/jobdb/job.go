package jobdb

import (
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	v1 "k8s.io/api/core/v1"

	armadamaps "github.com/armadaproject/armada/internal/common/maps"
	"github.com/armadaproject/armada/internal/common/types"
	"github.com/armadaproject/armada/internal/scheduler/adapters"
	"github.com/armadaproject/armada/internal/scheduler/internaltypes"
	"github.com/armadaproject/armada/internal/scheduler/pricing"
	"github.com/armadaproject/armada/pkg/bidstore"
)

// Job is the scheduler-internal representation of a job.
type Job struct {
	// Pointer to the jobDb used to create this job.
	// Not included in equals comparison.
	jobDb *JobDb
	// String representation of the job id.
	id string
	// Name of the queue this job belongs to.
	queue string
	// JobSet that the job belongs to.
	// We store this as it's needed for sending job event messages.
	jobSet string
	// Per-queue priority of this job.
	priority uint32
	// Requested per queue priority of this job.
	// This is used when syncing the postgres database with the scheduler-internal database.
	requestedPriority uint32
	// Job submission time in nanoseconds since the epoch.
	// I.e., the value returned by time.UnixNano().
	submittedTime int64
	// Hash of the scheduling requirements of the job.
	schedulingKey internaltypes.SchedulingKey
	// True if the job has been validated by the scheduler.
	// Any job that fails validation will be rejected.
	validated bool
	// True if the job is currently queued.
	// If this is set then the job will not be considered for scheduling.
	queued bool
	// The current version of the queued state.
	queuedVersion int32
	// Scheduling requirements of this job.
	jobSchedulingInfo *internaltypes.JobSchedulingInfo
	// All resource requirements, including floating resources, for this job
	allResourceRequirements internaltypes.ResourceList
	// Kubernetes (i.e. non-floating) resource requirements of this job
	kubernetesResourceRequirements internaltypes.ResourceList
	// Priority class of this job. Populated automatically on job creation.
	priorityClass types.PriorityClass
	// True if the user has requested this job be cancelled
	cancelRequested bool
	// The (first) user who cancelled this job
	cancelUser *string
	// True if the user has requested this job's jobSet be cancelled
	cancelByJobSetRequested bool
	// True if the scheduler has cancelled the job
	cancelled bool
	// True if the scheduler has failed the job
	failed bool
	// True if the scheduler has marked the job as succeeded
	succeeded bool
	// Job Runs by run id
	runsById map[string]*JobRun
	// The currently active run. The run with the latest timestamp is the active run.
	activeRun *JobRun
	// The timestamp of the currently active run.
	activeRunTimestamp int64
	// Pools for which the job is eligible. This is used for metrics reporting and to calculate demand for fair share
	pools []string
	// Price band of the job is associated with, it is used to determine the price set on the job
	priceBand bidstore.PriceBand
	// The bid price for each pool
	// A job doesn't have to have a bid for every pool, it'll default to 0 bid if not set in this map
	bidPricesPool map[string]pricing.Bid
	// Gang information for this job
	gangInfo GangInfo
}

func (job *Job) String() string {
	// Include field names in string representation by default.
	return fmt.Sprintf("%#v", job)
}

// Assert makes the assertions outlined below and returns
// - nil if the job is valid and
// - an error explaining why not otherwise.
//
// Assertions:
// Required fields must be set.
//
// All associated runs are valid. The state of LatestRun is compatible with that of the job.
//
// The states {Queued, Running, Cancelled, Failed, Succeeded} are mutually exclusive.
//
// Only these job state transitions are valid:
// - Queued -> {Running, Cancelled, Failed}
// - Running -> {Queued, Cancelled, Failed, Succeeded}
// - Succeeded, Cancelled, Failed -> {}
//
// QueuedVersion is 0 initially and is incremented by 1 every time the queued status of the job changes. Hence:
// - If the job is queued, the queuedVersion must be 2x the number of runs. E.g.:
//   - queued (qv = 0)
//   - queued -> running -> queued: qv = 2
//
// - If the job is running or succeeded, the queuedVersion must be one less than 2x number of runs. E.g.:
//   - queued -> running -> succeeded: qv = 1
//   - queued -> running -> queued -> running -> succeeded: qv = 3
//
// - If the job is failed or cancelled, the queuedVersion may be either equal to or 1 less than 2x the number of runs. E.g.:
//   - queued -> failed/cancelled: qv = 0
//   - queued -> running -> failed/cancelled: qv = 1
//   - queued -> running -> queued -> failed/cancelled: qv = 2
func (job *Job) Assert() error {
	if job == nil {
		return errors.Errorf("job is nil")
	}
	var result *multierror.Error

	// Assert that required fields are set.
	if job.Id() == "" {
		result = multierror.Append(result, errors.New("job has an empty id"))
	}
	if job.Queue() == "" {
		result = multierror.Append(result, errors.New("job has an empty queue"))
	}
	if job.Jobset() == "" {
		result = multierror.Append(result, errors.New("job has an empty jobSet"))
	}

	// Assert that runs are valid.
	latestRun := job.LatestRun()
	for runId, run := range job.runsById {
		if runId != run.Id() {
			result = multierror.Append(result, errors.Errorf("run %s is keyed by %s", run.Id(), runId))
		}
		if run.Created() > latestRun.Created() {
			result = multierror.Append(result, errors.Errorf("run %s is more recent than latestRun", run.Id()))
		}
		if run.JobId() != job.Id() {
			result = multierror.Append(result, errors.Errorf("run %s has a conflicting jobId", run.Id()))
		}
		if err := run.Assert(); err != nil {
			result = multierror.Append(result, err)
		}
	}

	// Assertions specific to the state of the job.
	if job.Queued() {
		if job.Succeeded() {
			result = multierror.Append(result, errors.New("job is marked as both queued and succeeded"))
		}
		if job.Cancelled() {
			result = multierror.Append(result, errors.New("job is marked as both queued and cancelled"))
		}
		if job.Failed() {
			result = multierror.Append(result, errors.New("job is marked as both queued and failed"))
		}
		if job.InTerminalState() {
			result = multierror.Append(result, errors.New("job is marked as queued and terminated"))
		}

		if 2*int32(len(job.runsById)) != job.QueuedVersion() {
			result = multierror.Append(result, errors.Errorf("job is queued but queuedVersion (%d) is not 2x the number of runs (%d)", job.queuedVersion, len(job.runsById)))
		}

		if run := job.LatestRun(); run != nil {
			if run.Running() {
				result = multierror.Append(result, errors.New("job is queued but latestRun is running"))
			} else if run.Succeeded() {
				result = multierror.Append(result, errors.New("job is queued but latestRun is succeeded"))
			} else if run.Cancelled() {
				result = multierror.Append(result, errors.New("job is queued but latestRun is cancelled"))
			}
		}
	} else if job.Succeeded() {
		if job.Cancelled() {
			result = multierror.Append(result, errors.New("job is marked as both succeeded and cancelled"))
		}
		if job.Failed() {
			result = multierror.Append(result, errors.New("job is marked as both succeeded and failed"))
		}
		if !job.InTerminalState() {
			result = multierror.Append(result, errors.New("job is marked as succeeded but not terminated"))
		}

		if 2*int32(len(job.runsById))-1 != job.QueuedVersion() {
			result = multierror.Append(result, errors.Errorf("job is succeeded but queuedVersion (%d) is not one less than 2x the number of runs (%d)", job.queuedVersion, len(job.runsById)))
		}

		if run := job.LatestRun(); run != nil {
			if !run.Succeeded() {
				// A job succeeds iff a job succeeds.
				result = multierror.Append(result, errors.New("job is succeeded but latestRun is not"))
			}
		} else {
			result = multierror.Append(result, errors.New("job is succeeded but has no associated runs"))
		}
	} else if job.Cancelled() {
		if !job.CancelRequested() && !job.CancelByJobsetRequested() {
			result = multierror.Append(result, errors.New("job is cancelled but not marked as cancel requested"))
		}

		if run := job.LatestRun(); run != nil && !job.InTerminalState() {
			result = multierror.Append(result, errors.New("job is cancelled but has an active run"))
		}
		if job.Failed() {
			result = multierror.Append(result, errors.New("job is marked as both cancelled and failed"))
		}
		if !job.InTerminalState() {
			result = multierror.Append(result, errors.New("job is marked as cancelled but not terminated"))
		}

		if 2*int32(len(job.runsById)) != job.QueuedVersion() && 2*int32(len(job.runsById))-1 != job.QueuedVersion() {
			result = multierror.Append(result, errors.Errorf("job is cancelled but queuedVersion (%d) is not either equal to or one less than 2x the number of runs (%d)", job.queuedVersion, len(job.runsById)))
		}

		// A job may be cancelled regardless of whether it has associated runs or not,
		// and whether those runs succeeded or not.
		if run := job.LatestRun(); run != nil && !run.InTerminalState() {
			result = multierror.Append(result, errors.New("job is cancelled but has an active run"))
		}
	} else if job.Failed() {
		if !job.InTerminalState() {
			result = multierror.Append(result, errors.New("job is marked as failed but not terminated"))
		}

		if 2*int32(len(job.runsById)) != job.QueuedVersion() && 2*int32(len(job.runsById))-1 != job.QueuedVersion() {
			result = multierror.Append(result, errors.Errorf("job is failed but queuedVersion (%d) is not either equal to or one less than 2x the number of runs (%d)", job.queuedVersion, len(job.runsById)))
		}

		if run := job.LatestRun(); run != nil && !run.InTerminalState() {
			result = multierror.Append(result, errors.New("job is failed but has an active run"))
		}
	} else {
		// Job must be running if it's not in any of the other states.
		if 2*int32(len(job.runsById))-1 != job.QueuedVersion() {
			result = multierror.Append(result, errors.Errorf("job is running but queuedVersion (%d) is not one less than 2x the number of runs (%d)", job.queuedVersion, len(job.runsById)))
		}

		if run := job.LatestRun(); run != nil {
			if run.Cancelled() {
				result = multierror.Append(result, errors.New("job is running but latestRun is cancelled"))
			} else if run.Failed() {
				result = multierror.Append(result, errors.New("job is running but latestRun is failed"))
			} else if run.Succeeded() {
				result = multierror.Append(result, errors.New("job is running but latestRun is succeeded"))
			} else if run.Returned() {
				result = multierror.Append(result, errors.New("job is running but latestRun is returned"))
			}
		} else {
			result = multierror.Append(result, errors.New("job is running but has no associated run"))
		}
	}

	if err := result.ErrorOrNil(); err != nil {
		// Avoid allocating the message "... invalid state" string if there were no errors.
		return errors.WithMessagef(err, "invalid job: %s", job)
	}
	return nil
}

func (job *Job) ensureJobSchedulingInfoFieldsInitialised() {
	// Initialise the annotation and nodeSelector maps if nil.
	// Since those need to be mutated in-place.
	podReq := job.jobSchedulingInfo.PodRequirements
	if podReq.Annotations == nil {
		podReq.Annotations = make(map[string]string)
	}
	if podReq.NodeSelector == nil {
		podReq.NodeSelector = make(map[string]string)
	}
}

// Equal returns true if job is equal to other and false otherwise.
// Scheduling requirements are assumed to be equal if both jobs have equal schedulingKey.
func (job *Job) Equal(other *Job) bool {
	if job == other {
		return true
	}
	if job == nil && other != nil {
		return false
	}
	if job != nil && other == nil {
		return false
	}
	if job.id != other.id {
		return false
	}
	if job.queue != other.queue {
		return false
	}
	if job.jobSet != other.jobSet {
		return false
	}
	if job.priority != other.priority {
		return false
	}
	if job.requestedPriority != other.requestedPriority {
		return false
	}
	if job.submittedTime != other.submittedTime {
		return false
	}
	if job.schedulingKey != other.schedulingKey {
		// Assume jobSchedulingInfo/resourceRequirements are equal if schedulingKey is equal.
		return false
	}
	if job.queued != other.queued {
		return false
	}
	if job.queuedVersion != other.queuedVersion {
		return false
	}
	if !job.priorityClass.Equal(other.priorityClass) {
		return false
	}
	if job.queued != other.queued {
		return false
	}
	if job.queuedVersion != other.queuedVersion {
		return false
	}
	if job.validated != other.validated {
		return false
	}
	if job.cancelRequested != other.cancelRequested {
		return false
	}
	if job.cancelByJobSetRequested != other.cancelByJobSetRequested {
		return false
	}
	if job.cancelUser != other.cancelUser {
		return false
	}
	if job.cancelled != other.cancelled {
		return false
	}
	if job.failed != other.failed {
		return false
	}
	if job.succeeded != other.succeeded {
		return false
	}
	if !job.activeRun.Equal(other.activeRun) {
		return false
	}
	if job.activeRunTimestamp != other.activeRunTimestamp {
		return false
	}
	if !slices.Equal(job.pools, other.pools) {
		return false
	}
	if !maps.Equal(job.bidPricesPool, other.bidPricesPool) {
		return false
	}
	if !job.gangInfo.Equal(other.gangInfo) {
		return false
	}
	if !armadamaps.DeepEqual(job.runsById, other.runsById) {
		return false
	}
	return true
}

// Id returns the id of the Job.
func (job *Job) Id() string {
	return job.id
}

// Jobset returns the jobSet the job belongs to.
func (job *Job) Jobset() string {
	return job.jobSet
}

// Queue returns the queue this job belongs to.
func (job *Job) Queue() string {
	return job.queue
}

// Priority returns the priority of the job.
func (job *Job) Priority() uint32 {
	return job.priority
}

// PriorityClass returns the priority class of the job.
func (job *Job) PriorityClass() types.PriorityClass {
	return job.priorityClass
}

// SchedulingKey returns the scheduling key associated with a job.
func (job *Job) SchedulingKey() internaltypes.SchedulingKey {
	return job.schedulingKey
}

// SubmitTime exists for compatibility with the LegacyJob interface.
func (job *Job) SubmitTime() time.Time {
	if job.jobSchedulingInfo == nil {
		return time.Time{}
	}
	return job.jobSchedulingInfo.SubmitTime
}

// RequestedPriority returns the requested priority of the job.
func (job *Job) RequestedPriority() uint32 {
	return job.requestedPriority
}

// Pools returns the pools associated with the job
func (job *Job) Pools() []string {
	return slices.Clone(job.pools)
}

func (job *Job) WithBidPrices(bids map[string]pricing.Bid) *Job {
	j := shallowCopyJob(*job)
	j.bidPricesPool = maps.Clone(bids)
	return j
}

// GetBidPrice resolves the current bid price.
// It considers running, non-preemptible jobs to have an effectively infinite price.
func (job *Job) GetBidPrice(pool string) float64 {
	if !job.queued && !job.priorityClass.Preemptible {
		return pricing.NonPreemptibleRunningPrice
	}
	return job.getBidPrice(pool)
}

// GetRawBidPrice resolves the current bid price.
// It considers running, non-preemptible jobs to have the simple job running price (i.e. ignores premtibility pricing).
func (job *Job) GetRawBidPrice(pool string) float64 {
	return job.getBidPrice(pool)
}

func (job *Job) getBidPrice(pool string) float64 {
	bidPrice, present := job.bidPricesPool[pool]
	if !present {
		return 0
	}
	if job.queued {
		return bidPrice.QueuedBid
	}
	return bidPrice.RunningBid
}

// GetAllBidPrices
// This should not be used to determine the bid price, use GetBidPrice for an accurate price
// Expected this will only be used for testing purposes
func (job *Job) GetAllBidPrices() map[string]pricing.Bid {
	return maps.Clone(job.bidPricesPool)
}

func (job *Job) WithPriceBand(priceBand bidstore.PriceBand) *Job {
	j := shallowCopyJob(*job)
	j.priceBand = priceBand
	return j
}

func (job *Job) GetPriceBand() bidstore.PriceBand {
	return job.priceBand
}

func (job *Job) WithGangInfo(gangInfo GangInfo) *Job {
	j := shallowCopyJob(*job)
	j.gangInfo = gangInfo
	return j
}

func (job *Job) GetGangInfo() GangInfo {
	return job.gangInfo
}

func (job *Job) IsInGang() bool {
	return job.gangInfo.IsGang()
}

// WithPriority returns a copy of the job with the priority updated.
func (job *Job) WithPriority(priority uint32) *Job {
	j := shallowCopyJob(*job)
	j.priority = priority
	return j
}

// WithPools returns a copy of the job with the pools updated.
func (job *Job) WithPools(pools []string) *Job {
	j := shallowCopyJob(*job)
	j.pools = slices.Clone(pools)
	return j
}

// WithPriorityClass returns a copy of the job with the priority class updated.
func (job *Job) WithPriorityClass(priorityClass types.PriorityClass) *Job {
	j := shallowCopyJob(*job)
	j.priorityClass = priorityClass
	return j
}

// WithSubmittedTime returns a copy of the job with submittedTime updated.
func (job *Job) WithSubmittedTime(submittedTime int64) *Job {
	j := shallowCopyJob(*job)
	j.submittedTime = submittedTime
	return j
}

// WithRequestedPriority returns a copy of the job with the priority updated.
func (job *Job) WithRequestedPriority(priority uint32) *Job {
	j := shallowCopyJob(*job)
	j.requestedPriority = priority
	return j
}

// JobSchedulingInfo returns the scheduling requirements associated with the job
func (job *Job) JobSchedulingInfo() *internaltypes.JobSchedulingInfo {
	return job.jobSchedulingInfo
}

// Annotations returns the annotations on the job.
func (job *Job) Annotations() map[string]string {
	if req := job.PodRequirements(); req != nil {
		return req.Annotations
	}
	return nil
}

// PriorityClassName returns the name of the job's Priority Class
// TODO: this can be inconsistent with job.PriorityClass()
func (job *Job) PriorityClassName() string {
	if schedulingInfo := job.JobSchedulingInfo(); schedulingInfo != nil {
		return schedulingInfo.PriorityClassName()
	}
	return ""
}

// ScheduledAtPriority returns the numeric priority at which the job was scheduled
// This will return false if the job has not been scheduled yet
func (job *Job) ScheduledAtPriority() (int32, bool) {
	run := job.LatestRun()
	if run == nil {
		return -1, false
	}
	scheduledAtPriority := run.ScheduledAtPriority()
	if scheduledAtPriority == nil {
		return -1, false
	}
	return *scheduledAtPriority, true
}

// NodeSelector returns the Node Selector requested by the Job
func (job *Job) NodeSelector() map[string]string {
	if req := job.PodRequirements(); req != nil {
		return req.NodeSelector
	}
	return nil
}

// Affinity returns the Affinity requested by the Job
func (job *Job) Affinity() *v1.Affinity {
	if req := job.PodRequirements(); req != nil {
		return req.Affinity
	}
	return nil
}

// Tolerations returns the Tolerations requested by the Job
func (job *Job) Tolerations() []v1.Toleration {
	if req := job.PodRequirements(); req != nil {
		return req.Tolerations
	}
	return nil
}

// ResourceRequirements returns the resource requirements of the Job
// KubernetesResourceRequirements below is preferred
func (job *Job) resourceRequirements() v1.ResourceRequirements {
	if req := job.PodRequirements(); req != nil {
		return req.ResourceRequirements
	}
	return v1.ResourceRequirements{}
}

// All resource requirements, including floating resources, for this job
func (job *Job) AllResourceRequirements() internaltypes.ResourceList {
	return job.allResourceRequirements
}

// Kubernetes (i.e. non-floating) resource requirements of this job
func (job *Job) KubernetesResourceRequirements() internaltypes.ResourceList {
	return job.kubernetesResourceRequirements
}

// PodRequirements returns the pod requirements of the Job
func (job *Job) PodRequirements() *internaltypes.PodRequirements {
	return job.jobSchedulingInfo.PodRequirements
}

// Queued returns true if the job should be considered by the scheduler for assignment or false otherwise.
func (job *Job) Queued() bool {
	return job.queued
}

// WithQueued returns a copy of the job with the queued status updated.
func (job *Job) WithQueued(queued bool) *Job {
	j := shallowCopyJob(*job)
	j.queued = queued
	return j
}

// QueuedVersion returns current queued state version.
func (job *Job) QueuedVersion() int32 {
	return job.queuedVersion
}

// WithQueuedVersion returns a copy of the job with the queued version updated.
func (job *Job) WithQueuedVersion(version int32) *Job {
	j := shallowCopyJob(*job)
	j.queuedVersion = version
	return j
}

// CancelRequested returns true if the user has requested this job be cancelled.
func (job *Job) CancelRequested() bool {
	return job.cancelRequested
}

// CancelUser returns the first user who cancelled this job
func (job *Job) CancelUser() *string {
	return job.cancelUser
}

// CancelByJobsetRequested returns true if the user has requested this job's jobSet be cancelled.
func (job *Job) CancelByJobsetRequested() bool {
	return job.cancelByJobSetRequested
}

// WithCancelRequested returns a copy of the job with the cancelRequested status updated.
func (job *Job) WithCancelRequested(cancelRequested bool) *Job {
	j := shallowCopyJob(*job)
	j.cancelRequested = cancelRequested
	return j
}

// WithCancelByJobsetRequested returns a copy of the job with the cancelByJobSetRequested status updated.
func (job *Job) WithCancelByJobsetRequested(cancelByJobsetRequested bool) *Job {
	j := shallowCopyJob(*job)
	j.cancelByJobSetRequested = cancelByJobsetRequested
	return j
}

// WithCancelUser returns a copy of the job with the cancel user updated.
func (job *Job) WithCancelUser(cancelUser *string) *Job {
	j := shallowCopyJob(*job)
	j.cancelUser = cancelUser
	return j
}

// Cancelled Returns true if the scheduler has cancelled the job
func (job *Job) Cancelled() bool {
	return job.cancelled
}

// WithCancelled returns a copy of the job with the cancelled status updated
func (job *Job) WithCancelled(cancelled bool) *Job {
	j := shallowCopyJob(*job)
	j.cancelled = cancelled
	return j
}

// Succeeded Returns true if the scheduler has marked the job as succeeded
func (job *Job) Succeeded() bool {
	return job.succeeded
}

// WithSucceeded returns a copy of the job with the succeeded status updated.
func (job *Job) WithSucceeded(succeeded bool) *Job {
	j := shallowCopyJob(*job)
	j.succeeded = succeeded
	return j
}

// Failed Returns true if the scheduler has marked the job as failed
func (job *Job) Failed() bool {
	return job.failed
}

// WithFailed returns a copy of the job with the failed status updated.
func (job *Job) WithFailed(failed bool) *Job {
	j := shallowCopyJob(*job)
	j.failed = failed
	return j
}

func (job *Job) WithoutTerminal() *Job {
	j := shallowCopyJob(*job)
	j.cancelled = false
	j.failed = false
	j.succeeded = false
	return j
}

// Created Returns the creation time of the job
func (job *Job) Created() int64 {
	return job.submittedTime
}

// ActiveRunTimestamp returns the creation time of the most recent run associated with this job.
func (job *Job) ActiveRunTimestamp() int64 {
	return job.activeRunTimestamp
}

// The timestamp of the currently active run.

// InTerminalState returns true if the job  is in a terminal state
func (job *Job) InTerminalState() bool {
	return job.succeeded || job.cancelled || job.failed
}

// HasRuns returns true if the job has been run
// If this is returns true then LatestRun is guaranteed to return a non-nil value.
func (job *Job) HasRuns() bool {
	return job.activeRun != nil
}

func (job *Job) ValidateResourceRequests() error {
	pr := job.jobSchedulingInfo.PodRequirements
	if pr == nil {
		return nil
	}
	req := pr.ResourceRequirements.Requests
	if req == nil {
		return nil
	}

	_, err := job.jobDb.resourceListFactory.FromJobResourceListFailOnUnknown(adapters.K8sResourceListToMap(req))
	return err
}

// WithNewRun creates a copy of the job with a new run on the given executor.
func (job *Job) WithNewRun(executor, nodeId, nodeName, pool string, scheduledAtPriority int32) *Job {
	now := job.jobDb.clock.Now()
	return job.WithUpdatedRun(job.jobDb.CreateRun(
		job.jobDb.uuidProvider.New(),
		job.Id(),
		now.UnixNano(),
		executor,
		nodeId,
		nodeName,
		pool,
		&scheduledAtPriority,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		false,
		&now,
		nil,
		nil,
		nil,
		nil,
		false,
		false,
	))
}

// WithUpdatedRun returns a copy of the job with the provided run upserted.
func (job *Job) WithUpdatedRun(run *JobRun) *Job {
	j := shallowCopyJob(*job)
	if j.activeRun == nil || run.created >= j.activeRunTimestamp {
		j.activeRunTimestamp = run.created
		j.activeRun = run
	}
	if j.runsById != nil {
		j.runsById = maps.Clone(j.runsById)
		j.runsById[run.id] = run
	} else {
		j.runsById = map[string]*JobRun{run.id: run}
	}
	return j
}

// NumReturned returns the number of times this job has been returned by executors
// Note that this is O(N) on Runs, but this should be fine as the number of runs should be small.
func (job *Job) NumReturned() uint {
	returned := uint(0)
	for _, run := range job.runsById {
		if run.returned {
			returned++
		}
	}
	return returned
}

// NumAttempts returns the number of times the executors tried to run this job.
// Note that this is O(N) on Runs, but this should be fine as the number of runs should be small.
func (job *Job) NumAttempts() uint {
	attempts := uint(0)
	for _, run := range job.runsById {
		if run.runAttempted {
			attempts++
		}
	}
	return attempts
}

// AllRuns returns all runs associated with job.
func (job *Job) AllRuns() []*JobRun {
	return maps.Values(job.runsById)
}

// LatestRun returns the currently active job run or nil if there are no runs yet.
// Callers should either guard against nil values explicitly or via HasRuns.
func (job *Job) LatestRun() *JobRun {
	return job.activeRun
}

// ResolvedPools returns the:
//   - The pools that the job is capable of running on for queued jobs
//   - The pool the job has been leased to for non-queued jobs
func (job *Job) ResolvedPools() []string {
	if job.activeRun != nil && !job.queued {
		return []string{job.activeRun.Pool()}
	}
	return job.pools
}

// RunById returns the Run corresponding to the provided run id or nil if no such Run exists.
func (job *Job) RunById(id string) *JobRun {
	return job.runsById[id]
}

// WithJobset returns a copy of the job with the jobSet updated.
func (job *Job) WithJobset(jobset string) *Job {
	j := shallowCopyJob(*job)
	j.jobSet = jobset
	return j
}

// WithQueue returns a copy of the job with the queue updated.
func (job *Job) WithQueue(queue string) *Job {
	j := shallowCopyJob(*job)
	j.queue = queue
	return j
}

// WithCreated returns a copy of the job with the creation time updated.
func (job *Job) WithCreated(created int64) *Job {
	j := shallowCopyJob(*job)
	j.submittedTime = created
	return j
}

// WithValidated returns a copy of the job with the validated updated.
func (job *Job) WithValidated(validated bool) *Job {
	j := shallowCopyJob(*job)
	j.validated = validated
	return j
}

// Validated returns true if the job has been validated
func (job *Job) Validated() bool {
	return job.validated
}

// RequestsFloatingResources returns true if this job requests any floating resources
func (job *Job) RequestsFloatingResources() bool {
	return !job.AllResourceRequirements().OfType(internaltypes.Floating).AllZero()
}

// WithJobSchedulingInfo returns a copy of the job with the job scheduling info updated.
func (job *Job) WithJobSchedulingInfo(jobSchedulingInfo *internaltypes.JobSchedulingInfo) (*Job, error) {
	j := shallowCopyJob(*job)
	j.jobSchedulingInfo = jobSchedulingInfo
	j.ensureJobSchedulingInfoFieldsInitialised()

	// Changing the scheduling info invalidates the scheduling key stored with the job.
	j.schedulingKey = SchedulingKeyFromJob(j.jobDb.schedulingKeyGenerator, j)

	j.allResourceRequirements = j.jobDb.getResourceRequirements(jobSchedulingInfo)
	j.kubernetesResourceRequirements = j.allResourceRequirements.OfType(internaltypes.Kubernetes)

	return j, nil
}

func (job *Job) DeepCopy() *Job {
	j := shallowCopyJob(*job)
	j.jobSchedulingInfo = job.jobSchedulingInfo.DeepCopy()
	j.ensureJobSchedulingInfoFieldsInitialised()
	j.schedulingKey = SchedulingKeyFromJob(j.jobDb.schedulingKeyGenerator, j)

	j.runsById = maps.Clone(j.runsById)
	for key, run := range j.runsById {
		j.runsById[key] = run.DeepCopy()
	}
	if j.activeRun != nil {
		j.activeRun = job.activeRun.DeepCopy()
	}

	return j
}

func shallowCopyJob(j Job) *Job {
	return &j
}

func SchedulingKeyFromJob(skg *internaltypes.SchedulingKeyGenerator, job *Job) internaltypes.SchedulingKey {
	return skg.Key(
		job.NodeSelector(),
		job.Affinity(),
		job.Tolerations(),
		job.resourceRequirements().Requests,
		job.PriorityClassName(),
	)
}
