// Package mysqltest provides isolated MySQL databases for integration tests.
package mysqltest

import (
	"fmt"
	"os"
	"testing"
	"time"

	"forum/internal/database"

	driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Open creates a database for one test and drops it during cleanup. The DSN
// is opt-in and must identify a test server with permission to create databases.
func Open(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN for an isolated MySQL instance")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	open := func() *gorm.DB {
		db, err := gorm.Open(mysql.Open(cfg.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			t.Fatal(err)
		}
		pool, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pool.Close() })
		return db
	}
	admin := open()
	name := fmt.Sprintf("forum_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	if err := admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := admin.Exec("DROP DATABASE " + name).Error; err != nil {
			t.Errorf("drop temporary test database: %v", err)
		}
	})
	cfg.DBName = name
	db := open()
	if err := database.CreateMissingTables(t.Context(), db, models...); err != nil {
		t.Fatal(err)
	}
	return db
}
