package database

import (
	"fmt"
	"reflect"

	"gorm.io/gorm"
)

// MigrateData 把 from 库的结构与数据复制到目标库（driver/dsn 指定）
// models 为各表的 &model.X{} 指针列表，顺序即复制顺序（无外键约束，顺序无耦合）。
func MigrateData(from *gorm.DB, driver, dsn string, models ...any) error {
	to, err := Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("连接目标库: %w", err)
	}
	sqlDB, err := to.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	// 结构先行：权限不足（缺 DDL）在此暴露
	if err := to.AutoMigrate(models...); err != nil {
		return fmt.Errorf("目标库建表: %w", err)
	}

	return to.Transaction(func(tx *gorm.DB) error {
		for _, m := range models {
			slicePtr := reflect.New(reflect.SliceOf(reflect.TypeOf(m).Elem())).Interface()
			if err := from.Find(slicePtr).Error; err != nil {
				return fmt.Errorf("读取源数据: %w", err)
			}
			if reflect.Indirect(reflect.ValueOf(slicePtr)).Len() == 0 {
				continue
			}
			if err := tx.Create(slicePtr).Error; err != nil {
				return fmt.Errorf("写入目标库: %w", err)
			}
		}
		return nil
	})
}
