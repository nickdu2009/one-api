package model

import (
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"
	"one-api/common"
	"os"
	"strings"
	"time"
)

var DB *gorm.DB

func createRootAccountIfNeed(ctx context.Context) error {
	var user User
	//if user.Status != common.UserStatusEnabled {
	if err := DB.WithContext(ctx).First(&user).Error; err != nil {
		common.SysLog("no user exists, create a root user for you: username is root, password is 123456")
		hashedPassword, err := common.Password2Hash("123456")
		if err != nil {
			return err
		}
		rootUser := User{
			Username:    "root",
			Password:    hashedPassword,
			Role:        common.RoleRootUser,
			Status:      common.UserStatusEnabled,
			DisplayName: "Root User",
			AccessToken: common.GetUUID(),
			Quota:       100000000,
		}
		DB.WithContext(ctx).Create(&rootUser)
	}
	return nil
}

func chooseDB() (*gorm.DB, error) {
	if os.Getenv("SQL_DSN") != "" {
		dsn := os.Getenv("SQL_DSN")
		if strings.HasPrefix(dsn, "postgres://") {
			// Use PostgreSQL
			common.SysLog("using PostgreSQL as database")
			common.UsingPostgreSQL = true
			return gorm.Open(postgres.New(postgres.Config{
				DSN:                  dsn,
				PreferSimpleProtocol: true, // disables implicit prepared statement usage
			}), &gorm.Config{
				PrepareStmt: true, // precompile SQL
			})
		}
		// Use MySQL
		common.SysLog("using MySQL as database")
		return gorm.Open(mysql.Open(dsn), &gorm.Config{
			PrepareStmt: true, // precompile SQL
		})
	}
	// Use SQLite
	common.SysLog("SQL_DSN not set, using SQLite as database")
	common.UsingSQLite = true
	return gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{
		PrepareStmt: true, // precompile SQL
	})
}

func InitDB(ctx context.Context) (err error) {
	db, err := chooseDB()
	if err == nil {
		if err := db.Use(tracing.NewPlugin(tracing.WithoutMetrics())); err != nil {
			panic(err)
		}
		if common.DebugEnabled {
			db = db.Debug()
		}
		DB = db
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}

		sqlDB.SetMaxIdleConns(common.GetOrDefault("SQL_MAX_IDLE_CONNS", 100))
		sqlDB.SetMaxOpenConns(common.GetOrDefault("SQL_MAX_OPEN_CONNS", 1000))
		sqlDB.SetConnMaxLifetime(time.Minute * time.Duration(common.GetOrDefault("SQL_MAX_LIFETIME", 60)))

		if !common.IsMasterNode {
			return nil
		}
		common.SysLog("database migration started")
		err = db.AutoMigrate(&Channel{})
		if err != nil {
			return err
		}
		err = db.AutoMigrate(&Token{})
		if err != nil {
			return err
		}
		err = db.AutoMigrate(&User{})
		if err != nil {
			return err
		}
		err = db.AutoMigrate(&Option{})
		if err != nil {
			return err
		}
		err = db.AutoMigrate(&Redemption{})
		if err != nil {
			return err
		}
		err = db.AutoMigrate(&Ability{})
		if err != nil {
			return err
		}
		err = db.AutoMigrate(&Log{})
		if err != nil {
			return err
		}
		common.SysLog("database migrated")
		err = createRootAccountIfNeed(ctx)
		if err != nil {
			return err
		}
		go func() {
			for {
				time.Sleep(time.Second)
				data, _ := json.Marshal(sqlDB.Stats())
				common.SysLog(fmt.Sprintf("sql db stats %s", data))
			}

		}()
		return nil
	} else {
		common.FatalLog(err)
	}
	return err
}

func CloseDB() error {
	sqlDB, err := DB.DB()
	if err != nil {
		return err
	}
	err = sqlDB.Close()
	return err
}
