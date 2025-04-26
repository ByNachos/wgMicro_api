package logger

import (
	"go.uber.org/zap"
)

// Logger - глобальный экземпляр zap.Logger
var Logger *zap.Logger

// Init инициализирует Logger, пишущий логи и в stdout, и в файл по пути logFilePath.
// logFilePath может быть относительным или абсолютным, например "logs/app.log".
func Init(logFilePath string) {
	// Берём стандартную production-конфигурацию (JSON, уровень INFO+).
	cfg := zap.NewProductionConfig()
	// Указываем два выхода: консоль и файл[6].
	cfg.OutputPaths = []string{"stdout", logFilePath}
	// Ошибки логируем в stderr и в тот же файл
	cfg.ErrorOutputPaths = []string{"stderr", logFilePath}

	// Дополняем конфиг опцией записи caller (файл:строка)
	// и создаём Logger
	logger, err := cfg.Build(zap.AddCaller())
	if err != nil {
		panic("failed to initialize zap logger: " + err.Error())
	}

	Logger = logger

	// Пробное сообщение - теперь вы увидите его и в консоли, и в файле
	Logger.Info("Logger initialized, output to console and file")
}
