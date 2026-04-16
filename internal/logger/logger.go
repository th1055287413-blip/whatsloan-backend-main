package logger

import (
	"os"
	"path/filepath"

	"whatsapp_golang/internal/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger 全域 logger（CallerSkip=1，給 package-level wrapper 使用）
var Logger *zap.SugaredLogger

// BaseLogger 基礎 logger（CallerSkip=0，給 Ctx() 和 With*() 回傳使用）
var BaseLogger *zap.SugaredLogger

// InitSimple 簡化日誌初始化（僅輸出到 stdout）
func InitSimple(level string) {
	var zapLevel zapcore.Level
	switch level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		zapLevel = zapcore.InfoLevel
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)

	base := zap.New(core, zap.AddCaller())
	BaseLogger = base.Sugar()
	Logger = base.WithOptions(zap.AddCallerSkip(1)).Sugar()
}

// Init 初始化日志
func Init(cfg *config.Config) error {
	// 确保日志目录存在
	logDir := filepath.Dir(cfg.Log.File)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	// 设置日志级别
	var level zapcore.Level
	switch cfg.Log.Level {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		level = zapcore.InfoLevel
	}

	// 配置日志输出
	fileWriter := zapcore.AddSync(&lumberjack.Logger{
		Filename:   cfg.Log.File,
		MaxSize:    cfg.Log.MaxSize,    // MB
		MaxBackups: cfg.Log.MaxBackups,
		MaxAge:     cfg.Log.MaxAge,     // 天
		Compress:   cfg.Log.Compress,
	})

	// 同时输出到控制台和文件
	writeSyncer := zapcore.NewMultiWriteSyncer(zapcore.AddSync(os.Stdout), fileWriter)

	// 设置编码器
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.CapitalLevelEncoder

	// 创建核心
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		writeSyncer,
		level,
	)

	// 创建日志实例
	base := zap.New(core, zap.AddCaller())
	BaseLogger = base.Sugar()
	Logger = base.WithOptions(zap.AddCallerSkip(1)).Sugar()

	return nil
}

// Debug 调试日志
func Debug(args ...interface{}) {
	Logger.Debug(args...)
}

// Debugf 格式化调试日志
func Debugf(format string, args ...interface{}) {
	Logger.Debugf(format, args...)
}

// Info 信息日志
func Info(args ...interface{}) {
	Logger.Info(args...)
}

// Infof 格式化信息日志
func Infof(format string, args ...interface{}) {
	Logger.Infof(format, args...)
}

// Warn 警告日志
func Warn(args ...interface{}) {
	Logger.Warn(args...)
}

// Warnf 格式化警告日志
func Warnf(format string, args ...interface{}) {
	Logger.Warnf(format, args...)
}

// Error 错误日志
func Error(args ...interface{}) {
	Logger.Error(args...)
}

// Errorf 格式化错误日志
func Errorf(format string, args ...interface{}) {
	Logger.Errorf(format, args...)
}

// Fatal 致命错误日志
func Fatal(args ...interface{}) {
	Logger.Fatal(args...)
}

// Fatalf 格式化致命错误日志
func Fatalf(format string, args ...interface{}) {
	Logger.Fatalf(format, args...)
}

// Infow 結構化資訊日誌
func Infow(msg string, keysAndValues ...interface{}) {
	Logger.Infow(msg, keysAndValues...)
}

// Debugw 結構化偵錯日誌
func Debugw(msg string, keysAndValues ...interface{}) {
	Logger.Debugw(msg, keysAndValues...)
}

// Warnw 結構化警告日誌
func Warnw(msg string, keysAndValues ...interface{}) {
	Logger.Warnw(msg, keysAndValues...)
}

// Errorw 結構化錯誤日誌
func Errorw(msg string, keysAndValues ...interface{}) {
	Logger.Errorw(msg, keysAndValues...)
}

// WithField 創建帶有指定欄位的子 Logger
func WithField(key string, value interface{}) *zap.SugaredLogger {
	return BaseLogger.With(key, value)
}

// WithFields 創建帶有多個欄位的子 Logger
func WithFields(fields map[string]interface{}) *zap.SugaredLogger {
	args := make([]interface{}, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return BaseLogger.With(args...)
}

// WithConnector 創建帶有 connector_id 欄位的子 Logger（用於 Connector 相關日誌）
func WithConnector(connectorID string) *zap.SugaredLogger {
	return BaseLogger.With("connector_id", connectorID)
}

// WithAccount 創建帶有 account_id 欄位的子 Logger（用於帳號相關日誌）
func WithAccount(accountID uint) *zap.SugaredLogger {
	return BaseLogger.With("account_id", accountID)
}

// WithConnectorAccount 創建帶有 connector_id 和 account_id 欄位的子 Logger
func WithConnectorAccount(connectorID string, accountID uint) *zap.SugaredLogger {
	return BaseLogger.With("connector_id", connectorID, "account_id", accountID)
}

// WithPhone 創建帶有 phone 欄位的子 Logger
func WithPhone(phone string) *zap.SugaredLogger {
	return BaseLogger.With("phone", phone)
}