package jobdb

import (
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"
	"golang.org/x/exp/maps"

	armadamath "github.com/armadaproject/armada/internal/common/math"
	armadaslices "github.com/armadaproject/armada/internal/common/slices"
	"github.com/armadaproject/armada/internal/scheduler/database"
	"github.com/armadaproject/armada/internal/scheduler/internaltypes"
	"github.com/armadaproject/armada/internal/scheduler/schedulerobjects"
	"github.com/armadaproject/armada/pkg/api"
)

// JobStateTransitions captures the process of updating a job.
// It bundles the updated job with booleans indicating which state transitions were applied to produce it.
// These are cumulative in the sense that a job with transitions queued -> scheduled -> queued -> running -> failed
// will have the fields queued, scheduled, running, and failed set to true.
type JobStateTransitions struct {
	Job *Job

	Queued              bool
	Leased              bool
	Pending             bool
	Running             bool
	Cancelled           bool
	PreemptionRequested bool
	Preempted           bool
	Failed              bool
	Succeeded           bool
}

// applyRunStateTransitions applies the state transitions of a run to that of the associated job.
func (jst JobStateTransitions) applyRunStateTransitions(rst RunStateTransitions) JobStateTransitions {
	jst.Queued = jst.Queued || rst.Returned
	jst.Leased = jst.Leased || rst.Leased
	jst.Pending = jst.Pending || rst.Pending
	jst.Running = jst.Running || rst.Running
	jst.Cancelled = jst.Cancelled || rst.Cancelled
	jst.PreemptionRequested = jst.PreemptionRequested || rst.PreemptionRequested
	jst.Preempted = jst.Preempted || rst.Preempted
	jst.Failed = jst.Failed || rst.Failed
	jst.Succeeded = jst.Succeeded || rst.Succeeded
	return jst
}

// RunStateTransitions captures the process of updating a run.
// It works in the same way as JobStateTransitions does for jobs.
type RunStateTransitions struct {
	JobRun *JobRun

	Leased              bool
	Returned            bool
	Pending             bool
	Running             bool
	Cancelled           bool
	PreemptionRequested bool
	Preempted           bool
	Failed              bool
	Succeeded           bool
}

// ReconcileDifferences reconciles any differences between jobs stored in the jobDb with those provided to this function
// and returns the updated jobs together with a summary of the state transitions applied to those jobs.
func (jobDb *JobDb) ReconcileDifferences(txn *Txn, jobRepoJobs []database.Job, jobRepoRuns []database.Run) ([]JobStateTransitions, error) {
	// Map jobs for which a run was updated to nil and jobs updated directly to the updated job.
	jobRepoJobsById := make(map[string]*database.Job, armadamath.Max(len(jobRepoJobs), len(jobRepoRuns)))
	for _, jobRepoRun := range jobRepoRuns {
		jobRepoJobsById[jobRepoRun.JobID] = nil
	}
	for _, jobRepoJob := range jobRepoJobs {
		jobRepoJob := jobRepoJob
		jobRepoJobsById[jobRepoJob.JobID] = &jobRepoJob
	}

	// Group updated runs by the id of the job they're associated with.
	jobRepoRunsById := armadaslices.MapAndGroupByFuncs(
		jobRepoRuns,
		func(jobRepoRun database.Run) string { return jobRepoRun.JobID },
		func(jobRepoRun database.Run) *database.Run { return &jobRepoRun },
	)

	jsts := make(map[string]JobStateTransitions, len(jobRepoJobsById))
	jobIdsToMarkAsPreemptionRequested := []string{}

	for jobId, jobRepoJob := range jobRepoJobsById {
		job := txn.GetById(jobId)
		jst, err := jobDb.reconcileJobDifferences(
			job,                    // Existing job in the jobDb.
			jobRepoJob,             // New or updated job from the jobRepo.
			jobRepoRunsById[jobId], // New or updated runs associated with this job from the jobRepo.
		)
		if err != nil {
			return nil, err
		}
		if jst.PreemptionRequested && job.IsInGang() {
			jobsInGang := txn.GetGangJobsIdsByGangId(job.Queue(), job.GetGangInfo().Id())
			jobIdsToMarkAsPreemptionRequested = append(jobIdsToMarkAsPreemptionRequested, jobsInGang...)
		}

		// We receive nil jobs from jobDb.ReconcileDifferences if a run is updated after the associated job is deleted.
		// In this case it is safe to ignore the jst.
		// TODO: don't generate a jst in the first place if this is the case!
		if jst.Job != nil {
			jsts[jobId] = jst
		}
	}
	markJobsAsPreemptionRequested(txn, jobIdsToMarkAsPreemptionRequested, jsts)
	return maps.Values(jsts), nil
}

