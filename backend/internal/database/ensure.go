// 库名经白名单校验（CREATE DATABASE 无法参数化,防标识符注入）。
package database

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// EnsureDatabase 确保外部引擎的目标库存在（不存在则创建）,SQLite 天然文件即库、直接成功。
// FIXME: CREATE DATABASE没法参数化,白名单先挡着
func EnsureDatabase(engine, host string, port int, user, password, name string) error {
	switch engine {
	case "sqlite":
		return nil
	case "mysql", "postgresql", "sqlserver":
		if !validDbName(name) {
			return fmt.Errorf("数据库名仅允许字母开头的字母/数字/下划线，长度 1-64")
		}
	default:
		return fmt.Errorf("不支持的数据库驱动: %s", engine)
	}

	db, err := openAdmin(engine, host, port, user, password)
	if err != nil {
		return fmt.Errorf("连接数据库服务器（建库）: %w", err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	switch engine {
	case "mysql":
		// utf8mb4：完整四字节 UTF-8，与业务 DSN 的 charset 一致
		return db.Exec(fmt.Sprintf(
			"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", name)).Error
	case "postgresql":
		var n int64
		if err := db.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", name).Scan(&n).Error; err != nil {
			return fmt.Errorf("查询库列表: %w", err)
		}
		if n > 0 {
			return nil
		}
		return db.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, name)).Error
	default: // sqlserver
		return db.Exec(fmt.Sprintf(
			"IF NOT EXISTS (SELECT name FROM sys.databases WHERE name = N'%s') CREATE DATABASE [%s]", name, name)).Error
	}
}

func openAdmin(engine, host string, port int, user, password string) (*gorm.DB, error) {
	switch engine {
	case "mysql":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?charset=utf8mb4&parseTime=True&loc=Local&timeout=5s&readTimeout=5s",
			user, password, host, port)
		return gorm.Open(mysql.Open(dsn), &gorm.Config{})
	case "postgresql":
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable connect_timeout=5",
			host, port, user, password)
		return gorm.Open(postgres.Open(dsn), &gorm.Config{})
	default:
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=master&connection+timeout=5",
			url.QueryEscape(user), url.QueryEscape(password), host, port)
		return gorm.Open(sqlserver.Open(dsn), &gorm.Config{})
	}
}

// validDbName 库名白名单：字母开头,字母/数字/下划线,1-64 位。
func validDbName(name string) bool {
	if len(name) < 1 || len(name) > 64 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9', r == '_':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// 文件打不开（损坏/被锁）按"非空"处理,走冲突避让,绝不覆盖
func SqliteAccountsEmpty(file string) bool {
	db, err := gorm.Open(sqlite.Open(file), &gorm.Config{})
	if err != nil {
		return false
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}
	var n int64
	if err := db.Raw("SELECT COUNT(*) FROM accounts").Scan(&n).Error; err != nil {
		return true // 表不存在 → 空文件
	}
	return n == 0
}

// TestAdmin 测试数据库服务器的连通性（管理连接，不指定业务库）。
// 安装引导场景目标库尚未创建——先验证服务器可达与账号可登录
// 目标主机必须是回环或私网地址：本工具连接的数据库天然部署在内网,
// 公网目标一律拒绝（防把服务器当作跳板探测公网）。
func TestAdmin(engine, host string, port int, user, password string, timeout time.Duration) error {
	if err := RequirePrivateHost(host); err != nil {
		return err
	}
	db, err := openAdmin(engine, host, port, user, password)
	if err != nil {
		return err
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return sqlDB.PingContext(ctx)
}

// CanCreateDatabase 探测账号是否具备建库权限（尽力而为：探测本身失败按"具备"
func CanCreateDatabase(engine, host string, port int, user, password string) (bool, string) {
	db, err := openAdmin(engine, host, port, user, password)
	if err != nil {
		return true, "" // 连接失败由 TestAdmin 报告
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}
	switch engine {
	case "mysql":
		rows, err := db.Raw("SHOW GRANTS FOR CURRENT_USER()").Rows()
		if err != nil {
			return true, ""
		}
		defer rows.Close()
		for rows.Next() {
			var g string
			if err := rows.Scan(&g); err != nil {
				continue
			}
			// 服务器级授权（*.*）含 ALL PRIVILEGES 或 CREATE 即可建库
			if strings.Contains(g, "*.*") &&
				(strings.Contains(g, "ALL PRIVILEGES") || strings.Contains(g, "CREATE")) {
				return true, ""
			}
		}
		return false, "该账号缺少服务器级 CREATE 权限"
	case "postgresql":
		var ok bool
		if err := db.Raw("SELECT rolcreatedb OR rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&ok).Error; err != nil {
			return true, ""
		}
		if ok {
			return true, ""
		}
		return false, "该角色缺少 CREATEDB 权限"
	case "sqlserver":
		var n int
		if err := db.Raw("SELECT IS_SRVROLEMEMBER('dbcreator')").Scan(&n).Error; err != nil {
			return true, ""
		}
		if n == 1 {
			return true, ""
		}
		return false, "该登录缺少 dbcreator 服务器角色"
	}
	return true, ""
}

// （DROP 无法参数化，防标识符注入）；连接管理库执行，不触碰其他库
func DropDatabase(engine, host string, port int, user, password, name string) error {
	if !validDbName(name) {
		return fmt.Errorf("数据库名不合法，拒绝删除")
	}
	db, err := openAdmin(engine, host, port, user, password)
	if err != nil {
		return err
	}
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		defer sqlDB.Close()
	}
	switch engine {
	case "mysql":
		return db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", name)).Error
	case "postgresql":
		return db.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS "%s"`, name)).Error
	case "sqlserver":
		return db.Exec(fmt.Sprintf(
			"IF EXISTS (SELECT name FROM sys.databases WHERE name = N'%s') DROP DATABASE [%s]", name, name)).Error
	}
	return fmt.Errorf("不支持的数据库驱动: %s", engine)
}

func DbExists(engine, host string, port int, user, password, name string) (bool, error) {
	switch engine {
	case "sqlite":
		_, err := os.Stat(name)
		return err == nil, nil
	case "mysql", "postgresql", "sqlserver":
	default:
		return false, fmt.Errorf("不支持的数据库驱动: %s", engine)
	}
	db, err := openAdmin(engine, host, port, user, password)
	if err != nil {
		return false, err
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	var n int64
	switch engine {
	case "mysql":
		err = db.Raw("SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", name).Scan(&n).Error
	case "postgresql":
		err = db.Raw("SELECT COUNT(*) FROM pg_database WHERE datname = ?", name).Scan(&n).Error
	default:
		err = db.Raw("SELECT COUNT(*) FROM sys.databases WHERE name = ?", name).Scan(&n).Error
	}
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func RequirePrivateHost(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		return checkPrivateIP(ip, host)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("无法解析数据库主机地址 %s", host)
	}
	for _, ip := range ips {
		if err := checkPrivateIP(ip, host); err == nil {
			return nil
		}
	}
	return fmt.Errorf("数据库主机 %s 不是内网地址，已拒绝连接", host)
}

func checkPrivateIP(ip net.IP, host string) error {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() {
		return nil
	}
	return fmt.Errorf("数据库主机 %s 不是内网地址，已拒绝连接", host)
}
