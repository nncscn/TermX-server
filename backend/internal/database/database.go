// 连接由 main 装配后注入 repository
package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ky/internal/config"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

func Init(cfg *config.Config) (*gorm.DB, error) {
	return Open(cfg.Database.Driver, cfg.Database.DSN)
}

func Open(driver, dsn string) (*gorm.DB, error) {
	switch driver {
	case "mysql":
		return gorm.Open(mysql.Open(dsn), &gorm.Config{})
	case "postgresql":
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})
	case "sqlserver":
		return gorm.Open(sqlserver.Open(dsn), &gorm.Config{})
	case "sqlite":
		if dir := filepath.Dir(dsn); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("创建数据目录: %w", err)
			}
		}
		return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	default:
		return nil, fmt.Errorf("不支持的数据库驱动: %s（可选 sqlite / mysql / postgresql / sqlserver）", driver)
	}
}

func TestConnection(driver, dsn string, timeout time.Duration) error {
	db, err := Open(driver, withConnectTimeout(driver, dsn, timeout))
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return sqlDB.PingContext(ctx)
}

// withConnectTimeout 把拨号超时写进各驱动 DSN，避免驱动层长时间挂起
func withConnectTimeout(driver, dsn string, timeout time.Duration) string {
	sec := int(timeout.Seconds())
	switch driver {
	case "mysql":
		if strings.Contains(dsn, "?") {
			return dsn + fmt.Sprintf("&timeout=%ds&readTimeout=%ds&writeTimeout=%ds", sec, sec, sec)
		}
		return dsn + fmt.Sprintf("?timeout=%ds&readTimeout=%ds&writeTimeout=%ds", sec, sec, sec)
	case "postgresql":
		if strings.Contains(dsn, "connect_timeout") {
			return dsn
		}
		return dsn + fmt.Sprintf(" connect_timeout=%d", sec)
	case "sqlserver":
		if strings.Contains(dsn, "connection timeout") {
			return dsn
		}
		if strings.Contains(dsn, "?") {
			return dsn + fmt.Sprintf("&connection%%20timeout=%d", sec)
		}
		return dsn + fmt.Sprintf("?connection%%20timeout=%d", sec)
	default:
		return dsn
	}
}
