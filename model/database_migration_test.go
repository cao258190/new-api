package model

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TestDatabaseMigrationLifecycle runs in separate seed and verification processes
// so a released version can create the database that the candidate must upgrade.
func TestDatabaseMigrationLifecycle(t *testing.T) {
	phase := os.Getenv("TEST_DATABASE_PHASE")
	if phase == "" {
		t.Skip("TEST_DATABASE_PHASE is not configured")
	}
	require.Contains(t, []string{"seed", "verify"}, phase)
	dialect := os.Getenv("TEST_DATABASE_DIALECT")
	require.Contains(t, []string{"sqlite", "mysql", "postgres"}, dialect)
	database := os.Getenv("TEST_DATABASE_NAME")
	require.Regexp(t, `^newapi_matrix_[a-z0-9_]{1,44}$`, database)

	previousDB, previousLogDB := DB, LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMaster, previousSQLitePath := common.IsMasterNode, common.SQLitePath
	t.Cleanup(func() {
		if LOG_DB != nil && LOG_DB != previousLogDB && LOG_DB != DB {
			assert.NoError(t, closeDB(LOG_DB))
		}
		if DB != nil && DB != previousDB {
			assert.NoError(t, closeDB(DB))
		}
		DB, LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.IsMasterNode, common.SQLitePath = previousMaster, previousSQLitePath
		initCol()
	})

	common.IsMasterNode = true
	t.Setenv("SKIP_64BIT_QUOTA_SCHEMA_CHECK", "false")
	t.Setenv("SQL_DSN", migrationFixtureDSN(t, dialect, database, phase == "seed"))
	// SQLite has one configured file path; a separate PostgreSQL log database
	// also exercises mixed main/log dialects instead of opening the same file twice.
	logDialect := dialect
	if dialect == "sqlite" {
		logDialect = "postgres"
	}
	t.Setenv("LOG_SQL_DSN", migrationFixtureDSN(t, logDialect, database+"_log", phase == "seed"))
	require.NoError(t, InitDB())
	require.NoError(t, InitLogDB())
	for _, connection := range []*gorm.DB{DB, LOG_DB} {
		versionSQL := "SELECT version()"
		if connection.Dialector.Name() == "sqlite" {
			versionSQL = "SELECT sqlite_version()"
		}
		var version string
		require.NoError(t, connection.Raw(versionSQL).Scan(&version).Error)
		t.Logf("%s engine version: %s", connection.Dialector.Name(), version)
	}

	if phase == "seed" {
		user := User{
			Username: "migration-fixture", DisplayName: "迁移用户",
			Password: "invalid-test-hash", AffCode: "migration-fixture",
			Quota: 5_000_000_000, UsedQuota: 123456, Group: "migration-group",
		}
		require.NoError(t, DB.Create(&user).Error)
		require.Positive(t, user.Id)
		require.NoError(t, DB.Create(&Token{
			UserId: user.Id, Key: "migration-fixture-key", Name: "迁移令牌",
		}).Error)
		require.NoError(t, DB.Create(&Option{Key: "MigrationFixture", Value: "保留已有配置"}).Error)
		require.NoError(t, DB.Create(&PrefillGroup{
			Name: "迁移预设", Type: "params", Items: JSONValue(`{"label":"迁移数据","zero":0}`),
		}).Error)
		require.NoError(t, DB.Create(&SubscriptionPlan{Title: "迁移套餐", PriceAmount: 1.234567}).Error)
		require.NoError(t, LOG_DB.Create(&Log{
			UserId: user.Id, CreatedAt: 1700000000, Type: LogTypeConsume,
			Content: "保留已有日志", ModelName: "migration-model", Quota: 12345,
		}).Error)
		indexes := map[string]map[string][]string{
			"main": migrationFixtureIndexes(t, DB, []string{"users", "tokens", "options", "prefill_groups", "subscription_plans", "logs"}),
			"log":  migrationFixtureIndexes(t, LOG_DB, []string{"logs"}),
		}
		encoded, err := common.Marshal(indexes)
		require.NoError(t, err)
		require.NoError(t, DB.Create(&Option{Key: "MigrationFixtureIndexes", Value: string(encoded)}).Error)
		return
	}

	var user User
	require.NoError(t, DB.Where("username = ?", "migration-fixture").First(&user).Error)
	assert.Equal(t, "迁移用户", user.DisplayName)
	assert.EqualValues(t, 5_000_000_000, user.Quota)
	assert.EqualValues(t, 123456, user.UsedQuota)
	assert.Equal(t, "migration-group", user.Group)
	var token Token
	require.NoError(t, DB.Where("name = ?", "迁移令牌").First(&token).Error)
	assert.Equal(t, user.Id, token.UserId)
	assert.Equal(t, "migration-fixture-key", token.Key)
	var option Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "MigrationFixture").First(&option).Error)
	assert.Equal(t, "保留已有配置", option.Value)
	var group PrefillGroup
	require.NoError(t, DB.Where("name = ?", "迁移预设").First(&group).Error)
	var items map[string]any
	require.NoError(t, common.Unmarshal(group.Items, &items))
	assert.Equal(t, map[string]any{"label": "迁移数据", "zero": float64(0)}, items)
	var plan SubscriptionPlan
	require.NoError(t, DB.Where("title = ?", "迁移套餐").First(&plan).Error)
	assert.InDelta(t, 1.234567, plan.PriceAmount, 0.000000001)
	var entry Log
	require.NoError(t, LOG_DB.Where("model_name = ?", "migration-model").First(&entry).Error)
	assert.Equal(t, user.Id, entry.UserId)
	assert.Equal(t, "保留已有日志", entry.Content)
	assert.EqualValues(t, 12345, entry.Quota)

	var storedIndexes Option
	require.NoError(t, DB.Where(commonKeyCol+" = ?", "MigrationFixtureIndexes").First(&storedIndexes).Error)
	var baselineIndexes map[string]map[string][]string
	require.NoError(t, common.UnmarshalJsonStr(storedIndexes.Value, &baselineIndexes))
	for name, connection := range map[string]*gorm.DB{"main": DB, "log": LOG_DB} {
		tables := make([]string, 0, len(baselineIndexes[name]))
		for table := range baselineIndexes[name] {
			tables = append(tables, table)
		}
		actual := migrationFixtureIndexes(t, connection, tables)
		for table, signatures := range baselineIndexes[name] {
			for _, signature := range signatures {
				assert.Contains(t, actual[table], signature, "%s.%s must preserve its existing index coverage", name, table)
			}
		}
	}

	assert.Error(t, DB.Create(&Token{UserId: user.Id, Key: token.Key, Name: "duplicate-key"}).Error)
	assert.Error(t, DB.Create(&Option{Key: option.Key, Value: "duplicate-key"}).Error)
	assert.Error(t, DB.Create(&PrefillGroup{Name: group.Name, Type: "params", Items: JSONValue(`{}`)}).Error)
	assert.Error(t, DB.Create(&User{Username: user.Username, Password: "invalid-test-hash", AffCode: "different-fixture"}).Error)

	// Run the actual application migrations again, including the separately
	// configured log database. Stable restarts must not keep issuing schema DDL.
	recorder := &migrationSQLRecorder{}
	DB = DB.Session(&gorm.Session{Logger: recorder})
	LOG_DB = LOG_DB.Session(&gorm.Session{Logger: recorder})
	for range 2 {
		recorder.reset()
		require.NoError(t, migrateDB())
		require.NoError(t, migrateLOGDB())
		assert.Empty(t, recorder.schemaMutations(), "repeated migrations must not mutate an unchanged schema")
	}
	var users, tokens, groups, logs int64
	require.NoError(t, DB.Model(&User{}).Count(&users).Error)
	require.NoError(t, DB.Model(&Token{}).Count(&tokens).Error)
	require.NoError(t, DB.Model(&PrefillGroup{}).Count(&groups).Error)
	require.NoError(t, LOG_DB.Model(&Log{}).Count(&logs).Error)
	assert.EqualValues(t, 1, users)
	assert.EqualValues(t, 1, tokens)
	assert.EqualValues(t, 1, groups)
	assert.EqualValues(t, 1, logs)
}

