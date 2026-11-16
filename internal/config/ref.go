package config

import (
	"errors"
	"fmt"
	"strings"
)

// Ref errors.
var (
	ErrRefNotFound  = errors.New("config: account not found")
	ErrRefAmbiguous = errors.New("config: account reference is ambiguous")
)

// ParseRef resolves "company/account", or a bare "account" id when it is
// unique across companies.
func (cfg Config) ParseRef(s string) (AccountRef, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return AccountRef{}, fmt.Errorf("%w: empty reference", ErrRefNotFound)
	}
	if comp, id, ok := strings.Cut(s, "/"); ok {
		c, _, found := cfg.FindCompany(comp)
		if !found {
			return AccountRef{}, fmt.Errorf("%w: %s", ErrRefNotFound, s)
		}
		a, _, found := c.FindAccount(id)
		if !found {
			return AccountRef{}, fmt.Errorf("%w: %s", ErrRefNotFound, s)
		}
		return AccountRef{Company: c, Account: a}, nil
	}
	var hits []AccountRef
	for _, r := range cfg.All() {
		if r.Account.ID == s {
			hits = append(hits, r)
		}
	}
	switch len(hits) {
	case 0:
		return AccountRef{}, fmt.Errorf("%w: %s", ErrRefNotFound, s)
	case 1:
		return hits[0], nil
	}
	var names []string
	for _, h := range hits {
		names = append(names, h.String())
	}
	return AccountRef{}, fmt.Errorf("%w: %s (%s)", ErrRefAmbiguous, s, strings.Join(names, ", "))
}
