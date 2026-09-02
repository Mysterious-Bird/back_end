package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port    string
	DB      DBConfig
	JWT     JWTConfig
	WeChat  WeChatConfig
	Payment PaymentConfig
	Backup  BackupConfig
	Upload  UploadConfig
	Map     MapConfig
}

// PaymentConfig 支付渠道。Provider=mock|wechat；wechat 需另配商户参数与证书。
type PaymentConfig struct {
	Provider          string
	WeChatEnabled     bool
	WeChatMchID       string
	WeChatAPIKey      string // APIv3 密钥，32 位
	WeChatSerialNo    string // 商户证书序列号
	WeChatCertPath    string // 商户证书路径（apiclient_cert.pem）
	WeChatKeyPath     string // 商户私钥路径（apiclient_key.pem）
	WeChatNotifyURL   string
	RefundAlertWebhookURL string // 退款发起失败告警 webhook（钉钉/企业微信/飞书机器人均可），空则仅记日志
	PayTimeoutMinutes int // 待支付订单超时分钟数，超时未支付则关单回滚
}

type BackupConfig struct {
	Enabled    bool
	Dir        string
	Interval   time.Duration
	RetainDays int
	Compress   bool

	// 备份邮件（加密 zip 附件）
	EmailEnabled    bool
	EmailTo         string
	EmailAPIURL     string
	EmailAPIKey     string
	EmailInterval   time.Duration // 发信间隔，默认 168h（每周）
	EmailZipPassword string
	EmailSubject    string
}

type DBConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Name     string
}

type JWTConfig struct {
	Secret string
}

type WeChatConfig struct {
	AppID  string
	Secret string
}

// MapConfig 地图服务配置。TencentKey 用于腾讯位置服务 POI 检索（配送范围地标）。
type MapConfig struct {
	TencentKey string
}

func (d DBConfig) DSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		d.User, d.Password, d.Host, d.Port, d.Name,
	)
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port: getEnv("PORT", "8080"),
		DB: DBConfig{
			Host:     getEnv("DB_HOST", "127.0.0.1"),
			Port:     getEnv("DB_PORT", "3306"),
			User:     getEnv("DB_USER", "root"),
			Password: getEnv("DB_PASSWORD", ""),
			Name:     getEnv("DB_NAME", "yujixinjiang"),
		},
		JWT: JWTConfig{
			Secret: getEnv("JWT_SECRET", "dev-secret-change-me"),
		},
		WeChat: WeChatConfig{
			AppID:  getEnv("WECHAT_APPID", ""),
			Secret: getEnv("WECHAT_SECRET", ""),
		},
		Payment: PaymentConfig{
			Provider:          getEnv("PAYMENT_PROVIDER", "mock"),
			WeChatEnabled:     getEnv("WECHAT_PAY_ENABLED", "false") == "true",
			WeChatMchID:       getEnv("WECHAT_MCH_ID", ""),
			WeChatAPIKey:      getEnv("WECHAT_PAY_API_KEY", ""),
			WeChatSerialNo:    getEnv("WECHAT_PAY_SERIAL_NO", ""),
			WeChatCertPath:    getEnv("WECHAT_PAY_CERT_PATH", ""),
			WeChatKeyPath:     getEnv("WECHAT_PAY_KEY_PATH", ""),
			WeChatNotifyURL:   getEnv("WECHAT_PAY_NOTIFY_URL", ""),
			RefundAlertWebhookURL: getEnv("REFUND_ALERT_WEBHOOK_URL", ""),
			PayTimeoutMinutes: loadPayTimeoutMinutes(),
		},
		Backup: loadBackupConfig(),
	}
	cfg.Upload = loadUploadConfig(cfg.Port)
	cfg.Map = MapConfig{
		TencentKey: getEnv("TENCENT_MAP_KEY", ""),
	}

	if err := cfg.normalizePayment(); err != nil {
		return nil, err
	}
	if err := cfg.validateRuntime(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// IsRelease 是否以生产模式运行（GIN_MODE=release）。
func IsRelease() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GIN_MODE")), "release")
}

var jwtSecretPlaceholders = map[string]struct{}{
	"":                                 {},
	"dev-secret-change-me":             {},
	"change-me-to-a-long-random-string": {},
}

