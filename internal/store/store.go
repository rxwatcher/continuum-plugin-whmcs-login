package store

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginrt "github.com/RXWatcher/silo-plugin-whmcs-login/internal/runtime"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS app_config (
	id INTEGER PRIMARY KEY DEFAULT 1,
	data JSONB NOT NULL DEFAULT '{}'::jsonb,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	CONSTRAINT app_config_singleton CHECK (id = 1)
);
INSERT INTO app_config (id, data) VALUES (1, '{}'::jsonb) ON CONFLICT (id) DO NOTHING;
`)
	return err
}

func DefaultConfig() pluginrt.Config {
	return pluginrt.Config{DiscordIDCustomField: "Discord ID"}
}

func (s *Store) GetConfig(ctx context.Context) (pluginrt.Config, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT data FROM app_config WHERE id = 1`).Scan(&raw)
	if err == pgx.ErrNoRows {
		if _, err := s.pool.Exec(ctx, `INSERT INTO app_config (id, data) VALUES (1, '{}'::jsonb) ON CONFLICT (id) DO NOTHING`); err != nil {
			return pluginrt.Config{}, fmt.Errorf("ensure app_config: %w", err)
		}
		return s.GetConfig(ctx)
	}
	if err != nil {
		return pluginrt.Config{}, fmt.Errorf("get app_config: %w", err)
	}
	cfg := DefaultConfig()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return pluginrt.Config{}, fmt.Errorf("decode app_config: %w", err)
		}
	}
	if cfg.DiscordIDCustomField == "" {
		cfg.DiscordIDCustomField = "Discord ID"
	}
	return cfg, nil
}

func (s *Store) UpdateConfig(ctx context.Context, cfg pluginrt.Config) error {
	cfg.DatabaseURL = ""
	if cfg.DiscordIDCustomField == "" {
		cfg.DiscordIDCustomField = "Discord ID"
	}
	if err := pluginrt.ValidateConfig(&cfg); err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode app_config: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO app_config (id, data, updated_at) VALUES (1, $1, NOW())
		ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()
	`, raw)
	if err != nil {
		return fmt.Errorf("update app_config: %w", err)
	}
	return nil
}

func (s *Store) ImportLegacyConfig(ctx context.Context, legacy pluginrt.Config) (pluginrt.Config, error) {
	current, err := s.GetConfig(ctx)
	if err != nil {
		return pluginrt.Config{}, err
	}
	if !reflect.DeepEqual(current, DefaultConfig()) {
		return current, nil
	}
	legacy.DatabaseURL = ""
	if legacy.DiscordIDCustomField == "" {
		legacy.DiscordIDCustomField = "Discord ID"
	}
	if reflect.DeepEqual(legacy, current) {
		return current, nil
	}
	if err := s.UpdateConfig(ctx, legacy); err != nil {
		return pluginrt.Config{}, err
	}
	return s.GetConfig(ctx)
}
