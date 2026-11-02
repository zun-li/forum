package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"forum/internal/config"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenMySQL 根据传入配置创建并检查数据库连接池。
func OpenMySQL(cfg config.MySQLConfig) (*gorm.DB, error) {
	dsnConfig := drivermysql.NewConfig()
	dsnConfig.User = cfg.User
	dsnConfig.Passwd = cfg.Password
	dsnConfig.Net = "tcp"
	dsnConfig.Addr = cfg.Addr
	dsnConfig.DBName = cfg.Database
	dsnConfig.ParseTime = true
	dsnConfig.Loc = time.UTC
	dsnConfig.Params = map[string]string{"charset": "utf8mb4"}
	dsnConfig.Timeout = 5 * time.Second
	dsnConfig.ReadTimeout = 5 * time.Second
	dsnConfig.WriteTimeout = 5 * time.Second

	// SQL 日志可能包含密码哈希；由上层记录脱敏后的错误。
	db, err := gorm.Open(mysql.Open(dsnConfig.FormatDSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open MySQL: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get connection pool: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping MySQL: %w", err)
	}

	return db, nil
}

// CreateMissingTables creates tables in the supplied foreign-key dependency order.
func CreateMissingTables(ctx context.Context, db *gorm.DB, models ...any) error {
	migrator := db.WithContext(ctx).Migrator()
	for _, model := range models {
		if migrator.HasTable(model) {
			continue
		}
		if err := migrator.CreateTable(model); err != nil {
			// Another instance may have created this table since HasTable.
			var mysqlErr *drivermysql.MySQLError
			if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1050 {
				return fmt.Errorf("create table for %T: %w", model, err)
			}
		}
	}
	return nil
}