// validateRuntime 在 release 下强制安全配置；开发环境仅告警。
func (c *Config) validateRuntime() error {
	secret := strings.TrimSpace(c.JWT.Secret)
	_, isPlaceholder := jwtSecretPlaceholders[secret]

	if IsRelease() {
		if isPlaceholder || len(secret) < 32 {
			return fmt.Errorf("生产环境 JWT_SECRET 必须为至少 32 字符的随机串，禁止使用示例值")
		}
		provider := strings.ToLower(strings.TrimSpace(c.Payment.Provider))
		if provider != "wechat" && provider != "wx" && provider != "weixin" {
			return fmt.Errorf("生产环境禁止使用 mock 支付，请设置 PAYMENT_PROVIDER=wechat 且 WECHAT_PAY_ENABLED=true")
		}
		if !c.Payment.WeChatEnabled {
			return fmt.Errorf("生产环境必须设置 WECHAT_PAY_ENABLED=true")
		}
		if strings.TrimSpace(c.WeChat.AppID) == "" || strings.TrimSpace(c.WeChat.Secret) == "" {
			return fmt.Errorf("生产环境必须配置 WECHAT_APPID 与 WECHAT_SECRET")
		}
		return nil
	}

	if isPlaceholder {
		log.Println("警告: JWT_SECRET 仍为示例值，上线前务必更换为随机长串")
	}
	provider := strings.ToLower(strings.TrimSpace(c.Payment.Provider))
	if provider == "mock" || provider == "" {
		log.Println("警告: 当前为 mock 支付（下单即视为已付），生产环境请改用微信支付")
	}
	return nil
}

// normalizePayment 规范化支付配置，启用微信时校验必填项。
func (c *Config) normalizePayment() error {
	p := &c.Payment
	p.Provider = strings.ToLower(strings.TrimSpace(p.Provider))
	p.WeChatMchID = strings.TrimSpace(p.WeChatMchID)
	p.WeChatAPIKey = strings.TrimSpace(p.WeChatAPIKey)
	p.WeChatSerialNo = strings.TrimSpace(strings.ToUpper(p.WeChatSerialNo))
	p.WeChatCertPath = strings.TrimSpace(p.WeChatCertPath)
	p.WeChatKeyPath = strings.TrimSpace(p.WeChatKeyPath)
	p.WeChatNotifyURL = strings.TrimSpace(p.WeChatNotifyURL)

	if p.Provider != "wechat" && p.Provider != "wx" && p.Provider != "weixin" {
		return nil
	}
	if !p.WeChatEnabled {
		return nil
	}

	missing := make([]string, 0, 6)
	if p.WeChatMchID == "" {
		missing = append(missing, "WECHAT_MCH_ID")
	}
	if p.WeChatAPIKey == "" {
		missing = append(missing, "WECHAT_PAY_API_KEY")
	} else if len(p.WeChatAPIKey) != 32 {
		return fmt.Errorf("WECHAT_PAY_API_KEY 必须为 32 字符（当前 %d），请与商户平台 APIv3 密钥保持一致", len(p.WeChatAPIKey))
	}
	if p.WeChatSerialNo == "" {
		missing = append(missing, "WECHAT_PAY_SERIAL_NO")
	}
	if p.WeChatKeyPath == "" {
		missing = append(missing, "WECHAT_PAY_KEY_PATH")
	}
	if p.WeChatNotifyURL == "" {
		missing = append(missing, "WECHAT_PAY_NOTIFY_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("微信支付配置不完整，缺少: %s", strings.Join(missing, ", "))
	}
	return nil
}

func loadBackupConfig() BackupConfig {
	interval, err := time.ParseDuration(getEnv("BACKUP_INTERVAL", "24h"))
	if err != nil || interval < time.Minute {
		interval = 24 * time.Hour
	}
	retain, _ := strconv.Atoi(getEnv("BACKUP_RETAIN_DAYS", "7"))
	emailInterval, err := time.ParseDuration(getEnv("BACKUP_EMAIL_INTERVAL", "168h"))
	if err != nil || emailInterval < time.Hour {
		emailInterval = 168 * time.Hour
	}
	apiURL := strings.TrimSpace(getEnv("BACKUP_EMAIL_API_URL", "https://www.catmicloud.cn/api/v1/email/send-attachment"))
	return BackupConfig{
		Enabled:          getEnv("BACKUP_ENABLED", "false") == "true",
		Dir:              getEnv("BACKUP_DIR", "backups"),
		Interval:         interval,
		RetainDays:       retain,
		Compress:         getEnv("BACKUP_COMPRESS", "true") == "true",
		EmailEnabled:     getEnv("BACKUP_EMAIL_ENABLED", "false") == "true",
		EmailTo:          strings.TrimSpace(getEnv("BACKUP_EMAIL_TO", "")),
		EmailAPIURL:      apiURL,
		EmailAPIKey:      strings.TrimSpace(getEnv("BACKUP_EMAIL_API_KEY", "")),
		EmailInterval:    emailInterval,
		EmailZipPassword: getEnv("BACKUP_EMAIL_ZIP_PASSWORD", ""),
		EmailSubject:     strings.TrimSpace(getEnv("BACKUP_EMAIL_SUBJECT", "雨季新江数据库备份")),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// loadPayTimeoutMinutes 读取待支付订单超时分钟数，默认 5，最小 1。
func loadPayTimeoutMinutes() int {
	v, err := strconv.Atoi(getEnv("PAY_TIMEOUT_MINUTES", "5"))
	if err != nil || v < 1 {
		return 5
	}
	return v
}