// migrationFixtureDSN creates only explicitly named disposable test databases.
// Server credentials come from the existing test DSNs and are never logged.
func migrationFixtureDSN(t *testing.T, dialect, database string, create bool) string {
	t.Helper()
	require.Regexp(t, `^newapi_matrix_[a-z0-9_]{1,48}$`, database)
	if dialect == "sqlite" {
		directory := os.Getenv("TEST_DATABASE_DIRECTORY")
		require.NotEmpty(t, directory)
		require.True(t, filepath.IsAbs(directory))
		require.NoError(t, os.MkdirAll(directory, 0o700))
		common.SQLitePath = filepath.Join(directory, database+".db")
		if create {
			require.NoFileExists(t, common.SQLitePath)
		}
		return "local"
	}
	env := "TEST_MYSQL_DSN"
	if dialect == "postgres" {
		env = "TEST_POSTGRES_DSN"
	}
	base := os.Getenv(env)
	require.NotEmpty(t, base, "%s must be configured", env)
	if create {
		connection, _, err := chooseDB(env, false)
		require.NoError(t, err)
		sqlDB, err := connection.DB()
		require.NoError(t, err)
		defer func() { assert.NoError(t, sqlDB.Close()) }()
		statement := "CREATE DATABASE ?"
		if dialect == "mysql" {
			statement += " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
		}
		require.NoError(t, connection.Exec(statement, clause.Table{Name: database}).Error)
	}
	if dialect == "mysql" {
		config, err := mysqldriver.ParseDSN(base)
		require.NoError(t, err)
		config.DBName = database
		return config.FormatDSN()
	}
	parsed, err := url.Parse(base)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, parsed.Scheme)
	parsed.Path = "/" + database
	return parsed.String()
}

// migrationFixtureIndexes records ordered columns and uniqueness, allowing an
// equivalent replacement index while detecting lost lookup or uniqueness rules.
func migrationFixtureIndexes(t *testing.T, connection *gorm.DB, tables []string) map[string][]string {
	t.Helper()
	result := make(map[string][]string, len(tables))
	for _, table := range tables {
		indexes, err := connection.Migrator().GetIndexes(table)
		require.NoError(t, err)
		for _, index := range indexes {
			unique, known := index.Unique()
			require.True(t, known)
			result[table] = append(result[table], fmt.Sprintf("%t:%s", unique, strings.Join(index.Columns(), ",")))
		}
		slices.Sort(result[table])
	}
	return result
}
