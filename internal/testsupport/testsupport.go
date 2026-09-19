// Package testsupport 提供测试用数据库：每个包拿到一个独立、干净、已迁移的库，
// 使 go test ./... 并行执行时各包互不干扰。
package testsupport

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"

	"cc-053/internal/database"
)

// adminDSN 连到维护库（postgres），用于 create/drop database。
func adminDSN() string {
	if d := os.Getenv("TEST_ADMIN_DSN"); d != "" {
		return d
	}
	return "host=/tmp port=55432 user=postgres dbname=postgres sslmode=disable"
}

func targetDSN(dbName string) string {
	if d := os.Getenv("TEST_DSN"); d != "" {
		return d
	}
	return fmt.Sprintf("host=/tmp port=55432 user=postgres dbname=%s sslmode=disable timezone=UTC", dbName)
}

// OpenFresh 重建并返回一个名为 dt_<name> 的、已完成迁移的数据库连接。
func OpenFresh(name string) (*sql.DB, error) {
	dbName := "dt_" + name

	admin, err := sql.Open("postgres", adminDSN())
	if err != nil {
		return nil, err
	}
	defer admin.Close()
	if _, err = admin.Exec(
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		 WHERE datname=$1 AND pid <> pg_backend_pid()`, dbName); err != nil {
		return nil, fmt.Errorf("terminate conns: %w", err)
	}
	if _, err = admin.Exec(`DROP DATABASE IF EXISTS ` + dbName); err != nil {
		return nil, fmt.Errorf("drop db: %w", err)
	}
	if _, err = admin.Exec(`CREATE DATABASE ` + dbName); err != nil {
		return nil, fmt.Errorf("create db: %w", err)
	}

	db, err := sql.Open("postgres", targetDSN(dbName))
	if err != nil {
		return nil, err
	}
	if err = db.Ping(); err != nil {
		return nil, err
	}
	if err = database.RunMigrations(db); err != nil {
		return nil, err
	}
	return db, nil
}
