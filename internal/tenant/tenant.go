package tenant

import (
	"context"
	"errors"
	"net/http"

	"github.com/neok/streaming/internal/catalog"
)

type ctxKey struct{}

type Tenant struct {
	ID   int64
	Slug string
}

type Resolver interface {
	TenantIDBySlug(ctx context.Context, slug string) (int64, error)
}

func WithTenant(ctx context.Context, t Tenant) context.Context {
	return context.WithValue(ctx, ctxKey{}, t)
}

func From(ctx context.Context) (Tenant, bool) {
	t, ok := ctx.Value(ctxKey{}).(Tenant)
	return t, ok
}

func Middleware(r Resolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			slug := req.PathValue("slug")
			if slug == "" {
				http.NotFound(w, req)
				return
			}
			id, err := r.TenantIDBySlug(req.Context(), slug)
			if err != nil {
				if errors.Is(err, catalog.ErrNotFound) {
					http.NotFound(w, req)
					return
				}
				http.Error(w, "internal", http.StatusInternalServerError)
				return
			}
			ctx := WithTenant(req.Context(), Tenant{ID: id, Slug: slug})
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	}
}
