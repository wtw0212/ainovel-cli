package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/voocel/ainovel-cli/internal/bootstrap"
	"github.com/voocel/ainovel-cli/internal/provider/antigravity"
)

func runAuthCommand(args []string) int {
	if len(args) == 0 {
		printAuthUsage()
		return 2
	}

	switch args[0] {
	case "help", "--help", "-h":
		printAuthUsage()
		return 0

	case "login":
		provider := "antigravity"
		if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			provider = strings.ToLower(args[1])
		}
		if provider != "antigravity" {
			fmt.Fprintf(os.Stderr, "auth: 目前僅支援 antigravity 登入 (got %q)\n", provider)
			return 2
		}
		return runAntigravityLogin()

	case "status":
		return runAntigravityStatus()

	case "models":
		return runAntigravityModels(args[1:])

	case "logout":
		provider := "antigravity"
		if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			provider = strings.ToLower(args[1])
		}
		if provider != "antigravity" {
			fmt.Fprintf(os.Stderr, "auth: 目前僅支援 antigravity 登出 (got %q)\n", provider)
			return 2
		}
		return runAntigravityLogout()

	default:
		fmt.Fprintf(os.Stderr, "auth: 未知子命令 %q\n", args[0])
		printAuthUsage()
		return 2
	}
}

func printAuthUsage() {
	fmt.Println(`使用方式:
  ainovel-cli auth <命令> [選項]

可用命令:
  login [provider]    進行 OAuth 登入授權並自動探測可用模型（預設: antigravity）
  status              查看目前登入憑證狀態
  models [provider]   從官方端點動態獲取帳號可用的所有模型 ID（加 --sync 同步至設定檔）
  logout [provider]   清除本地保存的授權憑證`)
}

func runAntigravityLogin() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("=== Google Antigravity OAuth 授權 ===")
	creds, err := antigravity.StartOAuthFlow(ctx, func(msg string) {
		fmt.Println(msg)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n登入失敗: %v\n", err)
		return 1
	}

	fmt.Println("正在初始化 Cloud Code Assist (GCP 專案關聯與免費配額激活)...")
	projectID, err := antigravity.DiscoverAndOnboardProject(ctx, creds, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: GCP 專案初始化失敗 (%v)，後續調用時將重試\n", err)
	} else {
		creds.ProjectID = projectID
		_ = antigravity.SaveCredentials(creds)
	}

	fmt.Println("\n✓ 授權與專案初始化成功！")
	fmt.Printf("  帳號: %s\n", creds.Email)
	if creds.ProjectID != "" {
		fmt.Printf("  GCP 專案: %s\n", creds.ProjectID)
	}
	fmt.Printf("  憑證保存路徑: %s\n", antigravity.DefaultAuthFilePath())

	// 自動從 Google 端點獲取該帳號可用的全部模型清單
	fmt.Println("\n正在動態探測可用模型清單 (fetchAvailableModels)...")
	models, err := antigravity.FetchAvailableModels(ctx, creds)
	if err == nil && len(models) > 0 {
		fmt.Printf("✓ 成功獲取到 %d 個可用模型:\n", len(models))
		for _, m := range models {
			thinkingStr := ""
			if m.SupportsThinking {
				thinkingStr = " [Thinking]"
			}
			fmt.Printf("    • %-28s (上下文: %dK)%s\n", m.ID, m.ContextWindow/1000, thinkingStr)
		}
		syncDiscoveredModelsToConfig(models)
	} else {
		fmt.Printf("（提示: 動態獲取模型清單未完成 (%v)，您仍可直接在設定檔填寫標準模型）\n", err)
	}

	fmt.Println("\n完成！您可以隨時執行 `ainovel-cli auth models` 查看最新模型，或啟動 ainovel-cli 開始創作。")
	return 0
}

