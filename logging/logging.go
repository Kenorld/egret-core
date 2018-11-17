package logging

import (
	"go.uber.org/zap"
)

var Logger *zap.Logger

func Init(cfg *zap.Config) (*zap.Logger, error) {
	logger, err := cfg.Build()
	Logger = logger
	return logger, err
}
