package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"signal-bot/pkg/models"
)

type Database struct {
	db *sql.DB
}

func New(path string) (*Database, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	d := &Database{db: db}
	if err := d.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return d, nil
}

func (d *Database) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS signals (
		id TEXT PRIMARY KEY,
		asset TEXT NOT NULL,
		direction TEXT NOT NULL,
		expiry INTEGER NOT NULL,
		amount REAL NOT NULL,
		confidence REAL NOT NULL,
		source TEXT,
		received_at DATETIME NOT NULL,
		processed_at DATETIME,
		raw TEXT
	);

	CREATE TABLE IF NOT EXISTS trades (
		id TEXT PRIMARY KEY,
		signal_id TEXT NOT NULL,
		asset TEXT NOT NULL,
		direction TEXT NOT NULL,
		amount REAL NOT NULL,
		expiry INTEGER NOT NULL,
		status TEXT NOT NULL,
		result TEXT,
		profit REAL,
		placed_at DATETIME NOT NULL,
		closed_at DATETIME,
		error_msg TEXT,
		FOREIGN KEY (signal_id) REFERENCES signals(id)
	);

	CREATE INDEX IF NOT EXISTS idx_signals_received ON signals(received_at);
	CREATE INDEX IF NOT EXISTS idx_trades_placed ON trades(placed_at);
	CREATE INDEX IF NOT EXISTS idx_trades_status ON trades(status);
	`

	_, err := d.db.Exec(schema)
	return err
}

func (d *Database) SaveSignal(signal *models.Signal) error {
	query := `
		INSERT OR REPLACE INTO signals (id, asset, direction, expiry, amount, confidence, source, received_at, processed_at, raw)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var processedAt interface{}
	if signal.ProcessedAt != nil {
		processedAt = signal.ProcessedAt
	}

	_, err := d.db.Exec(query,
		signal.ID,
		signal.Asset,
		signal.Direction,
		signal.Expiry,
		signal.Amount,
		signal.Confidence,
		signal.Source,
		signal.ReceivedAt,
		processedAt,
		signal.Raw,
	)

	return err
}

func (d *Database) SaveTrade(trade *models.Trade) error {
	query := `
		INSERT INTO trades (id, signal_id, asset, direction, amount, expiry, status, result, profit, placed_at, closed_at, error_msg)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var closedAt interface{}
	if trade.ClosedAt != nil {
		closedAt = trade.ClosedAt
	}

	_, err := d.db.Exec(query,
		trade.ID,
		trade.SignalID,
		trade.Asset,
		trade.Direction,
		trade.Amount,
		trade.Expiry,
		trade.Status,
		trade.Result,
		trade.Profit,
		trade.PlacedAt,
		closedAt,
		trade.ErrorMsg,
	)

	return err
}

func (d *Database) UpdateTrade(trade *models.Trade) error {
	query := `
		UPDATE trades 
		SET status = ?, result = ?, profit = ?, closed_at = ?, error_msg = ?
		WHERE id = ?
	`

	var closedAt interface{}
	if trade.ClosedAt != nil {
		closedAt = trade.ClosedAt
	}

	_, err := d.db.Exec(query,
		trade.Status,
		trade.Result,
		trade.Profit,
		closedAt,
		trade.ErrorMsg,
		trade.ID,
	)

	return err
}

func (d *Database) GetTodayStats() (*TradeStats, error) {
	query := `
		SELECT 
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN result = 'WIN' THEN 1 ELSE 0 END), 0) as wins,
			COALESCE(SUM(CASE WHEN result = 'LOSE' THEN 1 ELSE 0 END), 0) as losses,
			COALESCE(SUM(CASE WHEN profit < 0 THEN ABS(profit) ELSE 0 END), 0) as total_loss
		FROM trades
		WHERE DATE(placed_at) = DATE('now')
		  AND status != 'FAILED'
	`

	var stats TradeStats
	err := d.db.QueryRow(query).Scan(
		&stats.Total,
		&stats.Wins,
		&stats.Losses,
		&stats.TotalProfit,
	)
	return &stats, err
}

func (d *Database) Close() error {
	return d.db.Close()
}

type TradeStats struct {
	Total       int
	Wins        int
	Losses      int
	WinRate     float64
	TotalProfit float64
}