func markJobsAsPreemptionRequested(txn *Txn, jobIds []string, jsts map[string]JobStateTransitions) {
	for _, jobId := range jobIds {
		jst, exists := jsts[jobId]
		if !exists {
			job := txn.GetById(jobId)
			if job == nil {
				continue
			}
			jst = JobStateTransitions{
				Job: job,
			}
		}
		if jst.PreemptionRequested {
			continue
		} else {
			jobRun := jst.Job.LatestRun()
			if jobRun != nil {
				jobRun = jobRun.WithPreemptRequested(true)
			}
			jst.Job = jst.Job.WithUpdatedRun(jobRun)
			jst.PreemptionRequested = true
		}
		jsts[jobId] = jst
	}
}

// reconcileJobDifferences takes as its arguments for some job id
// - the job currently stored in the jobDb, or nil, if there is no such job,
// - the job stored in the job repository, or nil if there is no such job,
// - a slice composed of the runs associated with the job stored in the job repository,
// and returns a new jobdb.Job produced by reconciling any differences between the input jobs
// along with a summary of the state transitions applied to the job.
//
// TODO(albin): Pending, running, and preempted are not supported yet.
func (jobDb *JobDb) reconcileJobDifferences(job *Job, jobRepoJob *database.Job, jobRepoRuns []*database.Run) (jst JobStateTransitions, err error) {
	defer func() { jst.Job = job }()
	if job == nil && jobRepoJob == nil {
		return
	} else if job == nil && jobRepoJob != nil {
		if job, err = jobDb.schedulerJobFromDatabaseJob(jobRepoJob); err != nil {
			return
		}
		jst.Queued = true
	} else if job != nil && jobRepoJob == nil {
		// No direct updates to the job; just process any updated runs below.
	} else if job != nil && jobRepoJob != nil {
		if jobRepoJob.Validated && !job.Validated() {
			job = job.WithValidated(true).WithPools(jobRepoJob.Pools)
		}
		if jobRepoJob.CancelRequested && !job.CancelRequested() {
			job = job.WithCancelRequested(true)
		}
		if jobRepoJob.CancelByJobsetRequested && !job.CancelByJobsetRequested() {
			job = job.WithCancelByJobsetRequested(true)
		}
		if jobRepoJob.CancelUser != nil && jobRepoJob.CancelUser != job.CancelUser() {
			job = job.WithCancelUser(jobRepoJob.CancelUser)
		}
		if jobRepoJob.Cancelled && !job.Cancelled() {
			job = job.WithCancelled(true)
		}
		if jobRepoJob.Succeeded && !job.Succeeded() {
			job = job.WithSucceeded(true)
		}
		if jobRepoJob.Failed && !job.Failed() {
			job = job.WithFailed(true)
		}
		if uint32(jobRepoJob.Priority) != job.RequestedPriority() {
			job = job.WithRequestedPriority(uint32(jobRepoJob.Priority))
		}
		if uint32(jobRepoJob.SchedulingInfoVersion) > job.JobSchedulingInfo().Version {
			schedulingInfoProto := &schedulerobjects.JobSchedulingInfo{}
			if err = proto.Unmarshal(jobRepoJob.SchedulingInfo, schedulingInfoProto); err != nil {
				err = errors.Wrapf(err, "error unmarshalling scheduling info for job %s", jobRepoJob.JobID)
				return jst, err
			}
			schedulingInfo, err := internaltypes.FromSchedulerObjectsJobSchedulingInfo(schedulingInfoProto)
			if err != nil {
				err = errors.Wrapf(err, "error converting scheduler info for job %s", jobRepoJob.JobID)
				return jst, err
			}
			job, err = job.WithJobSchedulingInfo(schedulingInfo)
			if err != nil {
				err = errors.Wrapf(err, "error unmarshalling scheduling info for job %s", jobRepoJob.JobID)
				return jst, err
			}
		}
		if jobRepoJob.QueuedVersion > job.QueuedVersion() {
			job = job.WithQueuedVersion(jobRepoJob.QueuedVersion)
			job = job.WithQueued(jobRepoJob.Queued)
		}
	}

	// Reconcile run state transitions.
	for _, jobRepoRun := range jobRepoRuns {
		rst := jobDb.reconcileRunDifferences(job.RunById(jobRepoRun.RunID), jobRepoRun)
		jst = jst.applyRunStateTransitions(rst)
		job = job.WithUpdatedRun(rst.JobRun)
	}

	return
}

