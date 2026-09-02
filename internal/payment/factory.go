package payment

import (
	"fmt"
	"log"
	"strings"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/payment/wechatv3"

	"gorm.io/gorm"
)

// NewProvider 按配置创建支付实现。wechat 初始化失败时返回错误，不再静默降级为 Mock。
func NewProvider(cfg *config.Config, db *gorm.DB) (Provider, error) {
	name := "mock"
	if cfg != nil && cfg.Payment.Provider != "" {
		name = strings.ToLower(strings.TrimSpace(cfg.Payment.Provider))
	}
	switch name {
	case "wechat", "wx", "weixin":
		v3cfg := wechatv3.Config{
			MchID:    cfg.Payment.WeChatMchID,
			SerialNo: cfg.Payment.WeChatSerialNo,
			CertPath: cfg.Payment.WeChatCertPath,
			KeyPath:  cfg.Payment.WeChatKeyPath,
			APIKey:   cfg.Payment.WeChatAPIKey,
		}
		client, err := wechatv3.NewClient(v3cfg)
		if err != nil {
			return nil, fmt.Errorf("微信支付 V3 客户端初始化失败: %w", err)
		}
		if cfg.Payment.WeChatEnabled {
			if certSerial, err := wechatv3.ReadCertSerialNo(cfg.Payment.WeChatCertPath); err != nil {
				log.Printf("[wechat] 读取商户证书序列号失败: %v", err)
			} else if certSerial != cfg.Payment.WeChatSerialNo {
				log.Printf("[wechat] 警告: WECHAT_PAY_SERIAL_NO=%s 与证书文件不一致（证书为 %s），请更新 .env", cfg.Payment.WeChatSerialNo, certSerial)
			}
			if err := client.VerifyAPIKey(); err != nil {
				log.Printf("[wechat] APIv3 密钥验证失败: %v", err)
			} else {
				log.Printf("[wechat] APIv3 密钥验证通过")
			}
		}
		return &WeChatProvider{
			DB:        db,
			AppID:     cfg.WeChat.AppID,
			MchID:     cfg.Payment.WeChatMchID,
			APIKey:    cfg.Payment.WeChatAPIKey,
			NotifyURL: cfg.Payment.WeChatNotifyURL,
			Enabled:   cfg.Payment.WeChatEnabled,
			AlertWebhookURL: cfg.Payment.RefundAlertWebhookURL,
			Client:    client,
		}, nil
	default:
		if config.IsRelease() {
			return nil, fmt.Errorf("生产环境禁止使用 mock 支付")
		}
		return &MockProvider{DB: db}, nil
	}
}
