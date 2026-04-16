package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"whatsapp_golang/internal/llm"
	"whatsapp_golang/internal/model"
	contentSvc "whatsapp_golang/internal/service/content"
	systemSvc "whatsapp_golang/internal/service/system"
)

func main() {
	accountID := flag.Uint("account", 0, "WhatsApp account ID")
	chatID := flag.Uint("chat", 0, "Chat ID (optional, analyzes all chats if omitted)")
	flag.Parse()

	if *accountID == 0 {
		fmt.Fprintf(os.Stderr, "Usage: go run scripts/trigger_ai_analysis.go -account <id> [-chat <id>]\n")
		os.Exit(1)
	}

	_ = godotenv.Load()

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		os.Getenv("DB_HOST"), os.Getenv("DB_PORT"),
		os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"),
		os.Getenv("DB_NAME"), os.Getenv("DB_SSLMODE"))

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("DB 連線失敗: %v", err)
	}

	llmClient := llm.NewClient(func() string {
		// 獨立腳本：先查 DB system_configs，fallback 到 env
		var val string
		db.Raw("SELECT config_value FROM system_configs WHERE config_key = 'llm.api_key' LIMIT 1").Scan(&val)
		if val != "" {
			return val
		}
		return os.Getenv("OPENROUTER_API_KEY")
	}, 60)

	configSvc := systemSvc.NewConfigService(db)
	tagDefSvc := contentSvc.NewAiTagDefinitionService(db)

	analysisSvc := contentSvc.NewChatAIAnalysisService(&contentSvc.ChatAIAnalysisConfig{
		DB:        db,
		LLMClient: llmClient,
		ConfigSvc: configSvc,
		TagDefSvc: tagDefSvc,
	})

	ctx := context.Background()

	if *chatID > 0 {
		// 單一聊天室
		var chat model.WhatsAppChat
		if err := db.First(&chat, *chatID).Error; err != nil {
			log.Fatalf("找不到 chat %d: %v", *chatID, err)
		}
		fmt.Printf("分析 chat: %s (account=%d, chat=%d)\n", chat.JID, *accountID, *chatID)

		if err := analysisSvc.AnalyzeChat(ctx, uint(*accountID), uint(*chatID)); err != nil {
			log.Fatalf("分析失敗: %v", err)
		}
		fmt.Println("分析完成")
		return
	}

	// 整個帳號：查出所有非群組聊天室
	var chats []model.WhatsAppChat
	if err := db.Where("account_id = ? AND is_group = false", *accountID).Find(&chats).Error; err != nil {
		log.Fatalf("查詢聊天室失敗: %v", err)
	}
	fmt.Printf("帳號 %d 共 %d 個聊天室待分析\n", *accountID, len(chats))

	success, failed := 0, 0
	for i, chat := range chats {
		fmt.Printf("[%d/%d] 分析 chat %d (%s)...", i+1, len(chats), chat.ID, chat.JID)
		if err := analysisSvc.AnalyzeChat(ctx, uint(*accountID), chat.ID); err != nil {
			fmt.Printf(" 失敗: %v\n", err)
			failed++
		} else {
			fmt.Println(" 完成")
			success++
		}
	}
	fmt.Printf("分析結束：成功 %d，失敗 %d\n", success, failed)
}
