package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"shortlink.local/control/internal/proxycheck"
	"shortlink.local/control/internal/security"
	"shortlink.local/policy"
	"strings"
)

type ProxySettings struct {
	Provider     string `json:"provider"`
	BaseURL      string `json:"baseUrl"`
	Revision     int64  `json:"revision"`
	Enabled      bool   `json:"enabled"`
	HasKey       bool   `json:"hasKey"`
	DailyBudget  int    `json:"dailyBudget"`
	QueriesToday int    `json:"queriesToday"`
}

func (s *Store) ProxySettings(ctx context.Context) (ProxySettings, error) {
	var v ProxySettings
	err := s.DB.QueryRow(ctx, `SELECT provider,base_url,revision,enabled,api_key IS NOT NULL,daily_budget,coalesce((SELECT queries FROM proxycheck_usage WHERE day=(now() AT TIME ZONE 'UTC')::date),0) FROM proxycheck_settings`).Scan(&v.Provider, &v.BaseURL, &v.Revision, &v.Enabled, &v.HasKey, &v.DailyBudget, &v.QueriesToday)
	return v, err
}
func (s *Store) ProxyConfig(ctx context.Context) (proxycheck.Config, error) {
	var v proxycheck.Config
	var sealed []byte
	err := s.DB.QueryRow(ctx, "SELECT revision,enabled,api_key,provider,base_url FROM proxycheck_settings").Scan(&v.Revision, &v.Enabled, &sealed, &v.Provider, &v.BaseURL)
	if err != nil || !v.Enabled {
		return v, err
	}
	seed, err := s.Seed(ctx)
	if err != nil {
		return v, err
	}
	key, err := security.OpenSecret(seed, "proxycheck-api-key", sealed)
	if err != nil {
		return proxycheck.Config{}, err
	}
	v.Key = string(key)
	return v, nil
}
func (s *Store) UpdateProxySettings(ctx context.Context, expected int64, enabled bool, budget int, key string, remove bool, actor string) error {
	return s.UpdateIPQualitySettings(ctx, expected, enabled, budget, key, remove, actor, "", "")
}
func (s *Store) UpdateIPQualitySettings(ctx context.Context, expected int64, enabled bool, budget int, key string, remove bool, actor, provider, baseURL string) error {
	key = strings.TrimSpace(key)
	if len(key) > 256 || strings.ContainsAny(key, "\r\n\t ") || (remove && key != "") {
		return ErrPolicy
	}
	var sealed []byte
	if key != "" {
		seed, err := s.Seed(ctx)
		if err != nil {
			return err
		}
		sealed, err = security.Seal(seed, "proxycheck-api-key", []byte(key))
		if err != nil {
			return err
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var revision int64
	var old []byte
	var oldProvider, oldURL string
	var oldBudget int
	if err = tx.QueryRow(ctx, "SELECT revision,api_key,provider,base_url,daily_budget FROM proxycheck_settings FOR UPDATE").Scan(&revision, &old, &oldProvider, &oldURL, &oldBudget); err != nil {
		return err
	}
	if revision != expected {
		return ErrConflict
	}
	if provider == "" {
		provider = oldProvider
		if baseURL == "" {
			baseURL = oldURL
		}
	}
	if provider != "beon" {
		return ErrPolicy
	}
	budget = oldBudget // Historical column; daily quotas are no longer enforced.
	var ok bool
	baseURL, ok = proxycheck.ValidateBaseURL(baseURL)
	if !ok && (enabled || baseURL != "") {
		return ErrPolicy
	}
	// A change of recipient requires an explicit replacement key.
	if (provider != oldProvider || baseURL != oldURL) && key == "" {
		old = nil
	}
	if remove {
		old = nil
	} else if key != "" {
		old = sealed
	}
	if enabled && old == nil {
		return ErrPolicy
	}
	if _, err = tx.Exec(ctx, "UPDATE proxycheck_settings SET revision=revision+1,enabled=$1,daily_budget=$2,api_key=$3,provider=$4,base_url=$5,updated_at=now()", enabled, budget, old, provider, baseURL); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO audit_logs(actor_id,action) VALUES($1,'proxycheck_settings_updated')", actor); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s *Store) ReserveProxyQuery(ctx context.Context, revision int64) (bool, error) {
	var n int
	err := s.DB.QueryRow(ctx, `INSERT INTO proxycheck_usage(day,queries)
 SELECT (now() AT TIME ZONE 'UTC')::date,1 FROM proxycheck_settings WHERE enabled AND revision=$1
 ON CONFLICT(day) DO UPDATE SET queries=proxycheck_usage.queries+1
 WHERE EXISTS(SELECT 1 FROM proxycheck_settings WHERE enabled AND revision=$1
 AND provider='beon')
 RETURNING queries`, revision).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) LinkUsesProxy(ctx context.Context, domain, slug string) (bool, error) {
	var use bool
	err := s.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM links WHERE domain_id=$1 AND slug=$2 AND state='published' AND NOT disabled AND deleted_at IS NULL AND routing->>'proxyCheck'='true')", domain, slug).Scan(&use)
	return use, err
}

func requiredRoutingVersion(r *policy.LinkRouting) int {
	if r != nil && r.ProxyCheck && r.IPQualityBlock != nil {
		return 4
	}
	if r != nil && r.ProxyCheck {
		return 2
	}
	return 1
}
