package store

import (
	"context"
)

// cleanupApiKeys bounds each cleanup batch and never locks a parent afterwards.
func (s *Store) cleanupApiKeys(ctx context.Context) error {
	ended := s.DB.NewSelect().Model((*apiKeyRecord)(nil)).Column("ak.id").
		Where("LEAST(ak.expires_at, ak.revoked_at) < clock_timestamp() - interval '2160 hours'").
		Where("ak.expires_at IS NOT NULL OR ak.revoked_at IS NOT NULL").OrderExpr("ak.id").Limit(5000).For("UPDATE SKIP LOCKED")
	if _, err := s.DB.NewDelete().Model((*apiKeyRecord)(nil)).Where("id IN (?)", ended).Exec(ctx); err != nil {
		return err
	}
	expired := s.DB.NewSelect().Model((*apiIdempotencyRequest)(nil)).Column("ir.id").Where("ir.expires_at <= clock_timestamp()").OrderExpr("ir.id").Limit(5000).For("UPDATE SKIP LOCKED")
	_, err := s.DB.NewDelete().Model((*apiIdempotencyRequest)(nil)).Where("id IN (?)", expired).Exec(ctx)
	return err
}