func (jobDb *JobDb) reconcileRunDifferences(jobRun *JobRun, jobRepoRun *database.Run) (rst RunStateTransitions) {
	defer func() { rst.JobRun = jobRun }()
	if jobRun == nil && jobRepoRun == nil {
		return
	} else if jobRun == nil && jobRepoRun != nil {
		jobRun = jobDb.schedulerRunFromDatabaseRun(jobRepoRun)
		rst.Returned = jobRepoRun.Returned
		rst.Pending = jobRepoRun.Pending
		rst.Leased = jobRepoRun.LeasedTimestamp != nil
		rst.Running = jobRepoRun.Running
		rst.Preempted = jobRepoRun.Preempted
		rst.Cancelled = jobRepoRun.Cancelled
		rst.Failed = jobRepoRun.Failed
		rst.Succeeded = jobRepoRun.Succeeded
	} else if jobRun != nil && jobRepoRun == nil {
		return
	} else if jobRun != nil && jobRepoRun != nil {
		if jobRepoRun.LeasedTimestamp != nil && !jobRun.Leased() {
			jobRun = jobRun.WithLeased(true).WithLeasedTime(jobRepoRun.LeasedTimestamp)
			rst.Leased = true
		}
		if jobRepoRun.Pending && !jobRun.Pending() {
			jobRun = jobRun.WithPending(true).WithPendingTime(jobRepoRun.PendingTimestamp)
			rst.Pending = true
		}
		if jobRepoRun.Running && !jobRun.Running() {
			jobRun = jobRun.WithRunning(true).WithRunningTime(jobRepoRun.RunningTimestamp)
			rst.Running = true
		}
		if jobRepoRun.PreemptRequested && !jobRun.PreemptRequested() {
			jobRun = jobRun.WithPreemptRequested(true)
			rst.PreemptionRequested = true
		}
		if jobRepoRun.Preempted && !jobRun.Preempted() {
			jobRun = jobRun.WithPreempted(true).WithRunning(false).WithPreemptedTime(jobRepoRun.PreemptedTimestamp)
			rst.Preempted = true
		}
		if jobRepoRun.Cancelled && !jobRun.Cancelled() {
			jobRun = jobRun.WithCancelled(true).WithRunning(false).WithTerminatedTime(jobRepoRun.TerminatedTimestamp)
			rst.Cancelled = true
		}
		if jobRepoRun.Failed && !jobRun.Failed() {
			jobRun = jobRun.WithFailed(true).WithRunning(false).WithTerminatedTime(jobRepoRun.TerminatedTimestamp)
			rst.Failed = true
		}
		if jobRepoRun.Succeeded && !jobRun.Succeeded() {
			jobRun = jobRun.WithSucceeded(true).WithRunning(false).WithTerminatedTime(jobRepoRun.TerminatedTimestamp)
			rst.Succeeded = true
		}
		if jobRepoRun.Returned && !jobRun.Returned() {
			jobRun = jobRun.WithReturned(true).WithRunning(false)
			rst.Returned = true
		}
		if jobRepoRun.RunAttempted && !jobRun.RunAttempted() {
			jobRun = jobRun.WithAttempted(true)
		}
	}
	jobRun = jobDb.enforceTerminalStateExclusivity(jobRun, &rst)
	return
}

