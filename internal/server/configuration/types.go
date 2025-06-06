package configuration

import (
	"time"

	"github.com/redis/go-redis/v9"
	v1 "k8s.io/api/core/v1"

	authconfig "github.com/armadaproject/armada/internal/common/auth/configuration"
	commonconfig "github.com/armadaproject/armada/internal/common/config"
	grpcconfig "github.com/armadaproject/armada/internal/common/grpc/configuration"
	profilingconfig "github.com/armadaproject/armada/internal/common/profiling/configuration"
	armadaresource "github.com/armadaproject/armada/internal/common/resource"
	"github.com/armadaproject/armada/pkg/client"
)

type ArmadaConfig struct {
	Auth authconfig.AuthConfig

	GrpcPort    uint16
	HttpPort    uint16
	MetricsPort uint16
	Profiling   *profilingconfig.ProfilingConfig

	CorsAllowedOrigins []string
	GrpcGatewayPath    string

	Grpc grpcconfig.GrpcConfig

	SchedulerApiConnection client.ApiConnectionDetails

	EventsApiRedis redis.UniversalOptions
	Pulsar         commonconfig.PulsarConfig
	Postgres       PostgresConfig // Needs to point to the lookout db
	QueryApi       QueryApiConfig

	// Period At which the Queue cache will be refreshed
	QueueCacheRefreshPeriod time.Duration

	// Config relating to job submission.
	Submission SubmissionConfig
}

// SubmissionConfig contains config relating to job submission.
type SubmissionConfig struct {
	// The priorityClassName field on submitted pod must be either empty or in this list.
	// These names should correspond to priority classes defined in schedulingConfig.
	AllowedPriorityClassNames map[string]bool
	// Priority class name assigned to pods that do not specify one.
	// Must be an entry in PriorityClasses above.
	DefaultPriorityClassName string
	// Default job resource limits added to pods.
	DefaultJobLimits armadaresource.ComputeResources
	// Tolerations added to all submitted pods.
	DefaultJobTolerations []v1.Toleration
	// Tolerations added to all submitted pods of a given priority class.
	DefaultJobTolerationsByPriorityClass map[string][]v1.Toleration
	// Tolerations added to all submitted pods requesting a non-zero amount of some resource.
	DefaultJobTolerationsByResourceRequest map[string][]v1.Toleration
	// Tolerations that cannot be user-set.  Jobs submitted with these tolerations will be rejected
	RestrictedTolerationKeys []string
	// Pods of size greater than this are rejected at submission.
	MaxPodSpecSizeBytes uint
	// Jobs requesting less than this amount of resources are rejected at submission.
	MinJobResources v1.ResourceList
	// Default value of GangNodeUniformityLabelAnnotation if not set on submitted jobs.
	DefaultGangNodeUniformityLabel string
	// Minimum allowed termination grace period for pods submitted to Armada.
	// Should normally be set to a positive value, e.g., "10m".
	// Since a zero grace period causes Kubernetes to force delete pods, which may causes issues with container resource cleanup.
	//
	// The grace period of pods that either
	// - do not set a grace period, or
	// - explicitly set a grace period of 0 seconds,
	// is automatically set to MinTerminationGracePeriod.
	MinTerminationGracePeriod time.Duration
	// Max allowed grace period.
	// Should normally not be set greater than single-digit minutes,
	// since cancellation and preemption may need to wait for this amount of time.
	MaxTerminationGracePeriod time.Duration
	// Default activeDeadline for all pods that don't explicitly set activeDeadlineSeconds.
	// Is trumped by DefaultActiveDeadlineByResourceRequest.
	DefaultActiveDeadline time.Duration
	// Default activeDeadline for pods with at least one container requesting a given resource.
	// For example, if
	// DefaultActiveDeadlineByResourceRequest: map[string]time.Duration{"gpu": time.Second},
	// then all pods requesting a non-zero amount of gpu and don't explicitly set activeDeadlineSeconds
	// will have activeDeadlineSeconds set to 1.
	// Trumps DefaultActiveDeadline.
	DefaultActiveDeadlineByResourceRequest map[string]time.Duration
	// Maximum ratio of limits:requests per resource. Jobs who have a higher limits:resource ratio than this will be rejected.
	// Any resource type missing from this map will default to 1.0.
	MaxOversubscriptionByResourceRequest map[string]float64
	// Enforce that an init containers requestion non-integer cpu. This is due to https://github.com/kubernetes/kubernetes/issues/112228
	AssertInitContainersRequestFractionalCpu bool
	// Controls whether we add the gang id annotation as a label.
	AddGangIdLabel bool
	// Controls whether custom service names are allowed
	AllowCustomServiceNames bool
}

// TODO: we can probably just typedef this to map[string]string
type PostgresConfig struct {
	Connection map[string]string
}

type QueryApiConfig struct {
	MaxQueryItems int
}
