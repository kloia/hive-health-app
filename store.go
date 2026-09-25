package main

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	errNotFound          = errors.New("not found")
	errInsufficientStock = errors.New("insufficient stock")
)

type Product struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	Warehouse string    `json:"warehouse"`
	Quantity  int       `json:"quantity"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Movement struct {
	ID        int       `json:"id"`
	ProductID int       `json:"product_id"`
	Delta     int       `json:"delta"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type Report struct {
	ID            int       `json:"id"`
	GeneratedAt   time.Time `json:"generated_at"`
	TotalProducts int       `json:"total_products"`
	TotalQuantity int       `json:"total_quantity"`
}

// productOrder is a closed set of sort orders, so ORDER BY never contains
// caller-supplied text.
type productOrder int

const (
	orderByID productOrder = iota
	orderByWarehouse
)

func (o productOrder) sql() string {
	if o == orderByWarehouse {
		return "warehouse, name"
	}
	return "id"
}

type store interface {
	Ping(ctx context.Context) error
	ListProducts(ctx context.Context, order productOrder) ([]Product, error)
	GetProduct(ctx context.Context, id int) (Product, error)
	ApplyMovement(ctx context.Context, m Movement) (Movement, error)
	ListMovements(ctx context.Context, limit int) ([]Movement, error)
	ListReports(ctx context.Context, limit int) ([]Report, error)
}

type pgStore struct {
	db *sql.DB
}

func (s *pgStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *pgStore) ListProducts(ctx context.Context, order productOrder) ([]Product, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id, name, warehouse, quantity, updated_at FROM products ORDER BY "+order.sql())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := []Product{}
	for rows.Next() {
		var p Product
		if err := rows.Scan(&p.ID, &p.Name, &p.Warehouse, &p.Quantity, &p.UpdatedAt); err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return products, nil
}

func (s *pgStore) GetProduct(ctx context.Context, id int) (Product, error) {
	var p Product
	err := s.db.QueryRowContext(ctx, "SELECT id, name, warehouse, quantity, updated_at FROM products WHERE id = $1", id).
		Scan(&p.ID, &p.Name, &p.Warehouse, &p.Quantity, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, errNotFound
	}
	return p, err
}

// ApplyMovement changes a product's quantity and records the movement in one
// transaction. It refuses movements that would make the quantity negative.
func (s *pgStore) ApplyMovement(ctx context.Context, m Movement) (Movement, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return m, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		"UPDATE products SET quantity = quantity + $1, updated_at = NOW() WHERE id = $2 AND quantity + $1 >= 0",
		m.Delta, m.ProductID)
	if err != nil {
		return m, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return m, err
	}
	if n == 0 {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM products WHERE id = $1)", m.ProductID).Scan(&exists); err != nil {
			return m, err
		}
		if !exists {
			return m, errNotFound
		}
		return m, errInsufficientStock
	}

	err = tx.QueryRowContext(ctx,
		"INSERT INTO movements (product_id, delta, note, created_at) VALUES ($1, $2, $3, NOW()) RETURNING id, created_at",
		m.ProductID, m.Delta, m.Note).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return m, err
	}
	return m, tx.Commit()
}

func (s *pgStore) ListMovements(ctx context.Context, limit int) ([]Movement, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, product_id, delta, COALESCE(note, ''), created_at FROM movements ORDER BY id DESC LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	movements := []Movement{}
	for rows.Next() {
		var m Movement
		if err := rows.Scan(&m.ID, &m.ProductID, &m.Delta, &m.Note, &m.CreatedAt); err != nil {
			return nil, err
		}
		movements = append(movements, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return movements, nil
}

func (s *pgStore) ListReports(ctx context.Context, limit int) ([]Report, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, generated_at, total_products, total_quantity FROM daily_reports ORDER BY generated_at DESC LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reports := []Report{}
	for rows.Next() {
		var r Report
		if err := rows.Scan(&r.ID, &r.GeneratedAt, &r.TotalProducts, &r.TotalQuantity); err != nil {
			return nil, err
		}
		reports = append(reports, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reports, nil
}
