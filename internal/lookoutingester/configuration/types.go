package configuration

import (
	"time"

	log "github.com/armadaproject/armada/internal/common/logging"

	commonconfig "github.com/armadaproject/armada/internal/common/config"
	profilingconfig "github.com/armadaproject/armada/internal/common/profiling/configuration"
	"github.com/armadaproject/armada/internal/server/configuration"
)

type LookoutIngesterConfiguration struct {
	// Database configuration
	Postgres configuration.PostgresConfig
	// Metrics configuration
	MetricsPort uint16
	// General Pulsar configuration
	Pulsar commonconfig.PulsarConfig
	// Pulsar subscription name
	SubscriptionName string
	// Size in bytes above which job specs will be compressed when inserting in the database
	MinJobSpecCompressionSize int
	// Number of event messages that will be batched together before being inserted into the database
	BatchSize int
	// Maximum time since the last batch before a batch will be inserted into the database
	BatchDuration time.Duration
	// Time for which the pulsar consumer will wait for a new message before retrying
	PulsarReceiveTimeout time.Duration
	// Time for which the pulsar consumer will back off after receiving an error on trying to receive a message
	PulsarBackoffTime time.Duration
	// Deprecated: use Annotations.UserAnnotationPrefix instead
	// User annotations have a common prefix to avoid clashes with other annotations.  This prefix will be stripped from
	// The annotation before storing in the db
	UserAnnotationPrefix string

	// Annotations settings
	Annotations struct {
		// High cardinality annotations which should be removed from annotations before storing them in the db
		BlocklistAnnotations []string
		// User annotations have a common prefix to avoid clashes with other annotations.  This prefix will be stripped from
		// The annotation before storing in the db
		UserAnnotationPrefix string
	}
	// Between each attempt to store data in the database, there is an exponential backoff (starting out as 1s).
	// MaxBackoff caps this backoff to whatever it is specified (in seconds)
	MaxBackoff int
	// If non-nil, configures pprof profiling
	Profiling *profilingconfig.ProfilingConfig
	// List of Regexes which will identify fatal errors when inserting into postgres
	FatalInsertionErrors []string
}

func (c *LookoutIngesterConfiguration) GetUserAnnotationPrefix() string {
	if c.Annotations.UserAnnotationPrefix != "" {
		return c.Annotations.UserAnnotationPrefix
	}
	if c.UserAnnotationPrefix != "" {
		log.Warnf("Warning: UserAnnotationPrefix is deprecated; use Annotations.UserAnnotationPrefix instead")
		return c.UserAnnotationPrefix
	}
	return ""
}
