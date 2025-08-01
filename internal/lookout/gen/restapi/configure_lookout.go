// This file is safe to edit. Once it exists it will not be overwritten

package restapi

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"golang.org/x/exp/slices"

	"github.com/armadaproject/armada/internal/common/auth"
	log "github.com/armadaproject/armada/internal/common/logging"
	"github.com/armadaproject/armada/internal/common/serve"
	"github.com/armadaproject/armada/internal/lookout/configuration"
	"github.com/armadaproject/armada/internal/lookout/gen/restapi/operations"
	"github.com/armadaproject/armada/internal/lookout/metrics"
)

//go:generate swagger generate server --target ../../gen --name Lookout --spec ../../swagger.yaml --principal interface{} --exclude-main

var corsAllowedOrigins []string

func SetCorsAllowedOrigins(allowedOrigins []string) {
	corsAllowedOrigins = allowedOrigins
}

var authService auth.AuthService

func SetAuthService(s auth.AuthService) {
	authService = s
}

func configureFlags(api *operations.LookoutAPI) {
	// api.CommandLineOptionsGroups = []swag.CommandLineOptionsGroup{ ... }
}

func configureAPI(api *operations.LookoutAPI) http.Handler {
	// configure the api here
	api.ServeError = errors.ServeError

	// Set your custom logger if needed. Default one is log.Printf
	// Expected interface func(string, ...interface{})
	//
	// Example:
	// api.Logger = log.Printf

	api.UseSwaggerUI()
	// To continue using redoc as your UI, uncomment the following line
	// api.UseRedoc()

	api.JSONConsumer = runtime.JSONConsumer()

	api.JSONProducer = runtime.JSONProducer()

	if api.GetJobsHandler == nil {
		api.GetJobsHandler = operations.GetJobsHandlerFunc(func(params operations.GetJobsParams) middleware.Responder {
			return middleware.NotImplemented("operation operations.GetJobs has not yet been implemented")
		})
	}
	if api.GroupJobsHandler == nil {
		api.GroupJobsHandler = operations.GroupJobsHandlerFunc(func(params operations.GroupJobsParams) middleware.Responder {
			return middleware.NotImplemented("operation operations.GroupJobs has not yet been implemented")
		})
	}

	api.PreServerShutdown = func() {}

	api.ServerShutdown = func() {}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

// The TLS configuration before HTTPS server starts.
func configureTLS(tlsConfig *tls.Config) {
	// Make all necessary changes to the TLS configuration here.
}

// As soon as server is initialized but not run yet, this function will be called.
// If you need to modify a config, store server instance to stop it individually later, this is the place.
// This function can be called multiple times, depending on the number of serving schemes.
// scheme value will be set accordingly: "http", "https" or "unix".
func configureServer(s *http.Server, scheme, addr string) {
}

// The middleware configuration is for the handler executors. These do not apply to the swagger.json document.
// The middleware executes after routing but before authentication, binding and validation.
func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

var UIConfig configuration.UIConfig

// The middleware configuration happens before anything, this middleware also applies to serving the swagger.json document.
// So this is a good place to plug in a panic handling middleware, logging and metrics.
func setupGlobalMiddleware(apiHandler http.Handler) http.Handler {
	return allowCORS(
		uiHandler(
			authHandler(
				recordRequestDuration(
					apiHandler,
				),
			),
		), corsAllowedOrigins)
}

func authHandler(handler http.Handler) http.Handler {
	mux := http.NewServeMux()

	// do not authenticate requests to healthchecker endpoint
	mux.Handle("/health", handler)

	authFunction := auth.CreateHttpMiddlewareAuthFunction(authService)
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxWithPrincipal, err := authFunction(w, r)
		if err != nil {
			return
		}

		handler.ServeHTTP(w, r.WithContext(ctxWithPrincipal))
	}))

	return mux
}

func uiHandler(apiHandler http.Handler) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/", serve.SinglePageApplicationHandler(http.Dir("./internal/lookoutui/build")))

	mux.HandleFunc("/lookout-ui-config.js", func(w http.ResponseWriter, _ *http.Request) {
		lookoutUiConfigJsonB, err := json.Marshal(UIConfig)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			if _, err := w.Write([]byte("Unable to encode UI Config to JSON")); err != nil {
				log.WithError(err).Error("error writing JSON encoding error for /lookout-ui-config.js")
			}
			return
		}

		w.Header().Set("Content-Type", "application/javascript")
		if _, err := w.Write([]byte(fmt.Sprintf("window.__LOOKOUT_UI_CONFIG__ = %s", lookoutUiConfigJsonB))); err != nil {
			log.WithError(err).Error("error writing response for /lookout-ui-config.js")
		}
	})

	mux.Handle("/api/", apiHandler)
	mux.Handle("/health", apiHandler)

	return mux
}

func allowCORS(handler http.Handler, corsAllowedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && slices.Contains(corsAllowedOrigins, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if r.Method == "OPTIONS" && r.Header.Get("Access-Control-Request-Method") != "" {
				preflightHandler(w)
				return
			}
		}
		handler.ServeHTTP(w, r)
	})
}

func recordRequestDuration(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		handler.ServeHTTP(w, r)
		duration := time.Since(start)
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			metrics.RecordRequestDuration(auth.GetPrincipal(r.Context()).GetName(), r.URL.Path, float64(duration.Milliseconds()))
		}
	})
}

func preflightHandler(w http.ResponseWriter) {
	headers := []string{"Content-Type", "Accept", "Authorization"}
	w.Header().Set("Access-Control-Allow-Headers", strings.Join(headers, ","))
	methods := []string{"GET", "HEAD", "POST", "PUT", "DELETE"}
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ","))
}
