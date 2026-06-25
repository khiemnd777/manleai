package public_catalog

import (
	"context"
	"errors"
	"strings"
)

var ErrNotFound = errors.New("public catalog not found")

type Store interface {
	GetBySlug(ctx context.Context, slug string) (*Catalog, error)
	GetFirstPublished(ctx context.Context) (*Catalog, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) GetBySlug(ctx context.Context, slug string) (*Catalog, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !validSlug(slug) {
		return nil, ErrNotFound
	}
	return s.store.GetBySlug(ctx, slug)
}

func (s *Service) GetFirstPublished(ctx context.Context) (*Catalog, error) {
	return s.store.GetFirstPublished(ctx)
}

func validSlug(slug string) bool {
	if len(slug) < 3 || len(slug) > 64 {
		return false
	}
	if slug[0] == '-' || slug[len(slug)-1] == '-' {
		return false
	}
	previousHyphen := false
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
			previousHyphen = false
		case r >= '0' && r <= '9':
			previousHyphen = false
		case r == '-':
			if previousHyphen {
				return false
			}
			previousHyphen = true
		default:
			return false
		}
	}
	return true
}
