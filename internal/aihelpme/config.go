package aihelpme

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the daemon's call configuration, loaded from config.yaml at
// startup. The cli never touches it: only aihelpmed talks to the model API.
type Config struct {
	WorkspaceID  string
	Model        string
	SystemPrompt string
	CallTimeout  time.Duration
	QuotaDB      string
}

// configFile mirrors the yaml layout; CallTimeout stays a string so that
// "10m" parses as a duration instead of nanoseconds.
type configFile struct {
	WorkspaceID  string `yaml:"workspace_id"`
	Model        string `yaml:"model"`
	SystemPrompt string `yaml:"system_prompt"`
	CallTimeout  string `yaml:"call_timeout"`
	QuotaDB      string `yaml:"quota_db"`
}

const configExample = `workspace_id: <百炼 workspace id>
model: deepseek-v4-flash-0731
call_timeout: 10m              # 可选，默认 10m
system_prompt: <回答风格提示词，可留空>
quota_db: <quota 工具的 sqlite 路径，可选，启用 /model 接口>`

// ConfigPath returns the config file location: $AIHELPME_CONFIG when set,
// otherwise $XDG_CONFIG_HOME/aihelpme/config.yaml (usually
// ~/.config/aihelpme/config.yaml).
func ConfigPath() string {
	if p := os.Getenv("AIHELPME_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "aihelpme", "config.yaml")
}

// LoadConfig reads and validates config.yaml. A missing or malformed file
// fails with the path and a minimal example; empty workspace_id or model
// fails with the missing field names.
func LoadConfig() (*Config, error) {
	path := ConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置 %s 失败：%w\n配置需要如下内容：\n%s", path, err, configExample)
	}
	var f configFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("解析配置 %s 失败：%w", path, err)
	}
	cfg := &Config{
		WorkspaceID:  strings.TrimSpace(f.WorkspaceID),
		Model:        strings.TrimSpace(f.Model),
		SystemPrompt: strings.TrimSpace(f.SystemPrompt),
		QuotaDB:      strings.TrimSpace(f.QuotaDB),
	}
	cfg.CallTimeout = 10 * time.Minute
	if t := strings.TrimSpace(f.CallTimeout); t != "" {
		d, err := time.ParseDuration(t)
		if err != nil {
			return nil, fmt.Errorf("配置项 call_timeout 无效（%q）：%w", t, err)
		}
		cfg.CallTimeout = d
	}
	var missing []string
	if cfg.WorkspaceID == "" {
		missing = append(missing, "workspace_id")
	}
	if cfg.Model == "" {
		missing = append(missing, "model")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("配置缺少必填项 %s（%s）", strings.Join(missing, "、"), path)
	}
	return cfg, nil
}
