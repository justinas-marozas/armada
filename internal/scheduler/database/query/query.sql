-- name: SelectNewJobs :many
SELECT * FROM jobs WHERE serial > $1 ORDER BY serial LIMIT $2;

-- name: SelectAllJobIds :many
SELECT job_id FROM jobs;

-- name: SelectMaxJobSerial :one
SELECT serial FROM jobs ORDER BY serial DESC LIMIT 1;

-- name: SelectMaxRunSerial :one
SELECT serial FROM runs ORDER BY serial DESC LIMIT 1;

-- name: SelectInitialJobs :many
SELECT job_id, job_set, queue, priority, submitted, queued, queued_version, validated, cancel_requested, cancel_user, cancel_by_jobset_requested, cancelled, succeeded, failed, scheduling_info, scheduling_info_version, pools, price_band, serial FROM jobs WHERE serial > $1 AND cancelled = 'false' AND succeeded = 'false' and failed = 'false' ORDER BY serial LIMIT $2;

-- name: SelectUpdatedJobs :many
SELECT job_id, job_set, queue, priority, submitted, queued, queued_version, validated, cancel_requested, cancel_user, cancel_by_jobset_requested, cancelled, succeeded, failed, scheduling_info, scheduling_info_version, pools, price_band, serial FROM jobs WHERE serial > $1 ORDER BY serial LIMIT $2;

-- name: UpdateJobPriorityByJobSet :exec
UPDATE jobs SET priority = $1 WHERE job_set = $2 and queue = $3 and cancelled = false and succeeded = false and failed = false;

-- name: MarkJobsCancelRequestedBySetAndQueuedState :exec
UPDATE jobs SET cancel_by_jobset_requested = true, cancel_user = $1 WHERE job_set = sqlc.arg(job_set) and queue = sqlc.arg(queue) and queued = ANY(sqlc.arg(queued_states)::bool[]) and cancelled = false and succeeded = false and failed = false;

-- name: MarkJobsSucceededById :exec
UPDATE jobs SET succeeded = true WHERE job_id = ANY(sqlc.arg(job_ids)::text[]);

-- name: MarkJobsCancelRequestedById :exec
UPDATE jobs SET cancel_requested = true, cancel_user = $1 WHERE queue = sqlc.arg(queue) and job_set = sqlc.arg(job_set) and job_id = ANY(sqlc.arg(job_ids)::text[]) and cancelled = false and succeeded = false and failed = false;

-- name: MarkJobsCancelledById :exec
UPDATE jobs SET cancelled = true WHERE job_id = ANY(sqlc.arg(job_ids)::text[]);

-- name: MarkJobsFailedById :exec
UPDATE jobs SET failed = true WHERE job_id = ANY(sqlc.arg(job_ids)::text[]);

-- name: UpdateJobPriorityById :exec
UPDATE jobs SET priority = $1 WHERE queue = sqlc.arg(queue) and job_set = sqlc.arg(job_set) and job_id = ANY(sqlc.arg(job_ids)::text[]) and cancelled = false and succeeded = false and failed = false;

-- name: SelectInitialRuns :many
SELECT * FROM runs WHERE serial > $1 AND job_id = ANY(sqlc.arg(job_ids)::text[]) ORDER BY serial LIMIT $2;

-- name: SelectNewRuns :many
SELECT * FROM runs WHERE serial > $1 ORDER BY serial LIMIT $2;

-- name: SelectAllRunIds :many
SELECT run_id FROM runs;

-- name: SelectNewRunsForJobs :many
SELECT * FROM runs WHERE serial > $1 AND job_id = ANY(sqlc.arg(job_ids)::text[]) ORDER BY serial;

-- name: MarkJobRunsPreemptRequestedByJobId :exec
UPDATE runs SET preempt_requested = true WHERE queue = sqlc.arg(queue) and job_set = sqlc.arg(job_set) and job_id = ANY(sqlc.arg(job_ids)::text[]) and cancelled = false and succeeded = false and failed = false;

-- name: MarkJobRunsSucceededById :exec
UPDATE runs SET succeeded = true WHERE run_id = ANY(sqlc.arg(run_ids)::text[]);

-- name: MarkJobRunsFailedById :exec
UPDATE runs SET failed = true WHERE run_id = ANY(sqlc.arg(run_ids)::text[]);

-- name: MarkJobRunsReturnedById :exec
UPDATE runs SET returned = true WHERE run_id = ANY(sqlc.arg(run_ids)::text[]);

-- name: MarkJobRunsAttemptedById :exec
UPDATE runs SET run_attempted = true WHERE run_id = ANY(sqlc.arg(run_ids)::text[]);

-- name: MarkJobRunsRunningById :exec
UPDATE runs SET running = true WHERE run_id = ANY(sqlc.arg(run_ids)::text[]);

-- name: MarkRunsCancelledByJobId :exec
UPDATE runs SET cancelled = true WHERE job_id = ANY(sqlc.arg(job_ids)::text[]);

