package database

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"strings"
	"time"
)

// Open connects Bun through pgx's database/sql driver. Bun escapes arguments into the SQL
// text itself, so pgx sends every query over PostgreSQL's simple protocol. Every
// connection's TimeZone is UTC, whatever the server's default or the URL says, so
// date arithmetic (an interval in days, date_trunc) and the times PostgreSQL prints
// are the same on every server.
func Open(ctx context.Context, url string) (*bun.DB, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	for name := range cfg.RuntimeParams {
		if strings.EqualFold(name, "timezone") {
			delete(cfg.RuntimeParams, name)
		}
	}
	cfg.RuntimeParams["timezone"] = "UTC"
	sqldb := stdlib.OpenDB(*cfg)
	sqldb.SetMaxOpenConns(10)
	sqldb.SetMaxIdleConns(10)
	sqldb.SetConnMaxLifetime(time.Hour)
	sqldb.SetConnMaxIdleTime(30 * time.Minute)
	db := bun.NewDB(sqldb, pgdialect.New())
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