func runAntigravityModels(args []string) int {
	creds, err := antigravity.LoadCredentials()
	if err != nil {
		fmt.Println("Google Antigravity: 未登入，請先執行 ainovel-cli auth login antigravity")
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("正在從 Google Antigravity 端點獲取最新可用模型...")
	models, err := antigravity.FetchAvailableModels(ctx, creds)
	if err != nil {
		fmt.Fprintf(os.Stderr, "獲取模型清單失敗: %v\n", err)
		return 1
	}

	fmt.Printf("\n✓ 帳號 %s 目前可用的 Antigravity 模型 (%d 個):\n", creds.Email, len(models))
	fmt.Printf("%-30s %-12s %-10s %s\n", "MODEL ID", "CONTEXT", "THINKING", "DISPLAY NAME")
	fmt.Println(strings.Repeat("-", 75))
	for _, m := range models {
		th := "否"
		if m.SupportsThinking {
			th = "是"
		}
		fmt.Printf("%-30s %-12s %-10s %s\n", m.ID, fmt.Sprintf("%dK", m.ContextWindow/1000), th, m.DisplayName)
	}

	doSync := false
	for _, a := range args {
		if a == "--sync" {
			doSync = true
		}
	}
	if doSync {
		syncDiscoveredModelsToConfig(models)
	} else {
		fmt.Println("\n提示: 可加上 --sync 參數（ainovel-cli auth models --sync）將上述模型自動寫入 ~/.ainovel/config.json")
	}
	return 0
}

func syncDiscoveredModelsToConfig(models []antigravity.DiscoveredModel) {
	configPath := bootstrap.DefaultConfigPath()
	if configPath == "" {
		return
	}

	cfg, err := bootstrap.LoadConfig()
	if err != nil {
		cfg = bootstrap.Config{
			Provider:  "antigravity",
			ModelName: "gemini-3.8-flash",
			Providers: make(map[string]bootstrap.ProviderConfig),
		}
	}

	if cfg.Providers == nil {
		cfg.Providers = make(map[string]bootstrap.ProviderConfig)
	}

	pc := cfg.Providers["antigravity"]
	existingMap := make(map[string]bool)
	var newModelConfigs []bootstrap.ModelConfig
	for _, m := range models {
		if existingMap[m.ID] {
			continue
		}
		existingMap[m.ID] = true
		newModelConfigs = append(newModelConfigs, bootstrap.ModelConfig{
			Name:          m.ID,
			ContextWindow: m.ContextWindow,
		})
	}
	pc.Models = newModelConfigs
	cfg.Providers["antigravity"] = pc

	if cfg.Provider == "" || cfg.Provider == "antigravity" {
		cfg.Provider = "antigravity"
		if cfg.ModelName == "" && len(models) > 0 {
			cfg.ModelName = models[0].ID
		}
	}

	if err := bootstrap.SaveConfig(configPath, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "警告: 同步模型清單至設定檔失敗: %v\n", err)
	} else {
		fmt.Printf("✓ 已將 %d 個可用模型自動同步寫入至 %s\n", len(models), configPath)
	}
}

func runAntigravityStatus() int {
	creds, err := antigravity.LoadCredentials()
	if err != nil {
		fmt.Println("Google Antigravity: 未登入")
		fmt.Printf("憑證檔案不存在或無法讀取: %s\n", antigravity.DefaultAuthFilePath())
		fmt.Println("請執行: ainovel-cli auth login antigravity")
		return 0
	}

	now := time.Now().Unix()
	statusStr := "有效"
	if creds.ExpiresAt <= now {
		if creds.RefreshToken != "" {
			statusStr = "已過期 (將自動透過 Refresh Token 刷新)"
		} else {
			statusStr = "已過期 (需重新登入)"
		}
	}

	fmt.Println("Google Antigravity 憑證狀態:")
	fmt.Printf("  帳號: %s\n", creds.Email)
	if creds.ProjectID != "" {
		fmt.Printf("  GCP 專案: %s\n", creds.ProjectID)
	} else {
		fmt.Println("  GCP 專案: (尚未關聯，調用時將自動引導)")
	}
	fmt.Printf("  狀態: %s\n", statusStr)
	if creds.ExpiresAt > 0 {
		expTime := time.Unix(creds.ExpiresAt, 0).Local().Format("2006-01-02 15:04:05")
		fmt.Printf("  過期時間: %s\n", expTime)
	}
	fmt.Printf("  設定檔位置: %s\n", antigravity.DefaultAuthFilePath())
	return 0
}

func runAntigravityLogout() int {
	path := antigravity.DefaultAuthFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fmt.Println("Google Antigravity 未登入，無須清除。")
		return 0
	}
	if err := os.Remove(path); err != nil {
		fmt.Fprintf(os.Stderr, "清除憑證失敗: %v\n", err)
		return 1
	}
	fmt.Println("✓ Google Antigravity 憑證已清除。")
	return 0
}
