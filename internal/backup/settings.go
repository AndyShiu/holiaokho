package backup

import (
	"context"
	"encoding/json"

	"github.com/holiaokho/holiaokho/internal/db"
)

// Settings is the scheduled-backup configuration. It lives in the settings
// table so it can be edited from the UI; the config file seeds it on first
// start and acts as the fallback while it is unset.
type Settings struct {
	Enabled   bool   `json:"enabled"`
	Dir       string `json:"dir"`
	WithBlobs bool   `json:"withBlobs"`
	Keep      int    `json:"keep"`
	// Cron, when set, overrides the default daily schedule.
	Cron string `json:"cron"`
}

// Load reads the stored settings, falling back to the supplied config-file
// values when nothing has been saved yet.
func Load(ctx context.Context, d *db.DB, fallback Settings) Settings {
	var raw []byte
	if err := d.Pool.QueryRow(ctx, `SELECT value FROM settings WHERE key='backup'`).Scan(&raw); err != nil {
		return fallback
	}
	v := fallback
	if err := json.Unmarshal(raw, &v); err != nil {
		return fallback
	}
	if v.Keep <= 0 {
		v.Keep = 7
	}
	return v
}

// Save stores the settings.
func Save(ctx context.Context, d *db.DB, v Settings) error {
	if v.Keep <= 0 {
		v.Keep = 7
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = d.Pool.Exec(ctx, `INSERT INTO settings(key,value) VALUES ('backup',$1) ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value`, raw)
	return err
}