-- name: SelectJobsForExecutor :many
SELECT jr.run_id, j.queue, j.job_set, j.user_id, j.groups, j.submit_message
FROM runs jr
         JOIN jobs j
              ON jr.job_id = j.job_id
WHERE jr.executor = $1
  AND jr.run_id NOT IN (sqlc.arg(run_ids)::text[])
  AND jr.succeeded = false AND jr.failed = false AND jr.cancelled = false;

-- name: FindActiveRuns :many
SELECT run_id FROM runs WHERE run_id = ANY(sqlc.arg(run_ids)::text[])
                         AND (succeeded = false AND failed = false AND cancelled = false);

-- name: CountGroup :one
SELECT COUNT(*) FROM markers WHERE group_id= $1;

-- name: DeleteOldMarkers :exec
DELETE FROM markers WHERE created < sqlc.arg(cutoff)::timestamptz;

-- name: SelectAllMarkers :many
SELECT * FROM markers;

-- name: InsertMarker :exec
INSERT INTO markers (group_id, partition_id, created) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING;

-- Run errors
-- name: SelectRunErrorsById :many
SELECT * FROM job_run_errors WHERE run_id = ANY(sqlc.arg(run_ids)::text[]);

-- name: SelectAllRunErrors :many
SELECT * FROM job_run_errors;

-- name: SelectAllExecutors :many
SELECT * FROM executors;

-- name: SelectExecutorUpdateTimes :many
SELECT executor_id, last_updated FROM executors;

-- name: UpsertExecutor :exec
INSERT INTO executors (executor_id, last_request, last_updated)
VALUES(sqlc.arg(executor_id)::text, sqlc.arg(last_request)::bytea, sqlc.arg(update_time)::timestamptz)
ON CONFLICT (executor_id) DO UPDATE SET (last_request, last_updated) = (excluded.last_request,excluded.last_updated);

-- name: SetLeasedTime :exec
UPDATE runs SET leased_timestamp = $1 WHERE run_id = $2;

-- name: SetPendingTime :exec
UPDATE runs SET pending_timestamp = $1 WHERE run_id = $2;

-- name: SetRunningTime :exec
UPDATE runs SET running_timestamp = $1 WHERE run_id = $2;

-- name: SetTerminatedTime :exec
UPDATE runs SET terminated_timestamp = $1 WHERE run_id = $2;

-- name: UpsertExecutorSettings :exec
INSERT INTO executor_settings (executor_id, cordoned, cordon_reason, set_by_user, set_at_time)
VALUES (@executor_id::text, @cordoned::boolean, @cordon_reason::text, @set_by_user::text, @set_at_time::timestamptz)
ON CONFLICT (executor_id) DO UPDATE
  SET
    cordoned = excluded.cordoned,
    cordon_reason = excluded.cordon_reason,
    set_by_user = excluded.set_by_user,
    set_at_time = excluded.set_at_time;

-- name: DeleteExecutorSettings :exec
DELETE FROM executor_settings WHERE executor_id = @executor_id::text;

-- name: SelectAllExecutorSettings :many
SELECT executor_id, cordoned, cordon_reason, set_by_user, set_at_time FROM executor_settings;

-- name: SelectLatestJobSerial :one
SELECT serial FROM jobs ORDER BY serial DESC LIMIT 1;

-- name: SelectLatestJobRunSerial :one
SELECT serial FROM runs ORDER BY serial DESC LIMIT 1;

-- name: SelectJobsByExecutorAndQueues :many
SELECT j.*
FROM runs jr
       JOIN jobs j
            ON jr.job_id = j.job_id
WHERE jr.executor = @executor
  AND j.queue = ANY(@queues::text[])
  AND jr.succeeded = false AND jr.failed = false AND jr.cancelled = false AND jr.preempted = false;

-- name: SelectQueuedJobsByQueue :many
SELECT j.*
FROM jobs j
WHERE j.queue = ANY(@queue::text[])
  AND j.queued = true;

-- name: SelectLeasedJobsByQueue :many
SELECT j.*
FROM runs jr
       JOIN jobs j
            ON jr.job_id = j.job_id
WHERE j.queue = ANY(@queue::text[])
  AND jr.running = false
  AND jr.pending = false
  AND jr.succeeded = false
  AND jr.failed = false
  AND jr.cancelled = false
  AND jr.preempted = false;

-- name: SelectPendingJobsByQueue :many
SELECT j.*
FROM runs jr
       JOIN jobs j
            ON jr.job_id = j.job_id
WHERE j.queue = ANY(@queue::text[])
  AND jr.running = false
  AND jr.pending = true
  AND jr.succeeded = false
  AND jr.failed = false
  AND jr.cancelled = false
  AND jr.preempted = false;

-- name: SelectRunningJobsByQueue :many
SELECT j.*
FROM runs jr
       JOIN jobs j
            ON jr.job_id = j.job_id
WHERE j.queue = ANY(@queue::text[])
  AND jr.running = true
  AND jr.returned = false
  AND jr.succeeded = false
  AND jr.failed = false
  AND jr.cancelled = false
  AND jr.preempted = false;

