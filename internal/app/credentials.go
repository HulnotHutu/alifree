package app

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// 默认路径与旧版工具保持一致（含环境变量覆盖）。
func DefaultEnvFile() string {
	if v := os.Getenv("BAILIAN_ENV_FILE"); v != "" {
		return v
	}
	return filepath.Join(home(), ".config", "bailian-quota.env")
}

func DefaultDataDir() string {
	if v := os.Getenv("BAILIAN_OUT_DIR"); v != "" {
		return v
	}
	return filepath.Join(home(), ".local", "share", "bailian-quota")
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

// Credentials 是抓取所需的阿里云凭证。
type Credentials struct {
	AK  string
	SK  string
	STS string
}

// LoadCredentials 读取凭证：环境变量为底，凭证文件中的 ALIBABA_* 覆盖。
// 凭证文件格式为 KEY=VALUE（兼容引号与 # 注释，同旧版 run_free_quota.sh）。
func LoadCredentials(envFile string) Credentials {
	var c Credentials
	c.AK = os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	c.SK = os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	c.STS = os.Getenv("ALIBABA_CLOUD_SECURITY_TOKEN")

	if fileVars, err := godotenv.Read(envFile); err == nil {
		for key, val := range fileVars {
			switch key {
			case "ALIBABA_CLOUD_ACCESS_KEY_ID":
				c.AK = val
			case "ALIBABA_CLOUD_ACCESS_KEY_SECRET":
				c.SK = val
			case "ALIBABA_CLOUD_SECURITY_TOKEN":
				c.STS = val
			}
		}
	}
	return c
}

// Complete 报告凭证是否齐备。
func (c Credentials) Complete() bool { return c.AK != "" && c.SK != "" }
