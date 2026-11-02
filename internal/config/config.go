package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"forum/internal/session"
	"forum/pkg/jwt"

	"github.com/spf13/viper"
)

type SnowflakeConfig struct {
	NodeID int64 `mapstructure:"node_id"`
}

type JWTConfig struct {
	Secret        string        `mapstructure:"secret"`
	Issuer        string        `mapstructure:"issuer"`
	Audience      string        `mapstructure:"audience"`
	AccessTTL     time.Duration `mapstructure:"access_ttl"`
	RenewalWindow time.Duration `mapstructure:"renewal_window"`
	MaxLifetime   time.Duration `mapstructure:"max_lifetime"`
}

func (c JWTConfig) SessionOptions() session.Options {
	return session.Options{AccessTTL: c.AccessTTL, RenewalWindow: c.RenewalWindow, MaxLifetime: c.MaxLifetime}
}

func (c JWTConfig) Options() jwt.Options {
	return jwt.Options{
		Secret:   c.Secret,
		Issuer:   c.Issuer,
		Audience: c.Audience,
	}
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type Config struct {
	Redis     RedisConfig     `mapstructure:"redis"`
	JWT       JWTConfig       `mapstructure:"jwt"`
	Snowflake SnowflakeConfig `mapstructure:"snowflake"`
	Server    ServerConfig    `mapstructure:"server"`
	MySQL     MySQLConfig     `mapstructure:"mysql"`
}

type ServerConfig struct {
	Addr string `mapstructure:"addr"`
}

type MySQLConfig struct {
	Addr         string `mapstructure:"addr"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	Database     string `mapstructure:"database"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// Load 在启动时读取一次配置，环境变量优先于 YAML。
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetDefault("redis.addr", "127.0.0.1:6379")
	v.SetDefault("jwt.access_ttl", "30m")
	v.SetDefault("jwt.renewal_window", "5m")
	v.SetDefault("jwt.max_lifetime", "168h")

	// 显式绑定让 Unmarshal 也能识别环境变量，并保留已有的 DB_* 用法。
	bindings := map[string]string{
		"redis.addr":           "REDIS_ADDR",
		"redis.username":       "REDIS_USERNAME",
		"redis.password":       "REDIS_PASSWORD",
		"redis.db":             "REDIS_DB",
		"jwt.secret":           "JWT_SECRET",
		"jwt.issuer":           "JWT_ISSUER",
		"jwt.audience":         "JWT_AUDIENCE",
		"jwt.access_ttl":       "JWT_ACCESS_TTL",
		"jwt.renewal_window":   "JWT_RENEWAL_WINDOW",
		"jwt.max_lifetime":     "JWT_MAX_LIFETIME",
		"snowflake.node_id":    "SNOWFLAKE_NODE_ID",
		"server.addr":          "SERVER_ADDR",
		"mysql.addr":           "DB_ADDR",
		"mysql.user":           "DB_USER",
		"mysql.password":       "DB_PASSWORD",
		"mysql.database":       "DB_NAME",
		"mysql.max_open_conns": "DB_MAX_OPEN_CONNS",
		"mysql.max_idle_conns": "DB_MAX_IDLE_CONNS",
	}
	for key, env := range bindings {
		if err := v.BindEnv(key, env); err != nil {
			return nil, fmt.Errorf("bind config %s: %w", key, err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.UnmarshalExact(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c Config) validate() error {
	if err := c.JWT.Options().Validate(); err != nil {
		return err
	}
	if err := c.JWT.SessionOptions().Validate(); err != nil {
		return err
	}
	if c.Snowflake.NodeID < 0 || c.Snowflake.NodeID > 1023 {
		return fmt.Errorf("snowflake.node_id must be between 0 and 1023")
	}
	if err := validateAddress("server.addr", c.Server.Addr); err != nil {
		return err
	}
	if err := validateAddress("mysql.addr", c.MySQL.Addr); err != nil {
		return err
	}

	if err := validateAddress("redis.addr", c.Redis.Addr); err != nil {
		return err
	}
	if c.Redis.DB < 0 {
		return fmt.Errorf("redis.db must be nonnegative")
	}
	if strings.TrimSpace(c.MySQL.User) == "" || strings.TrimSpace(c.MySQL.Database) == "" {
		return fmt.Errorf("mysql.user and mysql.database are required")
	}
	if c.MySQL.MaxOpenConns <= 0 {
		return fmt.Errorf("mysql.max_open_conns must be positive")
	}
	if c.MySQL.MaxIdleConns < 0 || c.MySQL.MaxIdleConns > c.MySQL.MaxOpenConns {
		return fmt.Errorf("mysql.max_idle_conns must be between 0 and mysql.max_open_conns")
	}
	return nil
}

func validateAddress(key, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" {
		return fmt.Errorf("%s must contain a host and port", key)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return fmt.Errorf("%s port must be between 1 and 65535", key)
	}
	return nil
}
