// Package system composes the optional broker and bounded AI application on the
// existing engine/transport. It is not required for ordinary proxy operation.
package system

import (
	"context"
	"errors"
	"net/http"

	"github.com/NotRllyRn/proxykit/ai"
	"github.com/NotRllyRn/proxykit/examples/aihost"
	"github.com/NotRllyRn/proxykit/examples/brokerapp"
	"github.com/NotRllyRn/proxykit/extension"
)

// Config is trusted deployment configuration, immutable after New. Identity must
// recheck current authorization and return stable tenant/conversation ownership.
// Routes is an explicit encoding/account catalog, not model-generated endpoints.
// Broker's live account ACL/endpoint/credential checks remain authoritative.
type Config struct {
	Host     brokerapp.HostConfig
	App      *aihost.App
	Routes   []ai.Route
	Identity func(context.Context, string) (ai.Identity, error)
}

type targetKey struct{}

// New transfers Host's application listeners even on validation failure. The
// gateway listener remains caller-owned. Close the gateway before Host.Close.
func New(b *brokerapp.Broker, cfg Config) (*brokerapp.Host, error) {
	invalid := func() (*brokerapp.Host, error) {
		if cfg.Host.Catalog != nil {
			_ = cfg.Host.Catalog.Close()
		}
		if cfg.Host.Enrollment != nil {
			_ = cfg.Host.Enrollment.Close()
		}
		return nil, errors.New("invalid system composition")
	}
	if cfg.App == nil || cfg.App.Recorder == nil || cfg.App.Recorder.Sink == nil || cfg.Identity == nil || len(cfg.Routes) == 0 || cfg.Host.Select != nil || cfg.Host.Ingress != nil || cfg.App.BindTarget != nil {
		return invalid()
	}
	catalog := map[ai.Route]bool{}
	// Exact route identity permits shared endpoints only with an explicit account
	// and protocol carried through the private request-local target binding.
	for _, route := range cfg.Routes {
		if route.Account == "" || route.Protocol == "" || route.Endpoint == "" || route.Provider == "" || route.Model == "" || route.Version == "" || catalog[route] {
			return invalid()
		}
		p := cfg.App.Protocols[route.Protocol]
		if p == nil || p.Name() != route.Protocol {
			return invalid()
		}
		catalog[route] = true
	}
	if !catalog[cfg.App.Target] {
		return invalid()
	}
	app := *cfg.App
	recorder := *app.Recorder
	app.Recorder = &recorder
	authorize := func(ctx context.Context, auth extension.Authentication) error {
		id, err := cfg.Identity(ctx, auth.Subject)
		if err != nil || auth.Subject == "" || id.Caller != auth.Subject || id.Tenant == "" || id.Conversation == "" || auth.Claims["tenant"] != id.Tenant || auth.Claims["conversation"] != id.Conversation {
			return brokerapp.ErrDenied
		}
		return nil
	}
	app.BindTarget = func(ctx context.Context, route ai.Route) (context.Context, error) {
		if !catalog[route] {
			return ctx, brokerapp.ErrDenied
		}
		return context.WithValue(ctx, targetKey{}, route), nil
	}
	cfg.Host.Select = func(ctx context.Context, in extension.Input) (brokerapp.Selection, error) {
		route, ok := ctx.Value(targetKey{}).(ai.Route)
		if !ok || !catalog[route] || route.Endpoint != in.Request.URL || authorize(ctx, in.Auth) != nil {
			return brokerapp.Selection{}, brokerapp.ErrDenied
		}
		return brokerapp.Selection{AccountID: route.Account, Protocol: route.Protocol}, nil
	}
	recorder.EvidenceContext = func(ctx context.Context, in extension.Input) (ai.Route, error) {
		evidence, err := brokerapp.RouteEvidence(ctx, in)
		if err != nil {
			return ai.Route{}, err
		}
		route, ok := ctx.Value(targetKey{}).(ai.Route)
		if !ok || evidence.AccountID != route.Account || evidence.Protocol != route.Protocol || evidence.Endpoint != route.Endpoint || evidence.Provider != route.Provider {
			return ai.Route{}, brokerapp.ErrDenied
		}
		// Model is replaced with actual response model by HostManaged, not inferred
		// from either the requested model or broker's descriptive account metadata.
		return ai.Route{Account: evidence.AccountID, Provider: evidence.Provider, Endpoint: evidence.Endpoint, Protocol: evidence.Protocol, Version: route.Version}, nil
	}
	coordinate := app.Registration()
	for _, r := range cfg.Host.Registrations {
		// Request policies must actually run before the owned root short circuit.
		// Later stages would protect children but silently miss the root operation.
		if r.Stage <= extension.Upstream {
			before := false
			for _, id := range r.Before {
				if id == coordinate.ID {
					before = true
				}
			}
			if r.Stage != extension.Prepare || !before || r.Failure != extension.Block {
				return invalid()
			}
		}
	}
	cfg.Host.Registrations = append(append([]extension.Registration(nil), cfg.Host.Registrations...), coordinate)
	cfg.Host.Registrations = append(cfg.Host.Registrations, recorder.Registrations()...)
	cfg.Host.Ingress = func(next http.Handler) http.Handler {
		managed := app.Handler(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth, ok := extension.AuthenticationFromContext(r.Context())
			id, err := cfg.Identity(r.Context(), auth.Subject)
			if !ok || err != nil || id.Caller != auth.Subject || id.Tenant == "" || id.Conversation == "" {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			auth.Claims = map[string]string{"tenant": id.Tenant, "conversation": id.Conversation}
			managed.ServeHTTP(w, r.WithContext(extension.WithAuthentication(r.Context(), auth)))
		})
	}
	return brokerapp.NewHost(b, cfg.Host)
}