// enforceTerminalStateExclusivity ensures that a job run has a single terminal state regardless of what the database reports.
// terminal states are: preempted, cancelled, failed, and succeeded.
func (jobDb *JobDb) enforceTerminalStateExclusivity(jobRun *JobRun, rst *RunStateTransitions) *JobRun {
	if jobRun.Succeeded() {
		rst.Preempted, rst.Cancelled, rst.Failed, rst.Succeeded = false, false, false, true
		return jobRun.WithoutTerminal().WithSucceeded(true)
	}
	if jobRun.Failed() {
		rst.Preempted, rst.Cancelled, rst.Succeeded, rst.Failed = false, false, false, true
		return jobRun.WithoutTerminal().WithFailed(true)
	}
	if jobRun.Cancelled() {
		rst.Preempted, rst.Failed, rst.Succeeded, rst.Cancelled = false, false, false, true
		return jobRun.WithoutTerminal().WithCancelled(true)
	}
	if jobRun.Preempted() {
		rst.Cancelled, rst.Failed, rst.Succeeded, rst.Preempted = false, false, false, true
		return jobRun.WithoutTerminal().WithPreempted(true)
	}
	return jobRun
}

// schedulerJobFromDatabaseJob creates a new scheduler job from a database job.
func (jobDb *JobDb) schedulerJobFromDatabaseJob(dbJob *database.Job) (*Job, error) {
	schedulingInfoProto := &schedulerobjects.JobSchedulingInfo{}
	if err := proto.Unmarshal(dbJob.SchedulingInfo, schedulingInfoProto); err != nil {
		return nil, errors.WithMessagef(err, "error unmarshalling scheduling info for job %s", dbJob.JobID)
	}

	schedulingInfo, err := internaltypes.FromSchedulerObjectsJobSchedulingInfo(schedulingInfoProto)
	if err != nil {
		return nil, errors.WithMessagef(err, "error converting scheduling info for job %s", dbJob.JobID)
	}

	job, err := jobDb.NewJob(
		dbJob.JobID,
		dbJob.JobSet,
		dbJob.Queue,
		uint32(dbJob.Priority),
		schedulingInfo,
		dbJob.Queued,
		dbJob.QueuedVersion,
		dbJob.CancelRequested,
		dbJob.CancelByJobsetRequested,
		dbJob.Cancelled,
		dbJob.Submitted,
		dbJob.Validated,
		dbJob.Pools,
		dbJob.PriceBand,
	)
	if err != nil {
		return nil, err
	}

	if dbJob.Failed {
		// TODO(albin): Let's make this an argument to NewJob. Even better: have the state as an enum argument.
		job = job.WithFailed(dbJob.Failed)
	}
	if dbJob.Succeeded {
		// TODO(albin): Same comment as the above.
		job = job.WithSucceeded(dbJob.Succeeded)
	}
	if uint32(dbJob.Priority) != job.RequestedPriority() {
		// TODO(albin): Same comment as the above.
		job = job.WithRequestedPriority(uint32(dbJob.Priority))
	}
	return job, nil
}

// schedulerRunFromDatabaseRun creates a new scheduler job run from a database job run
func (jobDb *JobDb) schedulerRunFromDatabaseRun(dbRun *database.Run) *JobRun {
	nodeId := api.NodeIdFromExecutorAndNodeName(dbRun.Executor, dbRun.Node)
	return jobDb.CreateRun(
		dbRun.RunID,
		dbRun.JobID,
		dbRun.Created,
		dbRun.Executor,
		nodeId,
		dbRun.Node,
		dbRun.Pool,
		dbRun.ScheduledAtPriority,
		dbRun.LeasedTimestamp != nil,
		dbRun.Pending,
		dbRun.Running,
		dbRun.PreemptRequested,
		dbRun.Preempted,
		dbRun.Succeeded,
		dbRun.Failed,
		dbRun.Cancelled,
		dbRun.LeasedTimestamp,
		dbRun.PendingTimestamp,
		dbRun.RunningTimestamp,
		dbRun.PreemptedTimestamp,
		dbRun.TerminatedTimestamp,
		dbRun.Returned,
		dbRun.RunAttempted,
	)
}
